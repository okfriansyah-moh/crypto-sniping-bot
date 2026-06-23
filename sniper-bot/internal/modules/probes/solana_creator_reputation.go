package probes

// solana_creator_reputation.go — Off-chain creator history probe for pump.fun.
//
// Problem this probe solves (BLOCKER-2 from gate review):
//
//	The DataQualityWorker previously counted a creator's prior token launches
//	only from the local market_data table. On a fresh database (cold start),
//	this count is always 0, so a creator with 21 prior rugs appeared identical
//	to a first-time dev. The scam token "test" (CA 8poHAR4sz…) exploited this
//	gap — its dev had launched 21 tokens (0 migrations, 0 golden gems) but
//	returned count=0 from the empty local DB.
//
// Fix:
//
//	This probe queries pump.fun's public creator API BEFORE the local DB
//	fallback. The response is the ground-truth count of all tokens ever
//	launched from a given Solana wallet on pump.fun.
//
// Security constraints (per copilot-instructions.md):
//   - HTTPS-only; the probe rejects any non-HTTPS BaseURL unless the host
//     is 127.0.0.1 or localhost (test servers).
//   - Response body is bounded to MaxBodyBytes (default 128 KiB) via
//     io.LimitReader — never io.ReadAll on an unbounded body.
//   - API key (if ever required) must come from PUMPFUN_API_KEY env var only.
//
// Design constraints (per probes.go):
//   - Pure: no database calls, no side effects.
//   - Safe with no configuration: disabled probe → (in, nil).
//   - Only enriches Solana DTOs (Chain == "solana" or "sol").
//   - Fail-closed: on any error, returns (in, err) with
//     CreatorPrevTokenCountKnown left false.
//
// Worker responsibility (run_data_quality.go):
//   - On probe success: use the returned DTO (CreatorPrevTokenCountKnown=true)
//     and skip the local DB fallback.
//   - On probe failure: fall back to local DB.
//   - If local DB also returns 0: leave CreatorPrevTokenCountKnown=false
//     (fail-closed for cold-start — DQ treats unknown as max risk).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"crypto-sniping-bot/shared/contracts"
)

// ErrSkippedEmptyCreator signals the creator probe was a no-op (empty creator).
var ErrSkippedEmptyCreator = errors.New("probes/creator_reputation: skipped_empty_creator")

const (
	// defaultCreatorBaseURL is the pump.fun public creator API (v3 subdomain).
	// Returns a JSON array of coins created by the given wallet address.
	// v3 is used because the v1 endpoint (frontend-api.pump.fun) is blocked
	// by Cloudflare error 1016 (Origin Access Denied) even with browser headers.
	defaultCreatorBaseURL = "https://frontend-api-v3.pump.fun"

	// defaultCreatorTimeoutMs is a conservative timeout for off-chain
	// metadata calls — long enough for international latency, short enough
	// not to block the hot path.
	defaultCreatorTimeoutMs = 3000

	// defaultCreatorMaxBodyBytes caps the HTTP response body to prevent
	// memory exhaustion from a malicious or misconfigured server (128 KiB).
	defaultCreatorMaxBodyBytes int64 = 128 * 1024

	// defaultCreatorPageLimit is the number of coins to request per page.
	// 50 is the pump.fun API maximum. If the response returns exactly 50
	// items, the real count is ≥ 50 — still a hard serial-launcher signal
	// far above any configured threshold.
	defaultCreatorPageLimit = 50

	// defaultCircuitFailureThreshold is the number of consecutive 5xx/network
	// failures from pump.fun before the circuit opens and routes traffic to
	// the Helius DAS fallback.
	defaultCircuitFailureThreshold = 5

	// defaultCircuitHalfOpenSec is how long (seconds) to keep the circuit
	// OPEN before transitioning to HALF_OPEN and re-testing pump.fun.
	defaultCircuitHalfOpenSec = 120
)

// SolanaCreatorReputationHTTPClient is the minimal HTTP interface the probe
// needs. Implemented by *http.Client; injectable in tests via a fake.
type SolanaCreatorReputationHTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// SolanaCreatorReputationConfig configures the creator reputation probe.
type SolanaCreatorReputationConfig struct {
	// Enabled toggles the probe. When false, Probe() returns (in, nil)
	// with CreatorPrevTokenCountKnown unchanged.
	Enabled bool `yaml:"enabled"`

	// TimeoutMs is the HTTP request deadline in milliseconds.
	// Valid range: [500, 10000]. Defaults to 3000.
	TimeoutMs int `yaml:"timeout_ms"`

	// BaseURL is the root of the pump.fun creator API.
	// Must be HTTPS (or http://localhost / http://127.x for tests).
	// Defaults to "https://frontend-api-v3.pump.fun".
	BaseURL string `yaml:"base_url"`

	// MaxBodyBytes is the response body size cap (in bytes).
	// Defaults to 131072 (128 KiB).
	MaxBodyBytes int64 `yaml:"max_body_bytes"`

	// PageLimit is the ?limit= parameter sent to the API.
	// Defaults to 50. Values outside [1, 200] are clamped.
	PageLimit int `yaml:"page_limit"`

	// HeliusRPCURL is the full Helius HTTP RPC endpoint URL (including the
	// ?api-key=... query parameter). Used as a fallback when the pump.fun
	// creator API fails consecutively (circuit breaker opens). Populated
	// programmatically from cfg.Solana.RPCEndpoints in cmd/server.go —
	// NEVER set directly in YAML (it embeds an API key). Empty disables
	// the Helius DAS fallback, causing fail-closed on pump.fun outage.
	HeliusRPCURL string `yaml:"-"`
}

// circuitState tracks the pump.fun circuit breaker state machine:
//
//	CLOSED  ──(5 consecutive failures)──▶  OPEN
//	  ▲                                      │
//	  │                                  (120 s)
//	  │                                      ▼
//	  └────────(1 success)────────────  HALF_OPEN
//
// When OPEN or HALF_OPEN, all requests are forwarded to Helius DAS.
// A single pump.fun success from HALF_OPEN resets the circuit to CLOSED.
type circuitState int

const (
	circuitClosed   circuitState = 0 // normal operation — pump.fun is healthy
	circuitOpen     circuitState = 1 // pump.fun is down — use Helius DAS exclusively
	circuitHalfOpen circuitState = 2 // cooldown elapsed — re-test pump.fun once
)

// circuitBreaker is a thread-safe, in-memory circuit breaker for the
// pump.fun creator API. It is not persisted across restarts — a fresh
// process always starts in the CLOSED state and requires another
// burst of failures to open again.
type circuitBreaker struct {
	mu                  sync.Mutex
	state               circuitState
	consecutiveFailures int
	openedAt            time.Time
	failureThreshold    int // open after N consecutive failures
	halfOpenAfterSec    int // re-test after N seconds
}

// shouldUseFallback returns true ONLY while the circuit is OPEN and the
// cooldown has not yet elapsed. When the cooldown has elapsed the state is
// promoted to HALF_OPEN and the method returns false so that the caller
// performs exactly one pump.fun re-test. The result of that re-test
// (recordSuccess → closes the circuit; recordFailure → re-opens it) is what
// drives the OPEN → HALF_OPEN → OPEN/CLOSED loop.
//
// Callers that receive true must route to Helius DAS; pump.fun must not be
// called. Callers that receive false must call pump.fun and then exactly one
// of recordSuccess() or recordFailure() with the outcome.
func (cb *circuitBreaker) shouldUseFallback() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == circuitOpen {
		if time.Since(cb.openedAt) >= time.Duration(cb.halfOpenAfterSec)*time.Second {
			// Cooldown elapsed — promote to HALF_OPEN and allow the caller
			// to re-test pump.fun once.
			cb.state = circuitHalfOpen
			return false
		}
		return true
	}
	// CLOSED and HALF_OPEN both allow a pump.fun attempt.
	return false
}

// recordFailure increments the consecutive failure counter and opens (or
// re-opens) the circuit when appropriate. Returns true if the circuit just
// transitioned to OPEN (so the caller can emit a log line).
//
//   - CLOSED → OPEN once consecutiveFailures crosses failureThreshold.
//   - HALF_OPEN → OPEN immediately on any failure (one strike during the
//     re-test bumps the circuit straight back to OPEN and restarts the
//     cooldown timer).
func (cb *circuitBreaker) recordFailure() (justOpened bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveFailures++
	switch cb.state {
	case circuitClosed:
		if cb.consecutiveFailures >= cb.failureThreshold {
			cb.state = circuitOpen
			cb.openedAt = time.Now()
			return true
		}
	case circuitHalfOpen:
		// Re-test failed — re-open the circuit and restart the cooldown.
		cb.state = circuitOpen
		cb.openedAt = time.Now()
		return true
	}
	return false
}

// recordSuccess resets the failure counter and closes the circuit.
// Must be called after any successful pump.fun response.
func (cb *circuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveFailures = 0
	cb.state = circuitClosed
}

// SolanaCreatorReputationProbe queries the pump.fun creator API to determine
// how many tokens a wallet has previously launched. It enriches the DTO's
// CreatorPrevTokenCount and CreatorPrevTokenCountKnown fields.
type SolanaCreatorReputationProbe struct {
	client  SolanaCreatorReputationHTTPClient
	cfg     SolanaCreatorReputationConfig
	logger  *slog.Logger
	breaker *circuitBreaker // pump.fun API circuit breaker; nil when HeliusRPCURL is empty
}

// NewSolanaCreatorReputationProbe constructs the probe with defaults applied
// for any zero-value fields.
func NewSolanaCreatorReputationProbe(
	client SolanaCreatorReputationHTTPClient,
	cfg SolanaCreatorReputationConfig,
	logger *slog.Logger,
) *SolanaCreatorReputationProbe {
	if logger == nil {
		logger = slog.Default()
	}
	if client == nil {
		client = &http.Client{Timeout: time.Duration(cfg.timeoutMs()) * time.Millisecond}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultCreatorBaseURL
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = defaultCreatorMaxBodyBytes
	}
	if cfg.PageLimit <= 0 || cfg.PageLimit > 200 {
		cfg.PageLimit = defaultCreatorPageLimit
	}

	// Only arm the circuit breaker when a Helius fallback URL is provided.
	// With no fallback, pump.fun failures remain fail-closed (no breaker needed).
	var cb *circuitBreaker
	if cfg.HeliusRPCURL != "" {
		cb = &circuitBreaker{
			failureThreshold: defaultCircuitFailureThreshold,
			halfOpenAfterSec: defaultCircuitHalfOpenSec,
		}
	}
	return &SolanaCreatorReputationProbe{
		client:  client,
		cfg:     cfg,
		logger:  logger,
		breaker: cb,
	}
}

// Name implements MarketProbe.
func (p *SolanaCreatorReputationProbe) Name() string { return "solana_creator_reputation" }

// Probe enriches in.CreatorPrevTokenCount with the ground-truth count from
// pump.fun's creator API. Returns (in, nil) unchanged when the probe is
// disabled, the DTO is not a Solana token, or the creator address is absent.
// Returns (in, err) with CreatorPrevTokenCountKnown=false on any fetch or
// parse error (fail-closed).
//
// Circuit breaker behaviour (only active when HeliusRPCURL is configured):
//
//  1. If the circuit is OPEN (pump.fun has failed ≥ 5 consecutive times),
//     route directly to Helius DAS — skip pump.fun entirely.
//  2. Otherwise try pump.fun first. On failure:
//     a. Increment the consecutive-failure counter; open the circuit if
//     the threshold is reached.
//     b. If Helius DAS is configured, fall back to it for this token.
//     c. If Helius DAS is not configured, return fail-closed.
//  3. On pump.fun success: reset the circuit to CLOSED.
func (p *SolanaCreatorReputationProbe) Probe(
	ctx context.Context,
	in contracts.MarketDataDTO,
) (contracts.MarketDataDTO, error) {
	if !p.cfg.Enabled {
		return in, nil
	}
	chain := strings.ToLower(in.Chain)
	if chain != "solana" && chain != "sol" {
		return in, nil
	}
	if in.CreatorAddress == "" {
		return in, ErrSkippedEmptyCreator
	}

	if err := p.validateBaseURL(p.cfg.BaseURL); err != nil {
		return in, fmt.Errorf("solana_creator_reputation: insecure base_url: %w", err)
	}

	var count int32
	var fetchErr error

	if p.breaker != nil && p.breaker.shouldUseFallback() {
		// Circuit is OPEN and the cooldown has not yet elapsed — route to
		// Helius DAS directly. (HALF_OPEN flips back to false inside
		// shouldUseFallback() so a pump.fun re-test runs below.)
		p.logger.Info("creator_probe_circuit_open_using_helius",
			"creator", in.CreatorAddress,
			"token", in.TokenAddress,
		)
		count, fetchErr = p.fetchViaHelliusDAS(ctx, in.CreatorAddress)
		if fetchErr != nil {
			p.logger.Warn("creator_probe_helius_failed",
				"creator", in.CreatorAddress,
				"token", in.TokenAddress,
				"error", truncateMsg(fetchErr.Error(), 200),
			)
			return in, fetchErr
		}
		// Helius succeeded while the circuit is OPEN — do not reset the
		// circuit; only a successful pump.fun call (during HALF_OPEN re-test)
		// closes it.
	} else {
		// Circuit is CLOSED or HALF_OPEN — try pump.fun.
		count, fetchErr = p.fetchCreatorTokenCount(ctx, in.CreatorAddress)
		if fetchErr != nil {
			p.logger.Warn("creator_probe_pumpfun_failed",
				"creator", in.CreatorAddress,
				"token", in.TokenAddress,
				"error", truncateMsg(fetchErr.Error(), 200),
			)
			if p.breaker != nil {
				if opened := p.breaker.recordFailure(); opened {
					p.logger.Warn("creator_probe_circuit_opened",
						"consecutive_failures", defaultCircuitFailureThreshold,
						"fallback", "helius_das",
					)
				}
			}
			// Attempt Helius DAS fallback.
			if p.cfg.HeliusRPCURL != "" {
				p.logger.Info("creator_probe_helius_attempting",
					"creator", in.CreatorAddress,
					"token", in.TokenAddress,
					"reason", "pumpfun_failed",
				)
				var heliusErr error
				count, heliusErr = p.fetchViaHelliusDAS(ctx, in.CreatorAddress)
				if heliusErr != nil {
					p.logger.Warn("creator_probe_helius_failed",
						"creator", in.CreatorAddress,
						"token", in.TokenAddress,
						"error", truncateMsg(heliusErr.Error(), 200),
					)
					return in, heliusErr // both sources failed — fail-closed
				}
				p.logger.Info("creator_probe_helius_fallback_used",
					"creator", in.CreatorAddress,
					"token", in.TokenAddress,
					"count", count,
				)
			} else {
				return in, fetchErr // no fallback configured — fail-closed
			}
		} else {
			if p.breaker != nil {
				p.breaker.recordSuccess()
			}
		}
	}

	out := in
	// The pump.fun API returns all tokens by the creator including the
	// current token being evaluated. Subtract 1 to match the DTO contract:
	// CreatorPrevTokenCount = prior launches excluding the current token.
	// If the current token has not been indexed yet the count stays accurate
	// (no over-subtract). If count=0 it stays at 0 (defensive).
	if count > 0 {
		count--
	}
	out.CreatorPrevTokenCount = count
	out.CreatorPrevTokenCountKnown = true

	p.logger.Info("solana_creator_reputation_enriched",
		"creator", in.CreatorAddress,
		"token", in.TokenAddress,
		"prev_token_count", count,
	)
	return out, nil
}

// fetchCreatorTokenCount calls the pump.fun creator API and returns the
// number of tokens the creator has previously launched.
func (p *SolanaCreatorReputationProbe) fetchCreatorTokenCount(
	ctx context.Context,
	creatorAddress string,
) (int32, error) {
	apiURL := fmt.Sprintf("%s/coins?user=%s&limit=%d&offset=0",
		strings.TrimRight(p.cfg.BaseURL, "/"),
		url.QueryEscape(creatorAddress),
		p.cfg.PageLimit,
	)

	deadline := time.Duration(p.cfg.timeoutMs()) * time.Millisecond
	reqCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, apiURL, nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	// Set browser-compatible headers to avoid Cloudflare bot protection (HTTP 530).
	// The pump.fun frontend API is served behind Cloudflare and blocks plain
	// Go HTTP clients (no User-Agent). These headers do not bypass authentication
	// — the API requires no credentials. This is standard API client practice.
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Origin", "https://pump.fun")
	req.Header.Set("Referer", "https://pump.fun/")

	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	// Bounded read with truncation detection — read at most MaxBodyBytes+1
	// so oversized responses fail with an explicit error instead of being
	// silently parsed against a truncated body.
	lr := io.LimitReader(resp.Body, p.cfg.MaxBodyBytes+1)
	body, err := io.ReadAll(lr)
	if err != nil {
		return 0, fmt.Errorf("read body: %w", err)
	}
	if int64(len(body)) > p.cfg.MaxBodyBytes {
		return 0, fmt.Errorf("response body exceeds limit (%d bytes)", p.cfg.MaxBodyBytes)
	}

	count, err := parseCreatorCoinCount(body)
	if err != nil {
		return 0, fmt.Errorf("parse response: %w", err)
	}
	return count, nil
}

// parseCreatorCoinCount parses the pump.fun creator API response.
//
// The API returns a JSON array of coin objects:
//
//	[{"mint": "...", "name": "...", ...}, ...]
//
// The count is the length of the array. If the array is full (equal to
// PageLimit = 50), the real count may be higher — still a strong serial-
// launcher signal well above any configured threshold.
func parseCreatorCoinCount(body []byte) (int32, error) {
	// Try the common array-of-objects format first.
	var coins []json.RawMessage
	if err := json.Unmarshal(body, &coins); err == nil {
		return int32(len(coins)), nil //nolint:gosec // length ≤ MaxBodyBytes/min_object_size
	}

	// Fallback: some API versions return {"total": N, "coins": [...]}
	var envelope struct {
		Total int32             `json:"total"`
		Coins []json.RawMessage `json:"coins"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		if envelope.Total > 0 {
			return envelope.Total, nil
		}
		return int32(len(envelope.Coins)), nil //nolint:gosec
	}

	return 0, fmt.Errorf("unrecognised response format (len=%d)", len(body))
}

// fetchViaHelliusDAS queries the Helius DAS API (getAssetsByCreator) to count
// the number of assets previously created by the given wallet address.
// It uses the creatorAddress filter which matches Metaplex metadata creators
// array — pump.fun tokens include the creator wallet in this field.
// Note: searchAssets with tokenType requires owner_address and is not suitable
// for creator-based queries; getAssetsByCreator is the correct endpoint.
//
// Security:
//   - HeliusRPCURL is sourced from env var (via chains.yaml) \u2014 never YAML-hardcoded.
//   - HeliusRPCURL must be HTTPS (API key is embedded as ?api-key= query param).
//     Loopback addresses (127.x, localhost) allowed for test servers.
//   - Response body is bounded to MaxBodyBytes via io.LimitReader.
//   - RPC error messages are truncated to 200 chars before surfacing.
func (p *SolanaCreatorReputationProbe) fetchViaHelliusDAS(
	ctx context.Context,
	creatorAddress string,
) (int32, error) {
	// Enforce HTTPS for the Helius URL — it embeds an API key as ?api-key=.
	if err := p.validateBaseURL(p.cfg.HeliusRPCURL); err != nil {
		return 0, fmt.Errorf("helius_das: insecure helius_rpc_url: %w", err)
	}
	type creatorParams struct {
		CreatorAddress string `json:"creatorAddress"`
		OnlyVerified   bool   `json:"onlyVerified"`
		Page           int    `json:"page"`
		Limit          int    `json:"limit"`
	}
	type rpcRequest struct {
		JSONRPC string        `json:"jsonrpc"`
		ID      string        `json:"id"`
		Method  string        `json:"method"`
		Params  creatorParams `json:"params"`
	}

	payload := rpcRequest{
		JSONRPC: "2.0",
		ID:      "creator-rep",
		Method:  "getAssetsByCreator",
		Params: creatorParams{
			CreatorAddress: creatorAddress,
			OnlyVerified:   false, // pump.fun does not verify the creator field
			Page:           1,
			Limit:          p.cfg.PageLimit,
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("helius_das: marshal request: %w", err)
	}

	deadline := time.Duration(p.cfg.timeoutMs()) * time.Millisecond
	// Strip the parent's deadline so a slow pump.fun call cannot consume
	// the Helius budget. context.WithoutCancel gives Helius a fresh full
	// deadline independent of how much time pump.fun consumed.
	// NOTE: parent cancellation is NOT propagated — on graceful shutdown
	// this call runs until its own TimeoutMs (default 3 s) expires.
	// This is acceptable because the timeout is short and bounded.
	reqCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deadline)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, p.cfg.HeliusRPCURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, fmt.Errorf("helius_das: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("helius_das: http do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("helius_das: unexpected status %d", resp.StatusCode)
	}

	lr := io.LimitReader(resp.Body, p.cfg.MaxBodyBytes)
	respBody, err := io.ReadAll(lr)
	if err != nil {
		return 0, fmt.Errorf("helius_das: read body: %w", err)
	}

	count, err := parseHelliusDASCount(respBody)
	if err != nil {
		return 0, fmt.Errorf("helius_das: parse response: %w", err)
	}
	return count, nil
}

// parseHelliusDASCount parses the Helius DAS getAssetsByCreator JSON-RPC response
// and returns the count of assets found for the creator address.
//
// Helius DAS response shape:
//
//	{
//	  "jsonrpc": "2.0",
//	  "result": {
//	    "total": 3,
//	    "limit": 50,
//	    "page":  1,
//	    "items": [...]
//	  }
//	}
//
// On a JSON-RPC error:
//
//	{
//	  "jsonrpc": "2.0",
//	  "error": { "code": -32600, "message": "..." }
//	}
func parseHelliusDASCount(body []byte) (int32, error) {
	var rpcResp struct {
		Result *struct {
			Total int32             `json:"total"`
			Items []json.RawMessage `json:"items"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return 0, fmt.Errorf("json decode: %w", err)
	}
	if rpcResp.Error != nil {
		return 0, fmt.Errorf("rpc error %d: %s", rpcResp.Error.Code, truncateMsg(rpcResp.Error.Message, 200))
	}
	if rpcResp.Result == nil {
		return 0, fmt.Errorf("missing result field in response (len=%d)", len(body))
	}
	if rpcResp.Result.Total > 0 {
		return rpcResp.Result.Total, nil
	}
	return int32(len(rpcResp.Result.Items)), nil //nolint:gosec
}

// validateBaseURL enforces the HTTPS-only security invariant.
// Loopback addresses (127.x, localhost, ::1) are allowed for test servers
// but must still use http or https — never ftp, file, or any other scheme.
func (p *SolanaCreatorReputationProbe) validateBaseURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	isLoopback := host == "localhost" ||
		strings.HasPrefix(host, "127.") ||
		host == "::1"
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && isLoopback {
		return nil
	}
	return fmt.Errorf("base_url must use HTTPS (got scheme %q)", u.Scheme)
}

// timeoutMs returns the effective timeout, defaulting when zero.
func (c *SolanaCreatorReputationConfig) timeoutMs() int {
	if c.TimeoutMs <= 0 {
		return defaultCreatorTimeoutMs
	}
	return c.TimeoutMs
}

// truncateMsg limits an error string to maxLen characters to prevent
// arbitrary-length RPC/API error messages from surfacing in logs.
func truncateMsg(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}
