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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"crypto-sniping-bot/contracts"
)

const (
	// defaultCreatorBaseURL is the pump.fun public creator API.
	// Returns a JSON array of coins created by the given wallet address.
	defaultCreatorBaseURL = "https://frontend-api.pump.fun"

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
	// Defaults to "https://frontend-api.pump.fun".
	BaseURL string `yaml:"base_url"`

	// MaxBodyBytes is the response body size cap (in bytes).
	// Defaults to 131072 (128 KiB).
	MaxBodyBytes int64 `yaml:"max_body_bytes"`

	// PageLimit is the ?limit= parameter sent to the API.
	// Defaults to 50. Values outside [1, 200] are clamped.
	PageLimit int `yaml:"page_limit"`
}

// SolanaCreatorReputationProbe queries the pump.fun creator API to determine
// how many tokens a wallet has previously launched. It enriches the DTO's
// CreatorPrevTokenCount and CreatorPrevTokenCountKnown fields.
type SolanaCreatorReputationProbe struct {
	client SolanaCreatorReputationHTTPClient
	cfg    SolanaCreatorReputationConfig
	logger *slog.Logger
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
	return &SolanaCreatorReputationProbe{
		client: client,
		cfg:    cfg,
		logger: logger,
	}
}

// Name implements MarketProbe.
func (p *SolanaCreatorReputationProbe) Name() string { return "solana_creator_reputation" }

// Probe enriches in.CreatorPrevTokenCount with the ground-truth count from
// pump.fun's creator API. Returns (in, nil) unchanged when the probe is
// disabled, the DTO is not a Solana token, or the creator address is absent.
// Returns (in, err) with CreatorPrevTokenCountKnown=false on any fetch or
// parse error (fail-closed).
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
		return in, nil
	}

	if err := p.validateBaseURL(p.cfg.BaseURL); err != nil {
		return in, fmt.Errorf("solana_creator_reputation: insecure base_url: %w", err)
	}

	count, err := p.fetchCreatorTokenCount(ctx, in.CreatorAddress)
	if err != nil {
		p.logger.Warn("solana_creator_reputation_fetch_failed",
			"creator", in.CreatorAddress,
			"token", in.TokenAddress,
			"error", truncateMsg(err.Error(), 200),
		)
		// Fail-closed: caller must leave CreatorPrevTokenCountKnown=false.
		return in, err
	}

	out := in
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
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	// Bounded read — never io.ReadAll on an unbounded body.
	lr := io.LimitReader(resp.Body, p.cfg.MaxBodyBytes)
	body, err := io.ReadAll(lr)
	if err != nil {
		return 0, fmt.Errorf("read body: %w", err)
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

// validateBaseURL enforces the HTTPS-only security invariant.
// Loopback addresses (127.x, localhost) are allowed for test servers.
func (p *SolanaCreatorReputationProbe) validateBaseURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	isLoopback := host == "localhost" ||
		strings.HasPrefix(host, "127.") ||
		host == "::1"
	if u.Scheme == "https" || isLoopback {
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
