package probes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"crypto-sniping-bot/contracts"
)

// defaultCreatorCfg returns a config suitable for tests that point at a
// local httptest.Server (HTTP is allowed for loopback addresses).
func defaultCreatorCfg(baseURL string) SolanaCreatorReputationConfig {
	return SolanaCreatorReputationConfig{
		Enabled:      true,
		TimeoutMs:    500,
		BaseURL:      baseURL,
		MaxBodyBytes: defaultCreatorMaxBodyBytes,
		PageLimit:    50,
	}
}

func solanaCreatorDTO(creator string) contracts.MarketDataDTO {
	return contracts.MarketDataDTO{
		Chain:          "solana",
		TokenAddress:   "8poHAR4szrPjEcTYb72mDWKmqskeAKrZJLjbfPZdpum",
		CreatorAddress: creator,
	}
}

// ── Happy-path tests ──────────────────────────────────────────────────────────

func TestSolanaCreatorReputation_ArrayFormat21Tokens(t *testing.T) {
	// Simulate a dev with 21 prior tokens. The pump.fun API returns all tokens
	// including the current one being evaluated, so the array has 22 entries
	// (21 prior + 1 current). The probe subtracts 1 to match the DTO semantics
	// (CreatorPrevTokenCount = prior launches, excluding the current token).
	coins := make([]map[string]string, 22) // 21 prior + 1 current token
	for i := range coins {
		coins[i] = map[string]string{"mint": fmt.Sprintf("token%d", i)}
	}
	body, _ := json.Marshal(coins)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	p := NewSolanaCreatorReputationProbe(srv.Client(), defaultCreatorCfg(srv.URL), nil)
	in := solanaCreatorDTO("EvoD3sNDRPHAzQcRbr5o5psDVnbkpF5gD5LgFk6Txp5")
	out, err := p.Probe(context.Background(), in)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.CreatorPrevTokenCountKnown {
		t.Fatal("want CreatorPrevTokenCountKnown=true")
	}
	if out.CreatorPrevTokenCount != 21 {
		t.Fatalf("want count=21 prior tokens (22 from API minus current), got %d", out.CreatorPrevTokenCount)
	}
}

func TestSolanaCreatorReputation_ZeroTokensNewDev(t *testing.T) {
	// A legitimate first-time creator has 0 prior tokens.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	p := NewSolanaCreatorReputationProbe(srv.Client(), defaultCreatorCfg(srv.URL), nil)
	out, err := p.Probe(context.Background(), solanaCreatorDTO("newdev111"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.CreatorPrevTokenCountKnown {
		t.Fatal("want CreatorPrevTokenCountKnown=true even for 0 tokens (verified via API)")
	}
	if out.CreatorPrevTokenCount != 0 {
		t.Fatalf("want count=0, got %d", out.CreatorPrevTokenCount)
	}
}

func TestSolanaCreatorReputation_EnvelopeFormatWithTotal(t *testing.T) {
	// Some API versions wrap the array in {"total": N, "coins": [...]}.
	// total=42 means the creator has 42 tokens total (including the current one),
	// so CreatorPrevTokenCount = 42-1 = 41 prior tokens.
	body := `{"total":42,"coins":[{"mint":"a"},{"mint":"b"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	p := NewSolanaCreatorReputationProbe(srv.Client(), defaultCreatorCfg(srv.URL), nil)
	out, err := p.Probe(context.Background(), solanaCreatorDTO("somedev"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.CreatorPrevTokenCount != 41 {
		t.Fatalf("want count=41 prior tokens (42 from envelope total minus current), got %d", out.CreatorPrevTokenCount)
	}
}

// ── Fail-closed tests ─────────────────────────────────────────────────────────

func TestSolanaCreatorReputation_API500FailsClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := NewSolanaCreatorReputationProbe(srv.Client(), defaultCreatorCfg(srv.URL), nil)
	out, err := p.Probe(context.Background(), solanaCreatorDTO("somedev"))

	if err == nil {
		t.Fatal("want error on HTTP 500")
	}
	if out.CreatorPrevTokenCountKnown {
		t.Fatal("want CreatorPrevTokenCountKnown=false on API error (fail-closed)")
	}
}

func TestSolanaCreatorReputation_API404FailsClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "not found")
	}))
	defer srv.Close()

	p := NewSolanaCreatorReputationProbe(srv.Client(), defaultCreatorCfg(srv.URL), nil)
	out, err := p.Probe(context.Background(), solanaCreatorDTO("somedev"))

	if err == nil {
		t.Fatal("want error on HTTP 404")
	}
	if out.CreatorPrevTokenCountKnown {
		t.Fatal("want CreatorPrevTokenCountKnown=false on API 404 (fail-closed)")
	}
}

func TestSolanaCreatorReputation_MalformedJSONFailsClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `not valid json at all`)
	}))
	defer srv.Close()

	p := NewSolanaCreatorReputationProbe(srv.Client(), defaultCreatorCfg(srv.URL), nil)
	out, err := p.Probe(context.Background(), solanaCreatorDTO("somedev"))

	if err == nil {
		t.Fatal("want error on malformed JSON")
	}
	if out.CreatorPrevTokenCountKnown {
		t.Fatal("want CreatorPrevTokenCountKnown=false on parse error (fail-closed)")
	}
}

// ── Skip-path tests ───────────────────────────────────────────────────────────

func TestSolanaCreatorReputation_DisabledReturnsUnchanged(t *testing.T) {
	cfg := SolanaCreatorReputationConfig{Enabled: false, BaseURL: "https://example.com"}
	p := NewSolanaCreatorReputationProbe(nil, cfg, nil)

	in := solanaCreatorDTO("somedev")
	in.CreatorPrevTokenCountKnown = false

	out, err := p.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.CreatorPrevTokenCountKnown {
		t.Fatal("disabled probe must not set CreatorPrevTokenCountKnown")
	}
}

func TestSolanaCreatorReputation_NonSolanaChainSkipped(t *testing.T) {
	cfg := SolanaCreatorReputationConfig{Enabled: true, BaseURL: "https://example.com"}
	p := NewSolanaCreatorReputationProbe(nil, cfg, nil)

	in := contracts.MarketDataDTO{
		Chain:          "ethereum",
		TokenAddress:   "0xdeadbeef",
		CreatorAddress: "0xdeployer",
	}
	out, err := p.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.CreatorPrevTokenCountKnown {
		t.Fatal("non-Solana DTO must not be enriched by Solana creator probe")
	}
}

func TestSolanaCreatorReputation_EmptyCreatorAddressSkipped(t *testing.T) {
	cfg := SolanaCreatorReputationConfig{Enabled: true, BaseURL: "https://example.com"}
	p := NewSolanaCreatorReputationProbe(nil, cfg, nil)

	in := contracts.MarketDataDTO{
		Chain:          "solana",
		TokenAddress:   "sometoken",
		CreatorAddress: "", // blank
	}
	out, err := p.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.CreatorPrevTokenCountKnown {
		t.Fatal("probe must skip DTO with empty CreatorAddress")
	}
}

// ── Security tests ────────────────────────────────────────────────────────────

func TestSolanaCreatorReputation_InsecureHTTPBaseURLRejected(t *testing.T) {
	cfg := SolanaCreatorReputationConfig{
		Enabled:   true,
		BaseURL:   "http://malicious-api.example.com", // plain HTTP, not loopback
		TimeoutMs: 500,
	}
	// The probe rejects the insecure base_url before any HTTP call is made,
	// so passing a nil client is safe — the client is never invoked.
	p := NewSolanaCreatorReputationProbe(nil, cfg, nil)
	_, err := p.Probe(context.Background(), solanaCreatorDTO("somedev"))
	if err == nil {
		t.Fatal("want error for non-HTTPS base_url (security invariant)")
	}
	if !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("expected HTTPS error, got: %v", err)
	}
}

// ── Determinism test ──────────────────────────────────────────────────────────

func TestSolanaCreatorReputation_Determinism(t *testing.T) {
	// Same input always produces identical output.
	coins := make([]map[string]string, 5)
	for i := range coins {
		coins[i] = map[string]string{"mint": fmt.Sprintf("tok%d", i)}
	}
	body, _ := json.Marshal(coins)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	p := NewSolanaCreatorReputationProbe(srv.Client(), defaultCreatorCfg(srv.URL), nil)
	in := solanaCreatorDTO("repeatabledev")

	first, _ := p.Probe(context.Background(), in)
	second, _ := p.Probe(context.Background(), in)

	if first.CreatorPrevTokenCount != second.CreatorPrevTokenCount ||
		first.CreatorPrevTokenCountKnown != second.CreatorPrevTokenCountKnown {
		t.Fatal("non-deterministic probe results")
	}
}

// ── parseCreatorCoinCount unit tests ─────────────────────────────────────────

func TestParseCreatorCoinCount_EmptyArray(t *testing.T) {
	count, err := parseCreatorCoinCount([]byte(`[]`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Fatalf("want 0, got %d", count)
	}
}

func TestParseCreatorCoinCount_ArrayOf3(t *testing.T) {
	body := `[{"mint":"a"},{"mint":"b"},{"mint":"c"}]`
	count, err := parseCreatorCoinCount([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Fatalf("want 3, got %d", count)
	}
}

func TestParseCreatorCoinCount_EnvelopeWithTotalField(t *testing.T) {
	body := `{"total":99,"coins":[{"mint":"a"}]}`
	count, err := parseCreatorCoinCount([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 99 {
		t.Fatalf("want 99 (from total field), got %d", count)
	}
}

func TestParseCreatorCoinCount_InvalidJSON(t *testing.T) {
	_, err := parseCreatorCoinCount([]byte(`this is not json`))
	if err == nil {
		t.Fatal("want error for invalid JSON")
	}
}

// ── Circuit breaker + Helius DAS fallback tests ───────────────────────────────

// defaultCreatorCfgWithHelius returns a config that points pump.fun at
// pumpfunURL and Helius DAS at heliusURL (both local httptest servers).
func defaultCreatorCfgWithHelius(pumpfunURL, heliusURL string) SolanaCreatorReputationConfig {
	return SolanaCreatorReputationConfig{
		Enabled:      true,
		TimeoutMs:    500,
		BaseURL:      pumpfunURL,
		MaxBodyBytes: defaultCreatorMaxBodyBytes,
		PageLimit:    50,
		HeliusRPCURL: heliusURL,
	}
}

// heliusDASBody builds a minimal Helius DAS searchAssets response body.
func heliusDASBody(total int32) []byte {
	items := make([]map[string]string, 0)
	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"result": map[string]interface{}{
			"total": total,
			"limit": 50,
			"page":  1,
			"items": items,
		},
	})
	return body
}

// TestCreatorProbe_PumpFunFailsFallsBackToHelius verifies that when pump.fun
// returns a 503, the probe immediately falls back to Helius DAS and returns
// the count from Helius without error.
func TestCreatorProbe_PumpFunFailsFallsBackToHelius(t *testing.T) {
	pumpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable) // pump.fun is down
	}))
	defer pumpSrv.Close()

	// Helius returns total=6 (5 prior + 1 current → probe subtracts 1 → 5).
	heliusSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(heliusDASBody(6))
	}))
	defer heliusSrv.Close()

	cfg := defaultCreatorCfgWithHelius(pumpSrv.URL, heliusSrv.URL)
	p := NewSolanaCreatorReputationProbe(pumpSrv.Client(), cfg, nil)
	out, err := p.Probe(context.Background(), solanaCreatorDTO("serialdev"))

	if err != nil {
		t.Fatalf("want nil error (Helius fallback succeeded), got: %v", err)
	}
	if !out.CreatorPrevTokenCountKnown {
		t.Fatal("want CreatorPrevTokenCountKnown=true (Helius succeeded)")
	}
	if out.CreatorPrevTokenCount != 5 {
		t.Fatalf("want count=5 (6 from Helius minus current token), got %d", out.CreatorPrevTokenCount)
	}
}

// TestCreatorProbe_BothSourcesFailClosed verifies that when both pump.fun
// and Helius DAS fail, the probe returns an error and leaves
// CreatorPrevTokenCountKnown=false (double fail-closed).
func TestCreatorProbe_BothSourcesFailClosed(t *testing.T) {
	pumpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer pumpSrv.Close()

	heliusSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer heliusSrv.Close()

	cfg := defaultCreatorCfgWithHelius(pumpSrv.URL, heliusSrv.URL)
	p := NewSolanaCreatorReputationProbe(pumpSrv.Client(), cfg, nil)
	out, err := p.Probe(context.Background(), solanaCreatorDTO("somedev"))

	if err == nil {
		t.Fatal("want error when both sources fail")
	}
	if out.CreatorPrevTokenCountKnown {
		t.Fatal("want CreatorPrevTokenCountKnown=false when both sources fail (fail-closed)")
	}
}

// TestCreatorProbe_CircuitOpensAfterThresholdFailures verifies that after
// defaultCircuitFailureThreshold consecutive pump.fun failures the circuit
// breaker opens and subsequent probes route directly to Helius DAS.
func TestCreatorProbe_CircuitOpensAfterThresholdFailures(t *testing.T) {
	pumpCallCount := 0
	pumpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pumpCallCount++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer pumpSrv.Close()

	heliusSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(heliusDASBody(3))
	}))
	defer heliusSrv.Close()

	cfg := defaultCreatorCfgWithHelius(pumpSrv.URL, heliusSrv.URL)
	p := NewSolanaCreatorReputationProbe(pumpSrv.Client(), cfg, nil)
	in := solanaCreatorDTO("somedev")

	// Exhaust the failure threshold — each call fails pump.fun + falls back to Helius.
	for i := 0; i < defaultCircuitFailureThreshold; i++ {
		_, _ = p.Probe(context.Background(), in)
	}
	pumpBeforeOpen := pumpCallCount

	// Circuit should now be OPEN. This call must NOT hit pump.fun.
	out, err := p.Probe(context.Background(), in)

	if err != nil {
		t.Fatalf("want nil error after circuit opened (Helius handles it), got: %v", err)
	}
	if out.CreatorPrevTokenCount != 2 { // 3 - 1 (subtract current token)
		t.Fatalf("want count=2 from Helius after circuit opened, got %d", out.CreatorPrevTokenCount)
	}
	// pump.fun must not have been called again after the circuit opened.
	if pumpCallCount != pumpBeforeOpen {
		t.Fatalf("circuit is OPEN but pump.fun was called again (count=%d, expected %d)",
			pumpCallCount, pumpBeforeOpen)
	}
}

// TestCreatorProbe_InsecureHeliusURLRejected verifies that a non-HTTPS
// HeliusRPCURL (not loopback) is rejected before any HTTP call is made.
func TestCreatorProbe_InsecureHeliusURLRejected(t *testing.T) {
	pumpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable) // pump.fun is down
	}))
	defer pumpSrv.Close()

	cfg := defaultCreatorCfgWithHelius(
		pumpSrv.URL,
		"http://evil-rpc.example.com?api-key=SECRET", // non-HTTPS, non-loopback
	)
	p := NewSolanaCreatorReputationProbe(pumpSrv.Client(), cfg, nil)
	_, err := p.Probe(context.Background(), solanaCreatorDTO("somedev"))

	if err == nil {
		t.Fatal("want error: non-HTTPS HeliusRPCURL must be rejected (API key protection)")
	}
	if !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("expected HTTPS error for insecure Helius URL, got: %v", err)
	}
}

// ── parseHelliusDASCount unit tests ──────────────────────────────────────────

func TestParseHelliusDASCount_ValidResponse(t *testing.T) {
	body := heliusDASBody(7)
	count, err := parseHelliusDASCount(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 7 {
		t.Fatalf("want 7, got %d", count)
	}
}

func TestParseHelliusDASCount_RPCErrorBody(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","error":{"code":-32600,"message":"invalid request"}}`)
	_, err := parseHelliusDASCount(body)
	if err == nil {
		t.Fatal("want error for RPC error response")
	}
	if !strings.Contains(err.Error(), "-32600") {
		t.Fatalf("want error code in message, got: %v", err)
	}
}

func TestParseHelliusDASCount_MissingResult(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0"}`)
	_, err := parseHelliusDASCount(body)
	if err == nil {
		t.Fatal("want error when result field is missing")
	}
}

func TestParseHelliusDASCount_ZeroTotalFallsBackToItems(t *testing.T) {
	// When total=0 (API returned no total), count falls back to len(items).
	body := []byte(`{"jsonrpc":"2.0","result":{"total":0,"items":[{"id":"a"},{"id":"b"}]}}`)
	count, err := parseHelliusDASCount(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Fatalf("want count=2 from items len, got %d", count)
	}
}
