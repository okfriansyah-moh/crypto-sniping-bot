package probes

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-sniping-bot/shared/contracts"
)

// TestDEXScreenerProbe_PopulatesNewFields verifies that the probe correctly maps
// the four market-data fields from a well-formed DEXScreener API response onto
// the returned MarketDataDTO.
func TestDEXScreenerProbe_PopulatesNewFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{
			"pairs": [{
				"priceNative": "0.0001",
				"fdv": 6900.0,
				"marketCap": 6700.0,
				"volume": { "m5": 210.5, "h1": 4850.0, "h24": 18400.0 }
			}]
		}`)
	}))
	defer srv.Close()

	// Inject an HTTP client pointing to the test server.
	client := srv.Client()
	cfg := DEXScreenerMarketDataConfig{Enabled: true, TimeoutMs: 500}
	probe := NewDEXScreenerMarketDataProbe(client, cfg, nil)

	// Override the base URL to point at the test server.
	probe.baseURL = srv.URL + "/"

	in := contracts.MarketDataDTO{
		TokenAddress: "TokenAAA",
		Chain:        "solana",
	}
	out, err := probe.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("Probe: unexpected error: %v", err)
	}

	// FDV (6900) must be preferred over marketCap (6700).
	if out.MarketCapUsd != 6900.0 {
		t.Errorf("MarketCapUsd: want 6900.0, got %v", out.MarketCapUsd)
	}
	if out.VolumeUsd5m != 210.5 {
		t.Errorf("VolumeUsd5m: want 210.5, got %v", out.VolumeUsd5m)
	}
	if out.VolumeUsd1h != 4850.0 {
		t.Errorf("VolumeUsd1h: want 4850.0, got %v", out.VolumeUsd1h)
	}
	if out.VolumeUsd24h != 18400.0 {
		t.Errorf("VolumeUsd24h: want 18400.0, got %v", out.VolumeUsd24h)
	}
}

// TestDEXScreenerProbe_FailOpenWhenNoData verifies that the probe returns the
// input DTO unchanged (all market-data fields = 0.0) when DEXScreener returns
// an empty pairs array. The token must NOT be rejected.
func TestDEXScreenerProbe_FailOpenWhenNoData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"pairs": []}`)
	}))
	defer srv.Close()

	client := srv.Client()
	cfg := DEXScreenerMarketDataConfig{Enabled: true, TimeoutMs: 500}
	probe := NewDEXScreenerMarketDataProbe(client, cfg, nil)
	probe.baseURL = srv.URL + "/"

	in := contracts.MarketDataDTO{
		TokenAddress: "TokenBBB",
		Chain:        "solana",
	}
	out, err := probe.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("Probe: unexpected error for empty pairs: %v", err)
	}
	if out.MarketCapUsd != 0 || out.VolumeUsd5m != 0 || out.VolumeUsd1h != 0 || out.VolumeUsd24h != 0 {
		t.Errorf("expected all zero market-data fields for unindexed token, got MarketCapUsd=%v VolumeUsd5m=%v VolumeUsd1h=%v VolumeUsd24h=%v",
			out.MarketCapUsd, out.VolumeUsd5m, out.VolumeUsd1h, out.VolumeUsd24h)
	}
}

// TestDEXScreenerProbe_FailOpenOnHTTPError verifies that the probe returns
// (in, err) with fields = 0.0 when the HTTP request fails, preserving
// fail-open behaviour so the token is not rejected.
func TestDEXScreenerProbe_FailOpenOnHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := srv.Client()
	cfg := DEXScreenerMarketDataConfig{Enabled: true, TimeoutMs: 500}
	probe := NewDEXScreenerMarketDataProbe(client, cfg, nil)
	probe.baseURL = srv.URL + "/"

	in := contracts.MarketDataDTO{
		TokenAddress: "TokenCCC",
		Chain:        "solana",
	}
	out, err := probe.Probe(context.Background(), in)
	// Fail-open: error is returned but the DTO fields must still be zero.
	if err == nil {
		t.Fatal("expected an error on HTTP 500, got nil")
	}
	if out.MarketCapUsd != 0 || out.VolumeUsd1h != 0 {
		t.Errorf("expected zero fields on error, got MarketCapUsd=%v VolumeUsd1h=%v",
			out.MarketCapUsd, out.VolumeUsd1h)
	}
}

// TestDEXScreenerProbe_DisabledReturnsInputUnchanged verifies that a disabled
// probe is a no-op: the input DTO is returned without any HTTP call.
func TestDEXScreenerProbe_DisabledReturnsInputUnchanged(t *testing.T) {
	// A closed server — any attempt to connect will fail, proving no HTTP
	// call was made.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("HTTP call made to disabled probe")
	}))
	srv.Close()

	client := srv.Client()
	cfg := DEXScreenerMarketDataConfig{Enabled: false, TimeoutMs: 500}
	probe := NewDEXScreenerMarketDataProbe(client, cfg, nil)
	probe.baseURL = srv.URL + "/"

	in := contracts.MarketDataDTO{
		TokenAddress: "TokenDDD",
		Chain:        "solana",
		MarketCapUsd: 99999.0, // pre-existing value must survive pass-through
	}
	out, err := probe.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("disabled probe: unexpected error: %v", err)
	}
	if out.MarketCapUsd != 99999.0 {
		t.Errorf("disabled probe: MarketCapUsd should be unchanged, got %v", out.MarketCapUsd)
	}
}

// TestDEXScreenerProbe_FDVFallbackToMarketCap verifies that when fdv=0,
// the probe correctly falls back to the marketCap field.
func TestDEXScreenerProbe_FDVFallbackToMarketCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{
			"pairs": [{
				"fdv": 0,
				"marketCap": 3500.0,
				"volume": { "m5": 5.0, "h1": 25.0, "h24": 100.0 }
			}]
		}`)
	}))
	defer srv.Close()

	client := srv.Client()
	cfg := DEXScreenerMarketDataConfig{Enabled: true, TimeoutMs: 500}
	probe := NewDEXScreenerMarketDataProbe(client, cfg, nil)
	probe.baseURL = srv.URL + "/"

	in := contracts.MarketDataDTO{TokenAddress: "TokenEEE", Chain: "solana"}
	out, err := probe.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.MarketCapUsd != 3500.0 {
		t.Errorf("MarketCapUsd: want 3500.0 (fallback from fdv=0), got %v", out.MarketCapUsd)
	}
}

// TestDEXScreenerProbe_ProbeName verifies the stable probe identifier.
func TestDEXScreenerProbe_ProbeName(t *testing.T) {
	probe := NewDEXScreenerMarketDataProbe(nil, DEXScreenerMarketDataConfig{}, nil)
	if probe.Name() != "dexscreener_market_data" {
		t.Errorf("Name(): want dexscreener_market_data, got %q", probe.Name())
	}
}
