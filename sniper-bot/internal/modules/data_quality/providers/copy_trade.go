// Package providers — Copy-trading signal amplifier (P8).
//
// CopyTradeProvider checks whether any watched "alpha wallet" address has
// recently traded a given token. When an alpha wallet is detected as a buyer,
// the risk signal drops (higher trader confidence = lower risk contribution).
//
// Alpha wallet list is loaded from the COPY_TRADE_WALLETS environment variable
// as a comma-separated list of base58 (Solana) or hex (EVM) addresses.
// The list is never stored in YAML, never logged.
//
// Signal logic (returned as risk score — lower = better quality):
//
//	No alpha wallet traded → score = 0.5  (neutral / unknown)
//	≥1 alpha wallet traded → score = 0.0  (positive confirmation, reduce risk)
//	No wallet list configured → Degraded = true (fail-open)
//
// The provider queries the DEXScreener public transactions API, which is
// free and requires no API key. Chain-agnostic.
//
// Architecture:
//   - Fail-open: any error → Degraded=true, score=0, pipeline continues.
//   - Response body capped at 128 KiB.
//   - Timeout enforced via parent context.
//   - Boots with shadow_mode: true in config — flip after validation.
//   - Wallet list is NEVER logged, printed, or stored.
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	copyTradeBodyLimitBytes = 128 * 1024 // 128 KiB
	copyTradeWalletsEnv     = "COPY_TRADE_WALLETS"
	copyTradeRecentTxWindow = 100 // recent transactions to scan per token
)

// CopyTradeProvider monitors alpha wallets and emits a confidence signal.
type CopyTradeProvider struct {
	client       *http.Client
	logger       *slog.Logger
	baseURL      string   // injectable for tests
	alphaWallets []string // lowercased wallet addresses, never logged
}

// NewCopyTradeProvider returns a CopyTradeProvider.
// Alpha wallets are loaded from the COPY_TRADE_WALLETS env var (comma-separated).
// If none are configured the provider returns Degraded=true on every call.
func NewCopyTradeProvider(logger *slog.Logger) *CopyTradeProvider {
	if logger == nil {
		logger = slog.Default()
	}
	raw := os.Getenv(copyTradeWalletsEnv)
	var wallets []string
	if raw != "" {
		parts := strings.Split(raw, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				wallets = append(wallets, strings.ToLower(p))
			}
		}
	}
	if len(wallets) == 0 {
		logger.Warn("copy_trade_no_wallets",
			"env", copyTradeWalletsEnv,
			"hint", "set COPY_TRADE_WALLETS=addr1,addr2 to enable this provider",
		)
	} else {
		logger.Info("copy_trade_provider_loaded", "wallet_count", len(wallets))
	}
	return &CopyTradeProvider{
		client: &http.Client{
			Timeout: 280 * time.Millisecond,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout:     30 * time.Second,
				DisableKeepAlives:   false,
			},
		},
		logger:       logger,
		baseURL:      "https://api.dexscreener.com",
		alphaWallets: wallets,
	}
}

// Name returns the canonical provider name.
func (p *CopyTradeProvider) Name() string { return "copy_trade" }

// SetBaseURLForTest overrides the API base URL. For unit tests only.
func (p *CopyTradeProvider) SetBaseURLForTest(url string) { p.baseURL = url }

// Evaluate checks whether an alpha wallet has recently traded tokenAddress.
// Returns score = 0.0 (risk reduction) when a match is found, or
// score = 0.5 (neutral) when no alpha wallet activity is detected.
func (p *CopyTradeProvider) Evaluate(ctx context.Context, tokenAddress, chain string) (DQSignalDTO, error) {
	if len(p.alphaWallets) == 0 {
		return DQSignalDTO{
			ProviderName: p.Name(),
			Score:        0,
			Flags:        []string{"copy_trade_no_wallets"},
			Degraded:     true,
		}, nil
	}

	txs, err := p.fetchRecentTxs(ctx, tokenAddress, chain)
	if err != nil {
		p.logger.Warn("copy_trade_fetch_error", "error", err, "token", tokenAddress, "chain", chain)
		return DQSignalDTO{
			ProviderName: p.Name(),
			Score:        0,
			Flags:        []string{"copy_trade_degraded"},
			Degraded:     true,
		}, nil
	}

	for _, tx := range txs {
		maker := strings.ToLower(tx.Maker)
		for _, wallet := range p.alphaWallets {
			if maker == wallet {
				p.logger.Debug("copy_trade_alpha_match",
					"token", tokenAddress,
					"chain", chain,
					// wallet address intentionally omitted from logs
				)
				return DQSignalDTO{
					ProviderName: p.Name(),
					Score:        0.0, // alpha confirmed — lower risk
					Flags:        []string{"copy_trade_alpha_match"},
					Degraded:     false,
				}, nil
			}
		}
	}

	return DQSignalDTO{
		ProviderName: p.Name(),
		Score:        0.5, // neutral — no alpha wallet activity detected
		Flags:        []string{"copy_trade_no_match"},
		Degraded:     false,
	}, nil
}

// dexScreenerTx is a single transaction entry from the DEXScreener API.
type dexScreenerTx struct {
	Maker string `json:"maker"`
	Type  string `json:"type"` // "buy" | "sell"
}

// dexScreenerTxResponse is the DEXScreener /tokens/{chain}/{address}/txns response.
type dexScreenerTxResponse struct {
	SchemaVersion string `json:"schemaVersion"`
	Txns          []struct {
		Items []dexScreenerTx `json:"items"`
	} `json:"txns"`
}

// fetchRecentTxs fetches the most recent transactions for tokenAddress on chain
// using the DEXScreener public API.
func (p *CopyTradeProvider) fetchRecentTxs(ctx context.Context, tokenAddress, chain string) ([]dexScreenerTx, error) {
	// Sanitise inputs — never pass raw user/event data into the URL without validation.
	if err := validateAddressToken(tokenAddress); err != nil {
		return nil, fmt.Errorf("copy_trade: invalid token address: %w", err)
	}
	dexChain, err := mapChainToDexScreener(chain)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/tokens/v1/%s/%s", p.baseURL, dexChain, tokenAddress)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("copy_trade: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "crypto-sniping-bot/1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("copy_trade: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // token not found — return empty, not an error
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("copy_trade: http status %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, copyTradeBodyLimitBytes)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("copy_trade: read body: %w", err)
	}

	var result dexScreenerTxResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("copy_trade: unmarshal: %w", err)
	}

	// Flatten all tx groups and cap at copyTradeRecentTxWindow entries.
	var txs []dexScreenerTx
	for _, group := range result.Txns {
		txs = append(txs, group.Items...)
		if len(txs) >= copyTradeRecentTxWindow {
			txs = txs[:copyTradeRecentTxWindow]
			break
		}
	}
	return txs, nil
}

// validateAddressToken prevents path-injection by rejecting characters that
// cannot appear in a valid Solana/EVM token address.
func validateAddressToken(addr string) error {
	if addr == "" {
		return fmt.Errorf("empty address")
	}
	if len(addr) > 128 {
		return fmt.Errorf("address too long: %d chars", len(addr))
	}
	for _, c := range addr {
		if !isAlphanumericOrDash(c) {
			return fmt.Errorf("invalid character %q in address", c)
		}
	}
	return nil
}

func isAlphanumericOrDash(c rune) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '_'
}

// mapChainToDexScreener converts internal chain names to DEXScreener chain slugs.
// Returns an error for unrecognized chain names (allowlist-only, no passthrough).
func mapChainToDexScreener(chain string) (string, error) {
	switch strings.ToLower(chain) {
	case "ethereum", "eth":
		return "ethereum", nil
	case "bsc", "bnb":
		return "bsc", nil
	case "solana", "sol":
		return "solana", nil
	case "base":
		return "base", nil
	default:
		return "", fmt.Errorf("copy_trade: unsupported chain %q", chain)
	}
}
