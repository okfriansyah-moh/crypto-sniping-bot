// solana_rpc.go — concrete SolanaRPCClient implementation for the ingestion module.
//
// Implements ingestion_solana.SolanaRPCClient:
//   - SubscribeLogs  → Solana WebSocket logsSubscribe (RFC 6455, JSON-RPC 2.0)
//   - GetTransaction → HTTP POST getTransaction
//   - GetLatestBlockhash → HTTP POST getLatestBlockhash
//   - GetSlot → HTTP POST getSlot
//   - GetSignaturesForAddress → HTTP POST getSignaturesForAddress
//
// Endpoint URLs are taken from config.SolanaConfig.RPCEndpoints at construction
// time.  ${ENV_VAR} references are expanded by the config loader before reaching
// this package.
//
// Architecture invariants:
//   - Does NOT import database/ or contracts/.
//   - Does NOT import other internal/modules/ packages.
//   - All HTTP calls use a bounded timeout (requestTimeout) — no unbounded reads.
//   - Bot token and private keys never appear in log output.
package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/ingestion_solana"
)

const (
	solanaRequestTimeout   = 10 * time.Second
	solanaWSConnectTimeout = 20 * time.Second
	solanaWSReadDeadline   = 90 * time.Second // extended: ping every 20s keeps it alive
	solanaWSPingInterval   = 20 * time.Second // client-side keepalive ping interval
	solanaMaxResponseBytes = 4 << 20          // 4 MiB per RPC response
)

// SolanaClient implements ingestion_solana.SolanaRPCClient.
// Supports multi-endpoint failover: on a -32003 rate-limit error the client
// rotates to the next configured endpoint so the caller's retry hits a
// different provider (e.g. QuickNode → Helius).
type SolanaClient struct {
	wsEndpoints   []string // all configured ws endpoints, priority order
	httpEndpoints []string // all configured http endpoints, priority order
	// wsIdx / httpIdx are atomically incremented on -32003 errors.
	// activeWS / activeHTTP mod-wrap so the index cycles through all endpoints.
	wsIdx      atomic.Int64
	httpIdx    atomic.Int64
	httpClient *http.Client
	logger     *slog.Logger
	idCounter  atomic.Int64
	// txRateLimiter throttles getTransaction calls to the configured req/s cap.
	// Waiting for a tick before each call prevents -32013 rate-limit errors.
	txRateLimiter <-chan time.Time
}

// activeWS returns the current WebSocket endpoint.
func (c *SolanaClient) activeWS() string {
	if len(c.wsEndpoints) == 0 {
		return ""
	}
	return c.wsEndpoints[int(c.wsIdx.Load())%len(c.wsEndpoints)]
}

// activeHTTP returns the current HTTP endpoint.
func (c *SolanaClient) activeHTTP() string {
	if len(c.httpEndpoints) == 0 {
		return ""
	}
	return c.httpEndpoints[int(c.httpIdx.Load())%len(c.httpEndpoints)]
}

// isWSRateLimited returns true when the RPC error is a -32003 quota error on the WS path.
// When true the caller should rotate and return the error so the reconnect loop retries
// on the next endpoint.
func isWSRateLimited(e *rpcError) bool {
	return e != nil && e.Code == -32003
}

// NewSolanaClient returns a SolanaClient built from the given SolanaConfig.
// All configured ws and http endpoints are stored in priority order so the
// client can rotate to a fallback when the primary returns -32003.
// Returns an error if no endpoints are found.
func NewSolanaClient(cfg config.SolanaConfig, logger *slog.Logger) (*SolanaClient, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// Collect endpoints by kind, preserving the order they appear in config
	// (config is already sorted by priority in chains.yaml).
	var wsEPs, httpEPs []string
	for _, ep := range cfg.RPCEndpoints {
		switch ep.Kind {
		case "http":
			if ep.URL != "" {
				httpEPs = append(httpEPs, ep.URL)
			}
		case "ws":
			if ep.URL != "" {
				wsEPs = append(wsEPs, ep.URL)
			}
		}
	}

	if len(wsEPs) == 0 && len(httpEPs) == 0 {
		return nil, fmt.Errorf("solana_client: no RPC endpoints configured in chains.yaml")
	}

	if len(wsEPs) > 1 {
		logger.Info("solana_client_endpoints_loaded",
			"ws_count", len(wsEPs),
			"http_count", len(httpEPs),
		)
	}

	return &SolanaClient{
		wsEndpoints:   wsEPs,
		httpEndpoints: httpEPs,
		httpClient: &http.Client{
			Timeout: solanaRequestTimeout,
		},
		logger:        logger,
		txRateLimiter: buildRateLimiter(cfg.GetTransactionRPS),
	}, nil
}

// defaultGetTransactionRPS is the fallback when get_transaction_rps is unset.
const defaultGetTransactionRPS = 12

// buildRateLimiter returns a channel that produces one tick per interval,
// effectively limiting callers to rps requests per second.
// If rps ≤ 0 the default is used.
func buildRateLimiter(rps int) <-chan time.Time {
	if rps <= 0 {
		rps = defaultGetTransactionRPS
	}
	return time.NewTicker(time.Second / time.Duration(rps)).C
}

// ── JSON-RPC helpers ──────────────────────────────────────────────────────────

type rpcRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int64         `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

func (c *SolanaClient) nextID() int64 {
	return c.idCounter.Add(1)
}

// httpRPC sends a single JSON-RPC request to the active HTTP endpoint and
// unmarshals the result into result (must be a pointer).
// On a -32003 rate-limit error the HTTP endpoint index is rotated so the next
// call uses the fallback provider.
func (c *SolanaClient) httpRPC(ctx context.Context, method string, params []interface{}, result interface{}) error {
	httpEP := c.activeHTTP()
	if httpEP == "" {
		return fmt.Errorf("solana_client: no HTTP endpoint configured")
	}

	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      c.nextID(),
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("solana_client: marshal %s request: %w", method, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, httpEP, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("solana_client: build %s request: %w", method, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("solana_client: %s: %w", method, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	raw, err := io.ReadAll(io.LimitReader(resp.Body, solanaMaxResponseBytes))
	if err != nil {
		return fmt.Errorf("solana_client: read %s response: %w", method, err)
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(raw, &rpcResp); err != nil {
		return fmt.Errorf("solana_client: parse %s response: %w", method, err)
	}
	if rpcResp.Error != nil {
		if rpcResp.Error.Code == -32003 {
			newIdx := c.httpIdx.Add(1)
			c.logger.Warn("solana_http_rate_limited_rotating",
				"method", method,
				"from", httpEP,
				"to", c.httpEndpoints[int(newIdx)%len(c.httpEndpoints)],
				"total_endpoints", len(c.httpEndpoints),
			)
		}
		return fmt.Errorf("solana_client: %s: %w", method, rpcResp.Error)
	}
	if result != nil && len(rpcResp.Result) > 0 {
		if err := json.Unmarshal(rpcResp.Result, result); err != nil {
			return fmt.Errorf("solana_client: unmarshal %s result: %w", method, err)
		}
	}
	return nil
}

// ── SolanaRPCClient interface ─────────────────────────────────────────────────

// SubscribeLogs opens a logsSubscribe WebSocket subscription for programID.
// Returns a channel that receives LogsNotification values until:
//   - ctx is cancelled, or
//   - the WebSocket connection drops (channel is closed; caller should reconnect).
//
// When the server returns -32003 (rate limit) the active WS endpoint is rotated
// so that the next call from runProgramLoop's reconnect loop hits the fallback
// provider (e.g. QuickNode → Helius).
func (c *SolanaClient) SubscribeLogs(ctx context.Context, programID string) (<-chan ingestion_solana.LogsNotification, error) {
	wsEP := c.activeWS()
	if wsEP == "" {
		return nil, fmt.Errorf("solana_client: no WebSocket endpoint configured")
	}

	conn, err := dialWS(wsEP, solanaWSConnectTimeout)
	if err != nil {
		return nil, fmt.Errorf("solana_client: ws connect: %w", err)
	}

	// Send logsSubscribe request.
	subReq := rpcRequest{
		JSONRPC: "2.0",
		ID:      c.nextID(),
		Method:  "logsSubscribe",
		Params: []interface{}{
			map[string]interface{}{"mentions": []string{programID}},
			map[string]interface{}{"commitment": "confirmed"},
		},
	}
	if err := conn.WriteJSON(subReq); err != nil {
		conn.Close()
		return nil, fmt.Errorf("solana_client: send logsSubscribe: %w", err)
	}

	// Read subscription confirmation — must arrive before notifications.
	_ = conn.setDeadline(time.Now().Add(solanaWSReadDeadline))
	var subResp struct {
		JSONRPC string    `json:"jsonrpc"`
		ID      int64     `json:"id"`
		Result  int64     `json:"result"` // subscription ID
		Error   *rpcError `json:"error"`
	}
	if err := conn.ReadJSON(&subResp); err != nil {
		conn.Close()
		return nil, fmt.Errorf("solana_client: read subscribe response: %w", err)
	}
	if subResp.Error != nil {
		conn.Close()
		if isWSRateLimited(subResp.Error) {
			newIdx := c.wsIdx.Add(1)
			c.logger.Warn("solana_ws_rate_limited_rotating",
				"program", programID,
				"from", wsEP,
				"to", c.wsEndpoints[int(newIdx)%len(c.wsEndpoints)],
				"total_endpoints", len(c.wsEndpoints),
			)
		}
		return nil, fmt.Errorf("solana_client: logsSubscribe: %w", subResp.Error)
	}
	_ = conn.setDeadline(time.Time{}) // clear deadline; notifications are unbounded
	// Enable per-frame deadline refresh so that pong frames (consumed
	// transparently inside ReadJSON) reset the window, preventing spurious
	// i/o timeout errors on quiet slots.
	conn.readDeadline = solanaWSReadDeadline

	subID := subResp.Result
	ch := make(chan ingestion_solana.LogsNotification, 256)

	go func() {
		defer conn.Close()
		defer close(ch)
		c.logger.Info("solana_ws_subscribed",
			"program", programID,
			"subscription_id", subID,
		)

		// Keepalive: send a ping every solanaWSPingInterval so the server
		// responds with a pong. ReadJSON consumes pongs silently, which
		// means each pong arrival resets the effective idle window and
		// prevents the 90 s read-deadline from firing on quiet slots.
		go func() {
			ticker := time.NewTicker(solanaWSPingInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := conn.writePing(); err != nil {
						return // connection dead; ReadJSON goroutine will also exit
					}
				}
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Renew read deadline each iteration to detect stale connections.
			_ = conn.setDeadline(time.Now().Add(solanaWSReadDeadline))

			var notif logsNotificationEnvelope
			if err := conn.ReadJSON(&notif); err != nil {
				if ctx.Err() != nil {
					return // clean shutdown
				}
				c.logger.Warn("solana_ws_read_error",
					"program", programID,
					"error", err,
				)
				return // channel close triggers reconnect in runProgramLoop
			}

			if notif.Method != "logsNotification" {
				continue
			}
			if notif.Params.Subscription != subID {
				continue
			}

			v := notif.Params.Result.Value
			select {
			case ch <- ingestion_solana.LogsNotification{
				Signature: v.Signature,
				Logs:      v.Logs,
				Slot:      notif.Params.Result.Context.Slot,
				Err:       v.Err,
			}:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// GetTransaction fetches the full transaction by signature.
// Returns nil if the transaction is not yet visible at the configured commitment.
func (c *SolanaClient) GetTransaction(ctx context.Context, signature string) (*ingestion_solana.TransactionResult, error) {
	// Wait for a rate-limit token before issuing the HTTP call.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.txRateLimiter:
	}
	params := []interface{}{
		signature,
		map[string]interface{}{
			"encoding":                       "json",
			"commitment":                     "confirmed",
			"maxSupportedTransactionVersion": 0,
		},
	}

	var raw json.RawMessage
	if err := c.httpRPC(ctx, "getTransaction", params, &raw); err != nil {
		return nil, err
	}
	if string(raw) == "null" || len(raw) == 0 {
		return nil, nil // not yet visible
	}

	return parseGetTransactionResponse(signature, raw)
}

// GetLatestBlockhash returns the most recent blockhash and its last-valid slot.
func (c *SolanaClient) GetLatestBlockhash(ctx context.Context, commitment string) (string, uint64, error) {
	if commitment == "" {
		commitment = "confirmed"
	}
	params := []interface{}{
		map[string]interface{}{"commitment": commitment},
	}

	var result struct {
		Value struct {
			Blockhash            string `json:"blockhash"`
			LastValidBlockHeight uint64 `json:"lastValidBlockHeight"`
		} `json:"value"`
	}
	if err := c.httpRPC(ctx, "getLatestBlockhash", params, &result); err != nil {
		return "", 0, err
	}
	return result.Value.Blockhash, result.Value.LastValidBlockHeight, nil
}

// GetSlot returns the current slot at the given commitment level.
func (c *SolanaClient) GetSlot(ctx context.Context, commitment string) (uint64, error) {
	if commitment == "" {
		commitment = "confirmed"
	}
	params := []interface{}{
		map[string]interface{}{"commitment": commitment},
	}

	var slot uint64
	if err := c.httpRPC(ctx, "getSlot", params, &slot); err != nil {
		return 0, err
	}
	return slot, nil
}

// GetSignaturesForAddress returns up to limit signatures for programID within
// the given slot range. Results are newest-first per the Solana API contract.
func (c *SolanaClient) GetSignaturesForAddress(ctx context.Context, programID string, fromSlot, toSlot uint64, limit int) ([]string, error) {
	params := []interface{}{
		programID,
		map[string]interface{}{
			"limit":      limit,
			"commitment": "confirmed",
		},
	}

	var sigs []struct {
		Signature string `json:"signature"`
		Slot      uint64 `json:"slot"`
	}
	if err := c.httpRPC(ctx, "getSignaturesForAddress", params, &sigs); err != nil {
		return nil, err
	}

	out := make([]string, 0, len(sigs))
	for _, s := range sigs {
		if (fromSlot == 0 || s.Slot >= fromSlot) &&
			(toSlot == 0 || s.Slot <= toSlot) {
			out = append(out, s.Signature)
		}
	}
	return out, nil
}

// ── WebSocket notification types ──────────────────────────────────────────────

// logsNotificationEnvelope is the JSON shape of a Solana logsNotification.
type logsNotificationEnvelope struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  struct {
		Subscription int64 `json:"subscription"`
		Result       struct {
			Context struct {
				Slot uint64 `json:"slot"`
			} `json:"context"`
			Value struct {
				Signature string      `json:"signature"`
				Logs      []string    `json:"logs"`
				Err       interface{} `json:"err"`
			} `json:"value"`
		} `json:"result"`
	} `json:"params"`
}

// ── getTransaction response parser ───────────────────────────────────────────

// solanaTransactionJSON is the JSON shape of a getTransaction result.
// For v0 transactions, static account keys are in Transaction.Message.AccountKeys.
// ALT-resolved accounts live in Meta.LoadedAddresses and must be appended to
// form the full account list used by instruction account indices.
type solanaTransactionJSON struct {
	Slot        uint64 `json:"slot"`
	BlockTime   int64  `json:"blockTime"`
	Transaction struct {
		Message struct {
			AccountKeys     []string `json:"accountKeys"`
			RecentBlockhash string   `json:"recentBlockhash"`
			Instructions    []struct {
				ProgramIDIndex int    `json:"programIdIndex"`
				Accounts       []int  `json:"accounts"`
				Data           string `json:"data"` // base58
			} `json:"instructions"`
		} `json:"message"`
	} `json:"transaction"`
	// Meta.LoadedAddresses is populated for v0 transactions that use Address
	// Lookup Tables (ALTs).  Account indices in instructions refer into the
	// combined list: static keys + ALT writable + ALT readonly.
	Meta struct {
		LoadedAddresses struct {
			Writable []string `json:"writable"`
			Readonly []string `json:"readonly"`
		} `json:"loadedAddresses"`
	} `json:"meta"`
}

// parseGetTransactionResponse converts the raw JSON from getTransaction into
// a TransactionResult suitable for the normalizer.
func parseGetTransactionResponse(signature string, raw json.RawMessage) (*ingestion_solana.TransactionResult, error) {
	var tx solanaTransactionJSON
	if err := json.Unmarshal(raw, &tx); err != nil {
		return nil, fmt.Errorf("solana_client: parse transaction %s: %w", signature, err)
	}

	msg := tx.Transaction.Message
	// Build the full account list: static keys first, then ALT-resolved accounts
	// in Solana's canonical order (writable before readonly).  Instruction
	// account indices (programIdIndex and accounts[]) index into this combined
	// slice — using only the static keys causes out-of-bounds resolves and
	// silently drops accounts, producing "insufficient accounts" errors.
	keys := make([]string, 0,
		len(msg.AccountKeys)+
			len(tx.Meta.LoadedAddresses.Writable)+
			len(tx.Meta.LoadedAddresses.Readonly),
	)
	keys = append(keys, msg.AccountKeys...)
	keys = append(keys, tx.Meta.LoadedAddresses.Writable...)
	keys = append(keys, tx.Meta.LoadedAddresses.Readonly...)

	instrs := make([]ingestion_solana.InstructionData, 0, len(msg.Instructions))
	for i, instr := range msg.Instructions {
		if instr.ProgramIDIndex < 0 || instr.ProgramIDIndex >= len(keys) {
			continue
		}
		accounts := make([]string, 0, len(instr.Accounts))
		for _, idx := range instr.Accounts {
			if idx >= 0 && idx < len(keys) {
				accounts = append(accounts, keys[idx])
			}
		}
		// Data is base58 in the JSON API; store as raw bytes for the normalizer.
		data := decodeBase58(instr.Data)
		instrs = append(instrs, ingestion_solana.InstructionData{
			ProgramID: keys[instr.ProgramIDIndex],
			Accounts:  accounts,
			Data:      data,
			Index:     i,
		})
	}

	return &ingestion_solana.TransactionResult{
		Signature:       signature,
		Slot:            tx.Slot,
		BlockTime:       tx.BlockTime,
		Instructions:    instrs,
		AccountKeys:     keys,
		RecentBlockhash: msg.RecentBlockhash,
	}, nil
}

// decodeBase58 decodes a base-58 string into bytes.
// Returns nil on empty input; never panics.
func decodeBase58(s string) []byte {
	if s == "" {
		return nil
	}
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		carry := 0
		idx := -1
		for j := 0; j < len(alphabet); j++ {
			if alphabet[j] == s[i] {
				idx = j
				break
			}
		}
		if idx < 0 {
			return nil // invalid character
		}
		carry = idx
		for j := len(result) - 1; j >= 0; j-- {
			carry += 58 * int(result[j])
			result[j] = byte(carry & 0xFF)
			carry >>= 8
		}
		for carry > 0 {
			result = append([]byte{byte(carry & 0xFF)}, result...)
			carry >>= 8
		}
	}
	// Leading '1' characters → leading zero bytes.
	for i := 0; i < len(s) && s[i] == '1'; i++ {
		result = append([]byte{0}, result...)
	}
	return result
}
