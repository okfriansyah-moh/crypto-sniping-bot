package rpc

import (
	"testing"
)

// TestDEXScreenerParser_CapturesMarketCapAndVolume verifies that the expanded
// dexScreenerResponse struct correctly deserialises and that
// parseDEXScreenerMarketData returns the expected market-cap and volume values.
func TestDEXScreenerParser_CapturesMarketCapAndVolume(t *testing.T) {
	body := []byte(`{
		"pairs": [{
			"priceNative": "0.00000042",
			"fdv": 6900.0,
			"marketCap": 6700.0,
			"liquidity": { "usd": 15000 },
			"volume": { "m5": 210.5, "h1": 4850.0, "h6": 12300.0, "h24": 18400.0 }
		}]
	}`)

	md, err := parseDEXScreenerMarketData(body)
	if err != nil {
		t.Fatalf("parseDEXScreenerMarketData: unexpected error: %v", err)
	}

	// FDV (6900) must be preferred over marketCap (6700) when non-zero.
	if md.MarketCapUsd != 6900.0 {
		t.Errorf("MarketCapUsd: want 6900.0 (FDV preferred), got %v", md.MarketCapUsd)
	}
	if md.VolumeUsd5m != 210.5 {
		t.Errorf("VolumeUsd5m: want 210.5, got %v", md.VolumeUsd5m)
	}
	if md.VolumeUsd1h != 4850.0 {
		t.Errorf("VolumeUsd1h: want 4850.0, got %v", md.VolumeUsd1h)
	}
	if md.VolumeUsd24h != 18400.0 {
		t.Errorf("VolumeUsd24h: want 18400.0, got %v", md.VolumeUsd24h)
	}
}

// TestDEXScreenerParser_FallsBackToMarketCapWhenFDVIsZero verifies that
// parseDEXScreenerMarketData uses marketCap when fdv is 0 or absent.
func TestDEXScreenerParser_FallsBackToMarketCapWhenFDVIsZero(t *testing.T) {
	body := []byte(`{
		"pairs": [{
			"priceNative": "0.0001",
			"fdv": 0,
			"marketCap": 3500.0,
			"volume": { "m5": 10.0, "h1": 50.0, "h24": 200.0 }
		}]
	}`)

	md, err := parseDEXScreenerMarketData(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if md.MarketCapUsd != 3500.0 {
		t.Errorf("MarketCapUsd: want 3500.0 (fallback to marketCap), got %v", md.MarketCapUsd)
	}
}

// TestDEXScreenerParser_EmptyPairsReturnsZeroValues verifies fail-open behaviour
// when the token is not yet indexed by DEXScreener (empty pairs array).
func TestDEXScreenerParser_EmptyPairsReturnsZeroValues(t *testing.T) {
	body := []byte(`{"pairs": []}`)

	md, err := parseDEXScreenerMarketData(body)
	if err != nil {
		t.Fatalf("unexpected error for empty pairs: %v", err)
	}
	if md.MarketCapUsd != 0 || md.VolumeUsd5m != 0 || md.VolumeUsd1h != 0 || md.VolumeUsd24h != 0 {
		t.Errorf("expected all zero values for empty pairs, got %+v", md)
	}
}

// TestDEXScreenerParser_NullPairsReturnsZeroValues verifies that a null or
// missing pairs field does not cause an error.
func TestDEXScreenerParser_NullPairsReturnsZeroValues(t *testing.T) {
	body := []byte(`{"pairs": null}`)

	md, err := parseDEXScreenerMarketData(body)
	if err != nil {
		t.Fatalf("unexpected error for null pairs: %v", err)
	}
	if md.MarketCapUsd != 0 {
		t.Errorf("expected zero MarketCapUsd, got %v", md.MarketCapUsd)
	}
}

// TestDEXScreenerParser_NilVolumeObjectReturnsZeroVolume verifies that a pair
// without a volume field populates volume fields as 0.0 without error.
func TestDEXScreenerParser_NilVolumeObjectReturnsZeroVolume(t *testing.T) {
	body := []byte(`{
		"pairs": [{
			"priceNative": "0.001",
			"fdv": 5000.0
		}]
	}`)

	md, err := parseDEXScreenerMarketData(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if md.MarketCapUsd != 5000.0 {
		t.Errorf("MarketCapUsd: want 5000.0, got %v", md.MarketCapUsd)
	}
	if md.VolumeUsd5m != 0 || md.VolumeUsd1h != 0 || md.VolumeUsd24h != 0 {
		t.Errorf("expected zero volume fields when volume is absent, got 5m=%v 1h=%v 24h=%v",
			md.VolumeUsd5m, md.VolumeUsd1h, md.VolumeUsd24h)
	}
}

// TestParseDEXScreenerPrice_StillWorksAfterStructExpansion verifies that the
// existing parseDEXScreenerPrice function is unaffected by the parser expansion.
func TestParseDEXScreenerPrice_StillWorksAfterStructExpansion(t *testing.T) {
	body := []byte(`{
		"pairs": [{
			"priceNative": "0.00000123",
			"fdv": 9999.0,
			"volume": { "m5": 1.0, "h1": 5.0, "h24": 20.0 }
		}]
	}`)

	price, err := parseDEXScreenerPrice(body)
	if err != nil {
		t.Fatalf("parseDEXScreenerPrice: %v", err)
	}
	if price != "0.00000123" {
		t.Errorf("parseDEXScreenerPrice: want 0.00000123, got %q", price)
	}
}
