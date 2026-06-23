package data_quality

import (
	"context"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// runtimeWithMarketCapFilters returns a runtime config with market-cap and
// volume threshold filters activated (non-zero values). Other mandatory hard-
// rejects are fully relaxed so the test can isolate the market-cap/volume
// logic without interference.
func runtimeWithMarketCapFilters(minCap, maxCap, minVol float64) *config.DataQualityRuntimeConfig {
	rt := runtimeWithProfiles()
	// Relax all mandatory hard-rejects so only market-cap/volume can fire.
	rt.Thresholds.MaxCreatorPrevTokenCount = 0
	rt.Thresholds.RejectNoSocialLinks = false
	rt.Thresholds.RejectUnknownSocialLinks = false
	rt.Thresholds.RejectUnknownCreatorCount = false
	rt.Thresholds.MaxTotalSupply = 0
	rt.Thresholds.RejectUnknownTotalSupply = false
	rt.Thresholds.MinTokenAgeSeconds = 0
	// Set the filter values under test.
	rt.Thresholds.MinMarketCapUsd = minCap
	rt.Thresholds.MaxMarketCapUsd = maxCap
	rt.Thresholds.MinVolumeUsd1h = minVol
	return rt
}

// baseMarketCapDTO returns a clean PumpFunCreate MarketDataDTO that will PASS
// all hard-rejects (social links present, first-time dev, supply known+low).
// Tests override MarketCapUsd / VolumeUsd1h as needed.
func baseMarketCapDTO() contracts.MarketDataDTO {
	return contracts.MarketDataDTO{
		EventID:       "evt-mktcap-1",
		TraceID:       "trace-mktcap",
		CorrelationID: "corr-mktcap",
		VersionID:     "v1",
		TokenAddress:  "tokenMktCap",
		Chain:         "solana",
		EventTopic:    "PumpFunCreate",
		ReserveBaseRaw: "0",
		TaxKnown:      true,
		BuyTaxBps:     0,
		SellTaxBps:    0,
		PoolAgeSeconds: 30,
		// Clean dev profile — first-time launcher.
		CreatorAddress:             "devWalletClean",
		CreatorPrevTokenCountKnown: true,
		CreatorPrevTokenCount:      0,
		// Social links present.
		SocialLinksKnown: true,
		HasSocialLinks:   true,
		// Supply within allowed range.
		TotalSupplyKnown: true,
		TotalSupply:      500_000_000,
		// Market-cap / volume fields default to zero (unindexed).
		MarketCapUsd: 0,
		VolumeUsd1h:  0,
	}
}

// ── Market-cap filter: threshold = 0 (filter disabled) ───────────────────────

// TestProcessForMode_MarketCapFilterInertWhenThresholdZero verifies that when
// both MinMarketCapUsd and MaxMarketCapUsd are 0 (commented out in YAML), the
// filter does not reject any token regardless of its MarketCapUsd value.
func TestProcessForMode_MarketCapFilterInertWhenThresholdZero(t *testing.T) {
	// All thresholds = 0 (filter completely disabled)
	rt := runtimeWithMarketCapFilters(0, 0, 0)
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	// Token with a MarketCapUsd value that would have been rejected if the
	// filter were active (e.g., $1 — below any plausible min threshold).
	in := baseMarketCapDTO()
	in.MarketCapUsd = 1.0

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, r := range out.RejectReasons {
		if r == "market_cap_too_low" || r == "market_cap_too_high" {
			t.Errorf("filter must be inert when threshold=0, got RejectReasons=%v", out.RejectReasons)
		}
	}
}

// ── Market-cap filter: input field = 0 (token not yet indexed) ───────────────

// TestProcessForMode_MarketCapFilterInertWhenFieldZero verifies that when
// MarketCapUsd = 0 (brand-new token not yet indexed by DEXScreener), the
// filter does NOT reject the token even if thresholds are active. This is the
// critical guard-pattern invariant: BOTH threshold > 0 AND field > 0 required.
func TestProcessForMode_MarketCapFilterInertWhenFieldZero(t *testing.T) {
	// Thresholds are active, but the token has not been indexed yet.
	rt := runtimeWithMarketCapFilters(3000.0, 20000.0, 100.0)
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseMarketCapDTO()
	in.MarketCapUsd = 0   // not indexed by DEXScreener
	in.VolumeUsd1h = 0   // not indexed by DEXScreener

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, r := range out.RejectReasons {
		if r == "market_cap_too_low" || r == "market_cap_too_high" || r == "volume_too_low" {
			t.Errorf("filter must be inert when field=0 (unindexed), got RejectReasons=%v", out.RejectReasons)
		}
	}
}

// ── Reject: market_cap_too_low ────────────────────────────────────────────────

// TestProcessForMode_RejectsMarketCapTooLow verifies that a token whose
// MarketCapUsd is below MinMarketCapUsd is rejected with market_cap_too_low
// (provided both threshold > 0 and field > 0).
func TestProcessForMode_RejectsMarketCapTooLow(t *testing.T) {
	rt := runtimeWithMarketCapFilters(3000.0, 20000.0, 0)
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseMarketCapDTO()
	in.MarketCapUsd = 1500.0 // below $3 000 min

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionReject {
		t.Errorf("expected REJECT for market_cap_too_low, got %q (reasons=%v)", out.Decision, out.RejectReasons)
	}
	if !containsString(out.RejectReasons, "market_cap_too_low") {
		t.Errorf("expected market_cap_too_low in RejectReasons, got %v", out.RejectReasons)
	}
}

// ── Reject: market_cap_too_high ───────────────────────────────────────────────

// TestProcessForMode_RejectsMarketCapTooHigh verifies that a token whose
// MarketCapUsd exceeds MaxMarketCapUsd is rejected with market_cap_too_high.
func TestProcessForMode_RejectsMarketCapTooHigh(t *testing.T) {
	rt := runtimeWithMarketCapFilters(3000.0, 20000.0, 0)
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseMarketCapDTO()
	in.MarketCapUsd = 75000.0 // above $20 000 max

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionReject {
		t.Errorf("expected REJECT for market_cap_too_high, got %q (reasons=%v)", out.Decision, out.RejectReasons)
	}
	if !containsString(out.RejectReasons, "market_cap_too_high") {
		t.Errorf("expected market_cap_too_high in RejectReasons, got %v", out.RejectReasons)
	}
}

// ── Reject: volume_too_low ────────────────────────────────────────────────────

// TestProcessForMode_RejectsVolumeTooLow verifies that a token whose
// VolumeUsd1h is below MinVolumeUsd1h is rejected with volume_too_low
// (provided both threshold > 0 and field > 0).
func TestProcessForMode_RejectsVolumeTooLow(t *testing.T) {
	rt := runtimeWithMarketCapFilters(0, 0, 100.0)
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseMarketCapDTO()
	in.MarketCapUsd = 10000.0 // within range (cap thresholds disabled)
	in.VolumeUsd1h = 25.0    // below $100 / h min

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionReject {
		t.Errorf("expected REJECT for volume_too_low, got %q (reasons=%v)", out.Decision, out.RejectReasons)
	}
	if !containsString(out.RejectReasons, "volume_too_low") {
		t.Errorf("expected volume_too_low in RejectReasons, got %v", out.RejectReasons)
	}
}

// ── Volume filter: field = 0 (unindexed) ─────────────────────────────────────

// TestProcessForMode_VolumeFilterInertWhenFieldZero verifies that when
// VolumeUsd1h = 0 (not yet indexed by DEXScreener), the volume filter is
// inert even when MinVolumeUsd1h is configured.
func TestProcessForMode_VolumeFilterInertWhenFieldZero(t *testing.T) {
	rt := runtimeWithMarketCapFilters(0, 0, 100.0)
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseMarketCapDTO()
	in.VolumeUsd1h = 0 // not indexed

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, r := range out.RejectReasons {
		if r == "volume_too_low" {
			t.Errorf("volume filter must be inert when VolumeUsd1h=0, got RejectReasons=%v", out.RejectReasons)
		}
	}
}

// ── Pass-through: market cap within range ─────────────────────────────────────

// TestProcessForMode_PassesWhenMarketCapInRange verifies that a token whose
// MarketCapUsd sits inside [MinMarketCapUsd, MaxMarketCapUsd] is NOT rejected
// by the market-cap filter.
func TestProcessForMode_PassesWhenMarketCapInRange(t *testing.T) {
	rt := runtimeWithMarketCapFilters(3000.0, 20000.0, 0)
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseMarketCapDTO()
	in.MarketCapUsd = 8000.0 // within $3k–$20k window

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, r := range out.RejectReasons {
		if r == "market_cap_too_low" || r == "market_cap_too_high" {
			t.Errorf("in-range market cap must not be rejected, got RejectReasons=%v", out.RejectReasons)
		}
	}
}

// ── Mandatory hard-rejects still fire first ──────────────────────────────────

// TestProcessForMode_MandatoryRejectsFireBeforeMarketCapFilter verifies that
// the mandatory hard-reject for no_social_links fires even when market-cap
// thresholds are also set. Order of structural checks must not be disturbed by
// Task 18 additions.
func TestProcessForMode_MandatoryRejectsFireBeforeMarketCapFilter(t *testing.T) {
	rt := runtimeWithMarketCapFilters(3000.0, 20000.0, 100.0)
	rt.Thresholds.RejectNoSocialLinks = true    // mandatory hard-reject active
	rt.Thresholds.RejectUnknownSocialLinks = false
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseMarketCapDTO()
	in.SocialLinksKnown = true
	in.HasSocialLinks = false   // triggers no_social_links
	in.MarketCapUsd = 1500.0   // would also trigger market_cap_too_low
	in.VolumeUsd1h = 10.0     // would also trigger volume_too_low

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionReject {
		t.Errorf("expected REJECT due to no_social_links, got %q", out.Decision)
	}
	// Mandatory reject must be present.
	if !containsString(out.RejectReasons, "no_social_links") {
		t.Errorf("expected no_social_links in RejectReasons, got %v", out.RejectReasons)
	}
	// Market-cap and volume rejects may also appear (accumulation is fine) but
	// the key invariant is that no_social_links is present — mandatory first.
	_ = out.RejectReasons
}
