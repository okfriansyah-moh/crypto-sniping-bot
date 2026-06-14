// Package probes — dexscreener_market_data probe.
// Populates MarketCapUsd, VolumeUsd5m, VolumeUsd1h, VolumeUsd24h on
// MarketDataDTO by querying the public DEXScreener REST API.
//
// Fail-open contract: on any HTTP or parse error the four fields remain
// 0.0 (their Task-6 zero-value sentinels) and the token is NOT rejected
// at this stage. Layer-1 DQ structural-reject filters guard on
// "threshold > 0 && field > 0" so zero fields are always inert.
//
// Security invariants preserved:
//   - HTTPS-only endpoint (https://api.dexscreener.com/…)
//   - Response body bounded by 128 KiB io.LimitReader
//   - url.PathEscape applied to the token address in the URL path
//
// This file does NOT import internal/rpc — it defines its own HTTP client
// field for testability (inject a *http.Client backed by httptest.Server
// in unit tests). This matches the pattern used by all other probes in
// this package.
package probes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"crypto-sniping-bot/shared/contracts"
)

const (
	// dexscreenerMarketDataBaseURL is the DEXScreener token-pairs endpoint.
	// HTTPS only — per security invariant.
	dexscreenerMarketDataBaseURL = "https://api.dexscreener.com/latest/dex/tokens/"

	// dexscreenerMaxBodyBytes caps the HTTP response body to 128 KiB.
	// Per security invariant: DEXScreener 128 KiB cap must not be raised.
	dexscreenerMaxBodyBytes = 128 * 1024

	// dexscreenerDefaultTimeoutMs is the default HTTP timeout in milliseconds.
	dexscreenerDefaultTimeoutMs = 5_000
)

// DEXScreenerMarketDataConfig configures the dexscreener_market_data probe.
type DEXScreenerMarketDataConfig struct {
	// Enabled toggles the probe. When false, Probe() returns the input
	// unchanged — MarketCapUsd and VolumeUsd* remain 0.0.
	Enabled bool `yaml:"enabled"`
	// TimeoutMs bounds the HTTP call. Defaults to 5000 (5 s) when 0.
	TimeoutMs int `yaml:"timeout_ms"`
}

// dexscreenerPairsResponse is the unmarshal target for the DEXScreener API.
// Only the fields required to populate the four MarketDataDTO fields are
// declared here; unknown JSON fields are silently ignored.
type dexscreenerPairsResponse struct {
	Pairs []struct {
		// FDV is the fully-diluted valuation in USD. Preferred over MarketCap.
		FDV float64 `json:"fdv"`
		// MarketCap is the circulating-supply market cap in USD.
		MarketCap float64 `json:"marketCap"`
		// Volume captures trading volume across time windows.
		Volume *struct {
			M5  float64 `json:"m5"`
			H1  float64 `json:"h1"`
			H24 float64 `json:"h24"`
		} `json:"volume"`
	} `json:"pairs"`
}

// DEXScreenerMarketDataProbe enriches MarketDataDTO with market-cap and
// volume data from the DEXScreener public API. It implements MarketProbe.
type DEXScreenerMarketDataProbe struct {
	httpClient *http.Client
	cfg        DEXScreenerMarketDataConfig
	logger     *slog.Logger
	// baseURL is the API base URL. Defaults to dexscreenerMarketDataBaseURL.
	// Overridable in tests to point at an httptest.Server.
	baseURL string
}

// NewDEXScreenerMarketDataProbe returns a probe with the given config.
// Pass a non-nil *http.Client to override the default (useful in tests).
// When httpClient is nil, a new client with cfg.TimeoutMs is created.
func NewDEXScreenerMarketDataProbe(httpClient *http.Client, cfg DEXScreenerMarketDataConfig, logger *slog.Logger) *DEXScreenerMarketDataProbe {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.TimeoutMs <= 0 {
		cfg.TimeoutMs = dexscreenerDefaultTimeoutMs
	}
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: time.Duration(cfg.TimeoutMs) * time.Millisecond,
		}
	}
	return &DEXScreenerMarketDataProbe{
		httpClient: httpClient,
		cfg:        cfg,
		logger:     logger,
		baseURL:    dexscreenerMarketDataBaseURL,
	}
}

// Name returns the stable probe identifier.
func (p *DEXScreenerMarketDataProbe) Name() string { return "dexscreener_market_data" }

// Probe queries DEXScreener and populates MarketCapUsd, VolumeUsd5m,
// VolumeUsd1h, and VolumeUsd24h on the returned DTO.
//
// Fail-open: on any error the four fields remain 0.0 and (in, err) is
// returned so the orchestrator can log the degraded probe result without
// rejecting the token.
func (p *DEXScreenerMarketDataProbe) Probe(ctx context.Context, in contracts.MarketDataDTO) (contracts.MarketDataDTO, error) {
	if !p.cfg.Enabled {
		return in, nil
	}
	if in.TokenAddress == "" {
		return in, nil
	}

	// Construct URL with safe path segment. PathEscape encodes '/' and other
	// characters that would corrupt the API path.
	apiURL := p.baseURL + url.PathEscape(in.TokenAddress)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return in, fmt.Errorf("dexscreener_market_data: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "crypto-sniping-bot/1.0")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return in, fmt.Errorf("dexscreener_market_data: http: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return in, fmt.Errorf("dexscreener_market_data: status %d for %s", resp.StatusCode, in.TokenAddress)
	}

	// Bound body read to 128 KiB — security invariant must not be raised.
	body, err := io.ReadAll(io.LimitReader(resp.Body, dexscreenerMaxBodyBytes))
	if err != nil {
		return in, fmt.Errorf("dexscreener_market_data: read body: %w", err)
	}

	md, err := parseDexscreenerMarketData(body)
	if err != nil {
		return in, fmt.Errorf("dexscreener_market_data: parse: %w", err)
	}

	if md.marketCapUsd == 0 && md.volumeUsd5m == 0 && md.volumeUsd1h == 0 && md.volumeUsd24h == 0 {
		// Token not yet indexed or has zero data — fields remain 0.0. Not an error.
		p.logger.Debug("dexscreener_market_data_not_indexed",
			"token", in.TokenAddress,
			"chain", in.Chain,
		)
		return in, nil
	}

	// Copy the input DTO and populate the four fields.
	out := in
	out.MarketCapUsd = md.marketCapUsd
	out.VolumeUsd5m = md.volumeUsd5m
	out.VolumeUsd1h = md.volumeUsd1h
	out.VolumeUsd24h = md.volumeUsd24h

	p.logger.Debug("dexscreener_market_data_populated",
		"token", in.TokenAddress,
		"chain", in.Chain,
		"market_cap_usd", out.MarketCapUsd,
		"volume_usd_1h", out.VolumeUsd1h,
	)
	return out, nil
}

// dexscreenerParsed holds the extracted values from one DEXScreener response.
type dexscreenerParsed struct {
	marketCapUsd float64
	volumeUsd5m  float64
	volumeUsd1h  float64
	volumeUsd24h float64
}

// parseDexscreenerMarketData unmarshals the response body and extracts the
// market-cap and volume fields from the first pair.
// Returns zero values when no pairs are present (not an error).
func parseDexscreenerMarketData(body []byte) (dexscreenerParsed, error) {
	var r dexscreenerPairsResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return dexscreenerParsed{}, fmt.Errorf("json unmarshal: %w", err)
	}
	if len(r.Pairs) == 0 {
		return dexscreenerParsed{}, nil // not indexed — zero values, no error
	}
	pair := r.Pairs[0]

	// FDV is the preferred market-cap signal; fall back to MarketCap when zero.
	mcap := pair.FDV
	if mcap == 0 {
		mcap = pair.MarketCap
	}

	out := dexscreenerParsed{marketCapUsd: mcap}
	if pair.Volume != nil {
		out.volumeUsd5m = pair.Volume.M5
		out.volumeUsd1h = pair.Volume.H1
		out.volumeUsd24h = pair.Volume.H24
	}
	return out, nil
}
