package providers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-sniping-bot/internal/modules/data_quality/providers"
)

// rugcheckTestReport builds a minimal JSON response for the mock server.
func rugcheckTestReport(t *testing.T, score float64, rugged bool, risks []map[string]interface{}) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{
		"mint":   "TestMint111",
		"score":  score,
		"rugged": rugged,
		"risks":  risks,
	})
	if err != nil {
		t.Fatalf("marshal test report: %v", err)
	}
	return body
}

func TestRugCheckProvider_Name(t *testing.T) {
	p := providers.NewRugCheckProvider(nil)
	if p.Name() != "rugcheck" {
		t.Errorf("expected rugcheck, got %q", p.Name())
	}
}

func TestRugCheckProvider_NonSolanaChain_ZeroScoreNoDegradation(t *testing.T) {
	p := providers.NewRugCheckProvider(nil)

	for _, chain := range []string{"ethereum", "bsc", "polygon", "arbitrum", "evm"} {
		sig, err := p.Evaluate(context.Background(), "0xABC", chain)
		if err != nil {
			t.Errorf("chain %q: unexpected error: %v", chain, err)
		}
		if sig.Score != 0 {
			t.Errorf("chain %q: expected score 0.0, got %f", chain, sig.Score)
		}
		if sig.Degraded {
			t.Errorf("chain %q: expected Degraded=false for unsupported chain", chain)
		}
		if sig.ProviderName != "rugcheck" {
			t.Errorf("chain %q: wrong provider name %q", chain, sig.ProviderName)
		}
	}
}

func TestRugCheckProvider_NormalScore_Normalized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(rugcheckTestReport(t, 50000, false, nil))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	sig, err := p.Evaluate(context.Background(), "TestMint111", "solana")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sig.Degraded {
		t.Error("expected Degraded=false for valid response")
	}
	// 50000 / 100000 = 0.5 ± small float tolerance
	if sig.Score < 0.499 || sig.Score > 0.501 {
		t.Errorf("expected score ~0.5, got %f", sig.Score)
	}
}

func TestRugCheckProvider_RuggedToken_MaxScore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(rugcheckTestReport(t, 0, true, nil))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	sig, err := p.Evaluate(context.Background(), "RuggedMint", "solana")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sig.Score != 1.0 {
		t.Errorf("expected score 1.0 for rugged token, got %f", sig.Score)
	}
}

func TestRugCheckProvider_404_Degraded_ZeroScore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	sig, err := p.Evaluate(context.Background(), "UnknownMint", "solana")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sig.Degraded {
		t.Error("expected Degraded=true on HTTP 404")
	}
	if sig.Score != 0 {
		t.Errorf("expected score 0 on 404, got %f", sig.Score)
	}
}

func TestRugCheckProvider_5xx_Degraded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	sig, err := p.Evaluate(context.Background(), "ErrMint", "solana")
	if err == nil {
		t.Error("expected error for 5xx, got nil")
	}
	if !sig.Degraded {
		t.Error("expected Degraded=true on 5xx")
	}
}

func TestRugCheckProvider_DangerAndWarnFlags_Collected(t *testing.T) {
	risks := []map[string]interface{}{
		{"name": "FREEZE_AUTHORITY_ENABLED", "level": "danger", "score": 9000},
		{"name": "MINT_AUTHORITY_ENABLED", "level": "warn", "score": 5000},
		{"name": "LOW_HOLDERS", "level": "info", "score": 100}, // must NOT appear
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(rugcheckTestReport(t, 0, false, risks))
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	sig, err := p.Evaluate(context.Background(), "FlagMint", "solana")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]bool{
		"rugcheck:FREEZE_AUTHORITY_ENABLED": false,
		"rugcheck:MINT_AUTHORITY_ENABLED":   false,
	}
	for _, f := range sig.Flags {
		if _, ok := want[f]; ok {
			want[f] = true
		}
		if f == "rugcheck:LOW_HOLDERS" {
			t.Errorf("info-level flag should NOT appear in Flags: %q", f)
		}
	}
	for flag, seen := range want {
		if !seen {
			t.Errorf("expected flag %q not found in %v", flag, sig.Flags)
		}
	}
}

func TestRugCheckProvider_ContextCancelled_Degraded(t *testing.T) {
	// Server that blocks longer than test context deadline.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	sig, err := p.Evaluate(ctx, "SlowMint", "solana")
	if err == nil {
		t.Error("expected error from context cancellation, got nil")
	}
	if !sig.Degraded {
		t.Error("expected Degraded=true on timeout")
	}
}

func TestAggregator_ShadowMode_DoesNotAffectScore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(rugcheckTestReport(t, 100000, false, nil)) // max risk
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	entries := []providers.ProviderEntry{
		{Provider: p, Weight: 1.0, Enabled: true, ShadowMode: true},
	}
	agg := providers.NewAggregator(entries, 500, nil)

	result := agg.Evaluate(context.Background(), "MaxRiskMint", "solana")

	// Shadow mode: score must be 0.0 (not blended)
	if result.ExternalRiskScore != 0.0 {
		t.Errorf("shadow mode: expected ExternalRiskScore=0.0, got %f", result.ExternalRiskScore)
	}
	// But flags must still be collected from shadow providers.
	// The provider had no risks, so flags may be empty — that's fine.
}

func TestAggregator_DisabledProvider_Skipped(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	entries := []providers.ProviderEntry{
		{Provider: p, Weight: 1.0, Enabled: false, ShadowMode: false},
	}
	agg := providers.NewAggregator(entries, 500, nil)
	_ = agg.Evaluate(context.Background(), "SomeMint", "solana")

	if callCount != 0 {
		t.Errorf("disabled provider should not be called; got %d calls", callCount)
	}
}

func TestAggregator_AllProvidersDegraded_ZeroScore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	p := newTestProvider(t, srv.URL)
	entries := []providers.ProviderEntry{
		{Provider: p, Weight: 1.0, Enabled: true, ShadowMode: false},
	}
	agg := providers.NewAggregator(entries, 500, nil)
	result := agg.Evaluate(context.Background(), "DegradedMint", "solana")

	// Degraded provider should produce zero external score (fail-open).
	if result.ExternalRiskScore != 0 {
		t.Errorf("all degraded: expected score 0, got %f", result.ExternalRiskScore)
	}
	if !result.Degraded {
		t.Error("expected Degraded=true when provider returned 404")
	}
}

// newTestProvider creates a RugCheckProvider that targets the given test
// server URL instead of api.rugcheck.xyz. Since RugCheckProvider's baseURL
// is a package constant, we shadow it by creating a provider and patching
// only the HTTP client transport to redirect to our test server.
//
// Approach: use a custom http.Client whose Transport rewrites the host.
func newTestProvider(t *testing.T, serverURL string) *providers.RugCheckProvider {
	t.Helper()
	p := providers.NewRugCheckProvider(nil)
	p.SetBaseURLForTest(serverURL)
	return p
}
