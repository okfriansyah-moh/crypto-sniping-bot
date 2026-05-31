// Package rpc provides price fetching via the DEXScreener public API.
// This file implements position.PriceClient and learning.PriceClient.
package rpc

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
	dexScreenerBaseURL = "https://api.dexscreener.com/latest/dex/tokens/"
	priceClientTimeout = 5 * time.Second
	priceMaxBodyBytes  = 512 * 1024 // 512 KiB — DEXScreener responses are small
)

// DEXScreenerPriceClient implements position.PriceClient and
// learning.PriceClient by querying the DEXScreener public API.
//
// Endpoint: GET https://api.dexscreener.com/latest/dex/tokens/{tokenAddress}
// Returns the first pair's priceNative (price in chain-native token: SOL, ETH, BNB, …).
// This matches the price unit stored in PositionStateDTO.EntryPrice for all
// supported chains (Solana meme tokens: price in SOL; EVM: price in ETH/BNB).
//
// On error or empty response: returns ("", err). The callers (position monitor,
// shadow observer) treat empty string as "price unavailable" and skip the check
// rather than triggering a false exit. This is safe by design.
//
// Architecture note: this client is ONLY for live price polling (Layer 9
// position monitor and Layer 10 shadow observer). It is NOT a source of truth
// for execution pricing — on-chain getAmountsOut is used there.
type DEXScreenerPriceClient struct {
	httpClient *http.Client
	logger     *slog.Logger
}

// NewDEXScreenerPriceClient returns a new DEXScreenerPriceClient with a
// bounded timeout HTTP client.
func NewDEXScreenerPriceClient(logger *slog.Logger) *DEXScreenerPriceClient {
	if logger == nil {
		logger = slog.Default()
	}
	return &DEXScreenerPriceClient{
		httpClient: &http.Client{Timeout: priceClientTimeout},
		logger:     logger,
	}
}

// GetTokenPrice returns the current priceNative (price in chain-native token)
// for tokenAddress on the given chain.
//
// The chain parameter is accepted but not used in the DEXScreener query
// (DEXScreener resolves the chain from the token address). It is included for
// interface compatibility and logged for observability.
//
// Returns ("", nil) when the token has no known pairs (new/unindexed token).
// Returns ("", err) on HTTP or parse errors.
func (c *DEXScreenerPriceClient) GetTokenPrice(ctx context.Context, tokenAddress, chain string) (string, error) {
	if tokenAddress == "" {
		return "", fmt.Errorf("get token price: empty tokenAddress")
	}

	// Construct URL with safe path segment. PathEscape encodes '/' and other
	// special characters that would corrupt the API path.
	apiURL := dexScreenerBaseURL + url.PathEscape(tokenAddress)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("get token price: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "crypto-sniping-bot/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("get token price: http: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get token price: dexscreener returned %d for %s", resp.StatusCode, tokenAddress)
	}

	// Bound body read to prevent unbounded memory consumption.
	body, err := io.ReadAll(io.LimitReader(resp.Body, priceMaxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("get token price: read body: %w", err)
	}

	price, err := parseDEXScreenerPrice(body)
	if err != nil {
		return "", fmt.Errorf("get token price: parse: %w", err)
	}

	c.logger.Debug("price_fetched",
		"token", tokenAddress,
		"chain", chain,
		"price_native", price,
	)
	return price, nil
}

// dexScreenerResponse is an unmarshal target for the DEXScreener
// GET /latest/dex/tokens/{address} response.
//
// Full schema: https://docs.dexscreener.com/api/reference
// Fields captured: priceNative (L9 position monitor), fdv/marketCap, and
// volume (m5/h1/h6/h24) — used for observability and the L1 DQ probe.
type dexScreenerResponse struct {
	Pairs []struct {
		PriceNative string `json:"priceNative"`
		// MarketCap is the token's circulating-supply market cap in USD as
		// reported by DEXScreener. Zero when the token is not yet indexed.
		MarketCap float64 `json:"marketCap"`
		// FDV is the fully-diluted valuation in USD. Used as primary market-cap
		// signal; falls back to MarketCap when FDV is zero.
		FDV       float64 `json:"fdv"`
		Liquidity *struct {
			USD float64 `json:"usd"`
		} `json:"liquidity"`
		// Volume captures the trading volume in USD across multiple time windows.
		Volume *struct {
			M5  float64 `json:"m5"`
			H1  float64 `json:"h1"`
			H6  float64 `json:"h6"`
			H24 float64 `json:"h24"`
		} `json:"volume"`
	} `json:"pairs"`
}

// dexScreenerMarketData holds the market-cap and volume snapshot extracted from
// a DEXScreener response. Zero values mean the token is not yet indexed.
type dexScreenerMarketData struct {
	// MarketCapUsd is FDV when available, falling back to MarketCap.
	MarketCapUsd float64
	VolumeUsd5m  float64
	VolumeUsd1h  float64
	VolumeUsd24h float64
}

// parseDEXScreenerMarketData extracts market-cap and volume from the first pair.
// Returns a zero-value struct when no pairs are present — this is not an error;
// it means the token is not yet indexed by DEXScreener.
func parseDEXScreenerMarketData(body []byte) (dexScreenerMarketData, error) {
	var r dexScreenerResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return dexScreenerMarketData{}, fmt.Errorf("json unmarshal: %w", err)
	}
	if len(r.Pairs) == 0 {
		return dexScreenerMarketData{}, nil // not indexed yet — safe zero
	}
	p := r.Pairs[0]
	// FDV is the preferred market-cap signal; fall back to MarketCap when zero.
	mcap := p.FDV
	if mcap == 0 {
		mcap = p.MarketCap
	}
	md := dexScreenerMarketData{MarketCapUsd: mcap}
	if p.Volume != nil {
		md.VolumeUsd5m = p.Volume.M5
		md.VolumeUsd1h = p.Volume.H1
		md.VolumeUsd24h = p.Volume.H24
	}
	return md, nil
}

// parseDEXScreenerPrice extracts the first non-empty, non-zero priceNative
// from the pair list.
//
// Returns ("", nil) when no pairs are found — this is not an error; it means
// the token has not yet been indexed by DEXScreener.
func parseDEXScreenerPrice(body []byte) (string, error) {
	var r dexScreenerResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("json unmarshal: %w", err)
	}
	if len(r.Pairs) == 0 {
		return "", nil // not indexed yet — safe non-error empty
	}

	// Prefer the first valid priceNative entry. Pair ordering is upstream-defined,
	// and this parser intentionally stays deterministic and allocation-free.
	for _, pair := range r.Pairs {
		if pair.PriceNative != "" && pair.PriceNative != "0" {
			return pair.PriceNative, nil
		}
	}
	return "", nil // all pairs have zero/missing price — treat as unavailable
}
