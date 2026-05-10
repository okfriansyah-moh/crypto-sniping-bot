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

// dexScreenerResponse is a minimal unmarshal target for the DEXScreener
// GET /latest/dex/tokens/{address} response.
//
// Full schema: https://docs.dexscreener.com/api/reference
// We only extract the first pair's priceNative; all other fields are ignored.
type dexScreenerResponse struct {
	Pairs []struct {
		PriceNative string `json:"priceNative"`
		Liquidity   *struct {
			USD float64 `json:"usd"`
		} `json:"liquidity"`
	} `json:"pairs"`
}

// parseDEXScreenerPrice extracts priceNative from the first pair with non-zero
// liquidity. Falls back to the first pair if no liquidity field is present.
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

	// Prefer the pair with the highest liquidity (first non-empty priceNative).
	// DEXScreener returns pairs ordered by liquidity descending on most chains,
	// so taking the first valid entry is an O(1) safe approximation.
	for _, pair := range r.Pairs {
		if pair.PriceNative != "" && pair.PriceNative != "0" {
			return pair.PriceNative, nil
		}
	}
	return "", nil // all pairs have zero/missing price — treat as unavailable
}
