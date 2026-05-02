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
	solanaWSReadDeadline   = 90 * time.Second // extended: pings keep it alive
	solanaMaxResponseBytes = 4 << 20          // 4 MiB per RPC response
	// solanaWSPingInterval removed: each provider dialect owns its own value.
)

// endpointEntry pairs an RPC endpoint URL with the provider dialect that
// governs its rate-limit codes, ping cadence, and other behaviours.
type endpointEntry struct {
	URL     string
	Dialect ProviderDialect
}

// SolanaClient implements ingestion_solana.SolanaRPCClient.
// Supports multi-endpoint failover: on a provider rate-limit error the client
// rotates to the next configured endpoint so the caller's retry hits a
// different provider (e.g. QuickNode → Helius).
// Each endpoint carries a ProviderDialect that captures provider-specific
// behaviours (rate-limit codes, WS ping interval) — the core client logic is
// provider-agnostic.
type SolanaClient struct {
	wsEndpoints   []endpointEntry // all configured ws endpoints, priority order
	httpEndpoints []endpointEntry // all configured http endpoints, priority order
	// wsIdx / httpIdx are atomically incremented on provider rate-limit errors.
	// activeWS / activeHTTP mod-wrap so the index cycles through all endpoints.
	wsIdx      atomic.Int64
	httpIdx    atomic.Int64
	httpClient *http.Client
	logger     *slog.Logger
	idCounter  atomic.Int64
	// txRateLimiter throttles getTransaction calls to the configured req/s cap.
	// Waiting for a tick before each call prevents rate-limit errors.
	txRateLimiter <-chan time.Time
}

// activeWS returns the current WebSocket endpoint entry.
// Returns a zero-value endpointEntry (empty URL) if no WS endpoints are configured.
func (c *SolanaClient) activeWS() endpointEntry {
	if len(c.wsEndpoints) == 0 {
		return endpointEntry{}
	}
	return c.wsEndpoints[int(c.wsIdx.Load())%len(c.wsEndpoints)]
}

// activeHTTP returns the current HTTP endpoint entry.
// Returns a zero-value endpointEntry (empty URL) if no HTTP endpoints are configured.
func (c *SolanaClient) activeHTTP() endpointEntry {
	if len(c.httpEndpoints) == 0 {
		return endpointEntry{}
	}
	return c.httpEndpoints[int(c.httpIdx.Load())%len(c.httpEndpoints)]
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
	// Each entry carries a ProviderDialect derived from the provider hint or URL.
	var wsEPs, httpEPs []endpointEntry
	for _, ep := range cfg.RPCEndpoints {
		if ep.URL == "" {
			continue
		}
		entry := endpointEntry{
			URL:     ep.URL,
			Dialect: detectDialect(ep.Provider, ep.URL),
		}
		switch ep.Kind {
		case "http":
			httpEPs = append(httpEPs, entry)
		case "ws":
			wsEPs = append(wsEPs, entry)
		}
	}

	if len(wsEPs) == 0 && len(httpEPs) == 0 {
		return nil, fmt.Errorf("solana_client: no RPC endpoints configured in chains.yaml")
	}

	logger.Info("solana_client_endpoints_loaded",
		"ws_count", len(wsEPs),
		"http_count", len(httpEPs),
	)
	for i, e := range wsEPs {
		logger.Info("solana_ws_endpoint", "index", i, "provider", e.Dialect.Name())
	}
	for i, e := range httpEPs {
		logger.Info("solana_http_endpoint", "index", i, "provider", e.Dialect.Name())
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
// On a provider rate-limit error (dialect-specific code) the HTTP endpoint
// index is rotated so the next call uses the fallback provider.
func (c *SolanaClient) httpRPC(ctx context.Context, method string, params []interface{}, result interface{}) error {
	entry := c.activeHTTP()
	if entry.URL == "" {
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

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, entry.URL, bytes.NewReader(body))
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
		if entry.Dialect.IsRateLimited(rpcResp.Error.Code) {
			newIdx := c.httpIdx.Add(1)
			nextEntry := c.httpEndpoints[int(newIdx)%len(c.httpEndpoints)]
			c.logger.Warn("solana_http_rate_limited_rotating",
				"method", method,
				"provider", entry.Dialect.Name(),
				"from", entry.URL,
				"to_provider", nextEntry.Dialect.Name(),
				"to", nextEntry.URL,
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
// When the provider returns a rate-limit error the active WS endpoint is
// rotated so that the next call from runProgramLoop's reconnect loop hits the
// fallback provider (e.g. QuickNode → Helius).
func (c *SolanaClient) SubscribeLogs(ctx context.Context, programID string) (<-chan ingestion_solana.LogsNotification, error) {
	// Capture the current endpoint entry — dialect and URL are bound for the
	// lifetime of this subscription session.
	wsEntry := c.activeWS()
	if wsEntry.URL == "" {
		return nil, fmt.Errorf("solana_client: no WebSocket endpoint configured")
	}

	conn, err := dialWS(wsEntry.URL, solanaWSConnectTimeout)
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
		if wsEntry.Dialect.IsRateLimited(subResp.Error.Code) {
			newIdx := c.wsIdx.Add(1)
			nextEntry := c.wsEndpoints[int(newIdx)%len(c.wsEndpoints)]
			c.logger.Warn("solana_ws_rate_limited_rotating",
				"program", programID,
				"provider", wsEntry.Dialect.Name(),
				"from", wsEntry.URL,
				"to_provider", nextEntry.Dialect.Name(),
				"to", nextEntry.URL,
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
			"provider", wsEntry.Dialect.Name(),
			"subscription_id", subID,
		)

		// Keepalive: send pings at the provider's recommended interval.
		// Each pong resets the read deadline, preventing the 90 s window
		// from firing on quiet slots.
		go func() {
			ticker := time.NewTicker(wsEntry.Dialect.WSPingInterval())
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

// AccountInfo is the minimal subset of getAccountInfo response we need for
// Pyth price-account decoding (Phase 3 — price-feed-integration).
//
// Solana's getAccountInfo returns Data as a 2-element array [base64Data, "base64"]
// when encoding="base64" is requested; this struct preserves that wire shape.
type AccountInfo struct {
	Data       []string `json:"data"`     // ["<base64>","base64"]
	Owner      string   `json:"owner"`    // program owner (e.g. Pyth oracle program)
	Lamports   uint64   `json:"lamports"` // 0 means account does not exist
	Slot       uint64   `json:"-"`        // populated from context.slot wrapper
	Executable bool     `json:"executable"`
}

// GetAccountInfo fetches an account's raw data + owner at the given commitment.
// Used by the Phase 3 Pyth SOL/USD price feed and (later) Phase 4 enrichment
// for AMM reserve decoding. Returns ("", nil) when the account does not exist.
func (c *SolanaClient) GetAccountInfo(ctx context.Context, pubkey, commitment string) (*AccountInfo, error) {
	if commitment == "" {
		commitment = "confirmed"
	}
	params := []interface{}{
		pubkey,
		map[string]interface{}{
			"commitment": commitment,
			"encoding":   "base64",
		},
	}

	var result struct {
		Context struct {
			Slot uint64 `json:"slot"`
		} `json:"context"`
		Value *AccountInfo `json:"value"`
	}
	if err := c.httpRPC(ctx, "getAccountInfo", params, &result); err != nil {
		return nil, err
	}
	if result.Value == nil {
		return nil, nil
	}
	result.Value.Slot = result.Context.Slot
	return result.Value, nil
}

// TokenLargestAccount is one entry returned by getTokenLargestAccounts.
// Amount is the raw token amount (uint64 as string in JSON-RPC).
type TokenLargestAccount struct {
	Address  string `json:"address"`
	Amount   string `json:"amount"`
	Decimals int    `json:"decimals"`
}

// GetTokenLargestAccounts returns up to 20 largest token-account holders
// for an SPL mint, ordered by amount descending. Used by enrichment
// probes to compute holder concentration. Empty slice when the mint
// has no holders or does not exist.
func (c *SolanaClient) GetTokenLargestAccounts(ctx context.Context, mint, commitment string) ([]TokenLargestAccount, error) {
	if commitment == "" {
		commitment = "confirmed"
	}
	params := []interface{}{
		mint,
		map[string]interface{}{"commitment": commitment},
	}

	var result struct {
		Context struct {
			Slot uint64 `json:"slot"`
		} `json:"context"`
		Value []TokenLargestAccount `json:"value"`
	}
	if err := c.httpRPC(ctx, "getTokenLargestAccounts", params, &result); err != nil {
		return nil, err
	}
	return result.Value, nil
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
	// Meta.InnerInstructions is populated when a top-level instruction triggers
	// CPI calls. Pump.fun "create" is frequently invoked via CPI (e.g. from
	// launchpad wrappers), so inner instructions must be parsed to avoid
	// silently missing token creation events.
	Meta struct {
		LoadedAddresses struct {
			Writable []string `json:"writable"`
			Readonly []string `json:"readonly"`
		} `json:"loadedAddresses"`
		InnerInstructions []struct {
			// Index is the position of the outer instruction that triggered
			// these CPI calls. Used only to assign a stable InstructionData.Index.
			Index        int `json:"index"`
			Instructions []struct {
				ProgramIDIndex int    `json:"programIdIndex"`
				Accounts       []int  `json:"accounts"`
				Data           string `json:"data"` // base58
			} `json:"instructions"`
		} `json:"innerInstructions"`
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

	// decodeInstr is a shared helper that resolves a single instruction's
	// programID, accounts, and data bytes from the combined account key list.
	decodeInstr := func(programIDIndex int, accountIndices []int, data string, index int) (ingestion_solana.InstructionData, bool) {
		if programIDIndex < 0 || programIDIndex >= len(keys) {
			return ingestion_solana.InstructionData{}, false
		}
		accounts := make([]string, 0, len(accountIndices))
		for _, idx := range accountIndices {
			if idx >= 0 && idx < len(keys) {
				accounts = append(accounts, keys[idx])
			}
		}
		return ingestion_solana.InstructionData{
			ProgramID: keys[programIDIndex],
			Accounts:  accounts,
			Data:      decodeBase58(data),
			Index:     index,
		}, true
	}

	instrs := make([]ingestion_solana.InstructionData, 0, len(msg.Instructions))
	for i, instr := range msg.Instructions {
		if d, ok := decodeInstr(instr.ProgramIDIndex, instr.Accounts, instr.Data, i); ok {
			instrs = append(instrs, d)
		}
	}

	// Append inner instructions (CPI calls) so normalizers can detect programs
	// like Pump.fun "create" that are frequently invoked via CPI from wrappers.
	// Inner instruction indices are encoded as <outer_index>.<inner_position>
	// multiplied to avoid collisions with top-level indices; the exact value is
	// only used for content-addressable EventID generation.
	for _, outer := range tx.Meta.InnerInstructions {
		for j, instr := range outer.Instructions {
			// Use a stable index derived from outer position and inner offset.
			// Offset by len(msg.Instructions) to prevent collisions with outer indices.
			innerIndex := len(msg.Instructions) + outer.Index*1000 + j
			if d, ok := decodeInstr(instr.ProgramIDIndex, instr.Accounts, instr.Data, innerIndex); ok {
				instrs = append(instrs, d)
			}
		}
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
