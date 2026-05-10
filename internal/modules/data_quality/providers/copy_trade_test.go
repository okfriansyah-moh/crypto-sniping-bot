package providers_test

// copy_trade_test.go — unit tests for the CopyTradeProvider (P8).
// All tests use httptest.Server — no real network calls.
// Tests are deterministic and require no API keys.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-sniping-bot/internal/modules/data_quality/providers"
)

// buildCopyTradeServer returns a test server that simulates the DEXScreener
// transactions API. makerAddresses is the list of makers returned in the
// response.
func buildCopyTradeServer(t *testing.T, makerAddresses []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var items []map[string]interface{}
		for _, m := range makerAddresses {
			items = append(items, map[string]interface{}{"maker": m, "type": "buy"})
		}
		resp := map[string]interface{}{
			"schemaVersion": "1.0",
			"txns": []map[string]interface{}{
				{"items": items},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func newCopyTradeProviderWithURL(t *testing.T, baseURL string) *providers.CopyTradeProvider {
	t.Helper()
	p := providers.NewCopyTradeProvider(nil)
	p.SetBaseURLForTest(baseURL)
	return p
}

func TestCopyTradeProvider_Name(t *testing.T) {
	t.Setenv("COPY_TRADE_WALLETS", "wallet1")
	p := providers.NewCopyTradeProvider(nil)
	if got := p.Name(); got != "copy_trade" {
		t.Errorf("Name() = %q, want copy_trade", got)
	}
}

func TestCopyTradeProvider_NoWallets_ReturnsDegraded(t *testing.T) {
	t.Setenv("COPY_TRADE_WALLETS", "")
	p := providers.NewCopyTradeProvider(nil)
	result, _ := p.Evaluate(context.Background(), "TokenAddr123", "solana")
	if !result.Degraded {
		t.Fatal("expected Degraded=true when no wallets configured")
	}
}

func TestCopyTradeProvider_AlphaWalletMatch_ScoreZero(t *testing.T) {
	alphaWallet := "AlphaWallet111111111111111111111111111111111"
	t.Setenv("COPY_TRADE_WALLETS", alphaWallet)

	srv := buildCopyTradeServer(t, []string{alphaWallet, "OtherWallet222"})
	defer srv.Close()

	p := providers.NewCopyTradeProvider(nil)
	p.SetBaseURLForTest(srv.URL)

	result, err := p.Evaluate(context.Background(), "TokenAddr123", "solana")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Degraded {
		t.Fatal("expected Degraded=false on successful fetch")
	}
	if result.Score != 0.0 {
		t.Errorf("Score = %.2f, want 0.0 (alpha match reduces risk)", result.Score)
	}
	if result.ProviderName != "copy_trade" {
		t.Errorf("ProviderName = %q, want copy_trade", result.ProviderName)
	}
}

func TestCopyTradeProvider_NoAlphaMatch_ScoreNeutral(t *testing.T) {
	t.Setenv("COPY_TRADE_WALLETS", "AlphaWallet111111111111111111111111111111111")

	srv := buildCopyTradeServer(t, []string{"OtherWallet222", "OtherWallet333"})
	defer srv.Close()

	p := providers.NewCopyTradeProvider(nil)
	p.SetBaseURLForTest(srv.URL)

	result, err := p.Evaluate(context.Background(), "TokenAddr123", "solana")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Degraded {
		t.Fatal("expected Degraded=false")
	}
	if result.Score != 0.5 {
		t.Errorf("Score = %.2f, want 0.5 (neutral when no alpha match)", result.Score)
	}
}

func TestCopyTradeProvider_CaseInsensitiveMatch(t *testing.T) {
	// Alpha wallet stored uppercase; API returns lowercase.
	t.Setenv("COPY_TRADE_WALLETS", "ALPHAWALLET111")

	srv := buildCopyTradeServer(t, []string{"alphawallet111"})
	defer srv.Close()

	p := providers.NewCopyTradeProvider(nil)
	p.SetBaseURLForTest(srv.URL)

	result, _ := p.Evaluate(context.Background(), "TokenAddr123", "ethereum")
	if result.Score != 0.0 {
		t.Errorf("expected case-insensitive match → Score=0.0, got %.2f", result.Score)
	}
}

func TestCopyTradeProvider_HTTPError_ReturnsDegraded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	t.Setenv("COPY_TRADE_WALLETS", "AlphaWallet111111111111111111111111111111111")

	p := providers.NewCopyTradeProvider(nil)
	p.SetBaseURLForTest(srv.URL)

	result, _ := p.Evaluate(context.Background(), "TokenAddr123", "solana")
	if !result.Degraded {
		t.Fatal("expected Degraded=true on HTTP 500")
	}
}

func TestCopyTradeProvider_InvalidTokenAddress_ReturnsDegraded(t *testing.T) {
	t.Setenv("COPY_TRADE_WALLETS", "AlphaWallet111111111111111111111111111111111")
	p := providers.NewCopyTradeProvider(nil)

	// Path-injection attempt — must be caught by validateAddressToken
	result, _ := p.Evaluate(context.Background(), "../../../etc/passwd", "solana")
	if !result.Degraded {
		t.Fatal("expected Degraded=true for invalid token address (path injection)")
	}
}

func TestCopyTradeProvider_UnknownChain_ReturnsDegraded(t *testing.T) {
	// MED-01: unknown chain should fail with Degraded rather than pass raw string into URL.
	t.Setenv("COPY_TRADE_WALLETS", "AlphaWallet111111111111111111111111111111111")
	p := providers.NewCopyTradeProvider(nil)

	result, _ := p.Evaluate(context.Background(), "TokenAddr123", "unknown-chain/../../inject")
	if !result.Degraded {
		t.Fatal("expected Degraded=true for unknown/invalid chain")
	}
}
