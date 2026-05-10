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
	// Simulate a dev with 21 prior tokens (the exact gate-review scenario).
	coins := make([]map[string]string, 21)
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
		t.Fatalf("want count=21, got %d", out.CreatorPrevTokenCount)
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
	// Some API versions wrap the array in {"total": N, "coins": [...]}
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
	if out.CreatorPrevTokenCount != 42 {
		t.Fatalf("want count=42 (from envelope total), got %d", out.CreatorPrevTokenCount)
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
