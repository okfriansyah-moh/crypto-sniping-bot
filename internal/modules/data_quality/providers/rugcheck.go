// Package providers — rugcheck.xyz external data quality provider.
//
// RugCheckProvider calls https://api.rugcheck.xyz/v1/tokens/{mint}/report
// to retrieve a probabilistic risk score for Solana token mints.
//
// Safety rules:
//   - Solana-only: returns zero-score non-degraded for non-Solana chains.
//   - Response body bounded to 512 KiB to prevent memory abuse.
//   - HTTP timeout set under the 300 ms provider budget.
//   - 404 (token not yet indexed) is treated as degraded (unknown), NOT risky.
//   - No API key required (free public endpoint).
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

const (
	rugcheckBaseURL = "https://api.rugcheck.xyz/v1"
	rugcheckMaxBody = 512 * 1024 // 512 KiB
	rugcheckName    = "rugcheck"
)

// RugCheckProvider implements DataQualityProvider via the rugcheck.xyz public API.
// Supports Solana tokens only (EVM tokens receive a zero-score pass).
type RugCheckProvider struct {
	client          *http.Client
	logger          *slog.Logger
	supportedChains map[string]bool
	// baseURL can be overridden in tests. Defaults to rugcheckBaseURL.
	baseURL string
}

// rugcheckReportResponse mirrors the rugcheck.xyz v1 report JSON shape.
type rugcheckReportResponse struct {
	Mint   string             `json:"mint"`
	Score  float64            `json:"score"` // rugcheck native: higher = more risk
	Risks  []rugcheckRiskItem `json:"risks"`
	Rugged bool               `json:"rugged"` // already confirmed rugged
}

type rugcheckRiskItem struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Score int    `json:"score"`
	Level string `json:"level"` // "danger" | "warn" | "info"
}

// NewRugCheckProvider creates a RugCheckProvider with a bounded HTTP client.
// The client timeout is set to 280 ms — just under the typical 300 ms provider
// budget — so the context deadline fires before the OS-level TCP timeout.
func NewRugCheckProvider(logger *slog.Logger) *RugCheckProvider {
	if logger == nil {
		logger = slog.Default()
	}
	return &RugCheckProvider{
		client: &http.Client{
			Timeout: 280 * time.Millisecond,
		},
		logger:  logger,
		baseURL: rugcheckBaseURL,
		supportedChains: map[string]bool{
			"solana": true,
			"sol":    true,
		},
	}
}

// SetBaseURLForTest overrides the API base URL. For testing only.
// Production code must never call this method.
func (p *RugCheckProvider) SetBaseURLForTest(u string) { p.baseURL = u }

// Name implements DataQualityProvider.
func (p *RugCheckProvider) Name() string { return rugcheckName }

// Evaluate fetches the rugcheck.xyz risk report for the given token.
//
// For non-Solana chains, returns immediately with Score=0, Degraded=false
// (not a rug risk from this provider's perspective — chain not supported).
//
// HTTP 404: token not yet indexed → degraded=true, score=0 (unknown, not risky).
// Network/parse errors: returns degraded=true with the error for caller logging.
func (p *RugCheckProvider) Evaluate(ctx context.Context, tokenAddress, chain string) (DQSignalDTO, error) {
	if !p.supportedChains[chain] {
		return DQSignalDTO{ProviderName: rugcheckName, Score: 0, Degraded: false}, nil
	}

	endpoint := fmt.Sprintf("%s/tokens/%s/report",
		p.baseURL, url.PathEscape(tokenAddress))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return DQSignalDTO{ProviderName: rugcheckName, Degraded: true},
			fmt.Errorf("rugcheck build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "crypto-sniping-bot/1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		return DQSignalDTO{ProviderName: rugcheckName, Degraded: true},
			fmt.Errorf("rugcheck http: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode == http.StatusNotFound {
		// Not indexed yet — treat as incomplete data, not a risk signal.
		p.logger.Debug("rugcheck_not_indexed",
			"token", tokenAddress, "chain", chain)
		return DQSignalDTO{ProviderName: rugcheckName, Score: 0, Degraded: true}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return DQSignalDTO{ProviderName: rugcheckName, Degraded: true},
			fmt.Errorf("rugcheck unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, rugcheckMaxBody))
	if err != nil {
		return DQSignalDTO{ProviderName: rugcheckName, Degraded: true},
			fmt.Errorf("rugcheck read body: %w", err)
	}

	var report rugcheckReportResponse
	if err := json.Unmarshal(body, &report); err != nil {
		return DQSignalDTO{ProviderName: rugcheckName, Degraded: true},
			fmt.Errorf("rugcheck parse: %w", err)
	}

	// rugcheck.xyz native score range: [0, 100000]; higher = more risk.
	// Normalize to [0.0, 1.0] matching our convention.
	normalized := clampf(report.Score/100000.0, 0.0, 1.0)

	// Already-rugged tokens are maximum risk regardless of score.
	if report.Rugged {
		normalized = 1.0
	}

	// Collect danger/warn flags for observability.
	flags := make([]string, 0, len(report.Risks))
	for _, r := range report.Risks {
		if r.Level == "danger" || r.Level == "warn" {
			flags = append(flags, fmt.Sprintf("rugcheck:%s", r.Name))
		}
	}

	p.logger.Debug("rugcheck_evaluated",
		"token", tokenAddress,
		"chain", chain,
		"normalized_score", normalized,
		"rugged", report.Rugged,
		"flags", flags,
	)

	return DQSignalDTO{
		ProviderName: rugcheckName,
		Score:        normalized,
		Flags:        flags,
		Degraded:     false,
	}, nil
}

// clampf clamps v into [lo, hi].
func clampf(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
