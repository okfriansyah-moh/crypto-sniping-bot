package data_quality

import (
	"context"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// runtimeWithEnforcement returns a runtime config that mirrors production
// data_quality.yaml settings for all mandatory structural hard-rejects:
// serial-launcher, unknown-creator, social-link, unknown-social, and supply.
func runtimeWithEnforcement() *config.DataQualityRuntimeConfig {
	rt := runtimeWithProfiles()
	rt.Detectors.DevReputation = true
	rt.Thresholds.MaxCreatorPrevTokenCount = 1 // any prior launch → reject
	rt.Thresholds.NoSocialLinksRiskScore = 0.40
	rt.Thresholds.RejectNoSocialLinks = true
	rt.Thresholds.RejectUnknownSocialLinks = true  // mandatory fail-closed
	rt.Thresholds.RejectUnknownCreatorCount = true // mandatory fail-closed
	rt.Thresholds.MaxTotalSupply = 1_000_000_000   // 1B — mirrors YAML
	rt.Thresholds.RejectUnknownTotalSupply = true
	rt.RiskWeights.DevReputation = 0.25
	return rt
}

// baseNewLaunch returns a new PumpFunCreate event with a clean, legitimate
// dev profile — first-time launcher with social links present.
func baseNewLaunch() contracts.MarketDataDTO {
	return contracts.MarketDataDTO{
		EventID:       "evt-launch-1",
		TraceID:       "trace-1",
		CorrelationID: "corr-1",
		VersionID:     "v1",
		TokenAddress:  "token123",
		Chain:         "solana",
		EventTopic:    "PumpFunCreate",
		// New pump.fun launch: zero reserves (allowed for new launches)
		ReserveBaseRaw: "0",
		TaxKnown:       true,
		BuyTaxBps:      0,
		SellTaxBps:     0,
		PoolAgeSeconds: 1,
		// Clean dev: first-time, has social links
		CreatorAddress:             "devWallet1",
		CreatorPrevTokenCountKnown: true,
		CreatorPrevTokenCount:      0,
		SocialLinksKnown:           true,
		HasSocialLinks:             true,
	}
}

// ── Serial-launcher structural reject ────────────────────────────────────────

// TestStructuralReject_SerialLauncher_RejectsInBALANCED verifies that a dev
// with 382 prior tokens (TTTT pattern) is structurally rejected in BALANCED
// mode even though the dev-reputation score alone (0.25 weight) cannot push
// the aggregate over the 0.50 BALANCED threshold by itself.
func TestStructuralReject_SerialLauncher_RejectsInBALANCED(t *testing.T) {
	rt := runtimeWithEnforcement()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.CreatorPrevTokenCount = 382 // TTTT dev: 382 prior tokens
	in.HasSocialLinks = true       // even with social links present

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT for serial launcher (382 tokens), got %q (score=%.3f, reasons=%v)",
			out.Decision, out.RiskScore, out.RejectReasons)
	}
	if !containsString(out.RejectReasons, "serial_launcher") {
		t.Errorf("expected serial_launcher in RejectReasons, got %v", out.RejectReasons)
	}
}

// TestStructuralReject_SerialLauncher_ThresholdExact verifies threshold=1 is
// inclusive: a dev with exactly 1 prior token is rejected (≥ threshold).
func TestStructuralReject_SerialLauncher_ThresholdExact(t *testing.T) {
	rt := runtimeWithEnforcement()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.CreatorPrevTokenCount = 1 // exactly at threshold

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT for count=1 (threshold=1), got %q", out.Decision)
	}
	if !containsString(out.RejectReasons, "serial_launcher") {
		t.Errorf("expected serial_launcher reason, got %v", out.RejectReasons)
	}
}

// TestStructuralReject_SerialLauncher_ZeroCountPasses verifies that a
// first-time dev (count=0) does NOT trigger the serial_launcher reject.
func TestStructuralReject_SerialLauncher_ZeroCountPasses(t *testing.T) {
	rt := runtimeWithEnforcement()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.CreatorPrevTokenCount = 0 // first-time dev

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsString(out.RejectReasons, "serial_launcher") {
		t.Errorf("first-time dev should NOT have serial_launcher reason; got %v", out.RejectReasons)
	}
}

// TestStructuralReject_SerialLauncher_UnknownCountRejectsWhenFlagEnabled verifies
// that when CreatorPrevTokenCountKnown=false (probe timed out, API error, or not
// yet run) and RejectUnknownCreatorCount=true, the token is structurally rejected
// via "unknown_creator_count". This is the mandatory fail-closed gap: probe
// failure must not silently convert a serial rug developer into a first-timer.
func TestStructuralReject_SerialLauncher_UnknownCountRejectsWhenFlagEnabled(t *testing.T) {
	rt := runtimeWithEnforcement() // RejectUnknownCreatorCount=true
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.CreatorPrevTokenCountKnown = false // probe timed out or API error
	in.CreatorPrevTokenCount = 0          // zero value (not populated)

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT for unknown creator count (fail-closed), got %q (reasons=%v)",
			out.Decision, out.RejectReasons)
	}
	if !containsString(out.RejectReasons, "unknown_creator_count") {
		t.Errorf("expected unknown_creator_count in RejectReasons, got %v", out.RejectReasons)
	}
}

// TestStructuralReject_SerialLauncher_UnknownCountPassesWhenFlagDisabled verifies
// that when RejectUnknownCreatorCount=false, an unknown creator count does NOT
// produce a structural reject (soft-scoring path applies instead).
func TestStructuralReject_SerialLauncher_UnknownCountPassesWhenFlagDisabled(t *testing.T) {
	rt := runtimeWithEnforcement()
	rt.Thresholds.RejectUnknownCreatorCount = false // operator opt-out
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.CreatorPrevTokenCountKnown = false
	in.CreatorPrevTokenCount = 0

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsString(out.RejectReasons, "unknown_creator_count") {
		t.Errorf("disabled flag must not produce unknown_creator_count reason; got %v", out.RejectReasons)
	}
}

// TestStructuralReject_SerialLauncher_DisabledByZeroThreshold confirms that
// setting MaxCreatorPrevTokenCount=0 disables the structural reject entirely.
func TestStructuralReject_SerialLauncher_DisabledByZeroThreshold(t *testing.T) {
	rt := runtimeWithEnforcement()
	rt.Thresholds.MaxCreatorPrevTokenCount = 0 // disabled
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.CreatorPrevTokenCount = 382

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsString(out.RejectReasons, "serial_launcher") {
		t.Errorf("threshold=0 should disable serial_launcher check; got %v", out.RejectReasons)
	}
}

// ── No-social-links structural reject ────────────────────────────────────────

// TestStructuralReject_NoSocialLinks_RejectsInBALANCED verifies that a token
// with confirmed absent social links (SocialLinksKnown=true, HasSocialLinks=false)
// is structurally rejected when reject_no_social_links=true, even though the
// social-links score contribution alone (0.40 × 0.25 = 0.10) is below the
// BALANCED 0.50 threshold.
func TestStructuralReject_NoSocialLinks_RejectsInBALANCED(t *testing.T) {
	rt := runtimeWithEnforcement()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.CreatorPrevTokenCount = 0 // clean dev — only social signal fires
	in.SocialLinksKnown = true
	in.HasSocialLinks = false // no Twitter, no Telegram, no website

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT for no social links, got %q (score=%.3f, reasons=%v)",
			out.Decision, out.RiskScore, out.RejectReasons)
	}
	if !containsString(out.RejectReasons, "no_social_links") {
		t.Errorf("expected no_social_links in RejectReasons, got %v", out.RejectReasons)
	}
}

// TestStructuralReject_NoSocialLinks_WithSocialLinksPasses ensures a token
// that HAS social links is not incorrectly flagged.
func TestStructuralReject_NoSocialLinks_WithSocialLinksPasses(t *testing.T) {
	rt := runtimeWithEnforcement()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.HasSocialLinks = true // LOL reference token pattern

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsString(out.RejectReasons, "no_social_links") {
		t.Errorf("token with social links should not get no_social_links reject; got %v", out.RejectReasons)
	}
}

// TestStructuralReject_NoSocialLinks_UnknownRejectsWhenFlagEnabled verifies that
// when SocialLinksKnown=false (metadata probe timed out or fetch error) and
// RejectUnknownSocialLinks=true, the token is structurally rejected via
// "unknown_social_links". A token whose social presence cannot be verified
// must not pass — probe failure must not equal approval.
func TestStructuralReject_NoSocialLinks_UnknownRejectsWhenFlagEnabled(t *testing.T) {
	rt := runtimeWithEnforcement() // RejectUnknownSocialLinks=true
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.SocialLinksKnown = false // metadata probe timed out or errored
	in.HasSocialLinks = false

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT for unknown social links (fail-closed), got %q (reasons=%v)",
			out.Decision, out.RejectReasons)
	}
	if !containsString(out.RejectReasons, "unknown_social_links") {
		t.Errorf("expected unknown_social_links in RejectReasons, got %v", out.RejectReasons)
	}
}

// TestStructuralReject_NoSocialLinks_UnknownPassesWhenFlagDisabled verifies that
// when RejectUnknownSocialLinks=false, an unknown social link status does NOT
// produce a structural reject (soft-scoring path applies instead).
func TestStructuralReject_NoSocialLinks_UnknownPassesWhenFlagDisabled(t *testing.T) {
	rt := runtimeWithEnforcement()
	rt.Thresholds.RejectUnknownSocialLinks = false // operator opt-out
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.SocialLinksKnown = false
	in.HasSocialLinks = false

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsString(out.RejectReasons, "unknown_social_links") {
		t.Errorf("disabled flag must not produce unknown_social_links reason; got %v", out.RejectReasons)
	}
}

// TestStructuralReject_NoSocialLinks_DisabledByFlag verifies that when
// RejectNoSocialLinks=false, missing social links is a scoring signal only
// (not a structural reject).
func TestStructuralReject_NoSocialLinks_DisabledByFlag(t *testing.T) {
	rt := runtimeWithEnforcement()
	rt.Thresholds.RejectNoSocialLinks = false // disabled
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.CreatorPrevTokenCount = 0
	in.SocialLinksKnown = true
	in.HasSocialLinks = false

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsString(out.RejectReasons, "no_social_links") {
		t.Errorf("disabled reject flag must not add no_social_links reason; got %v", out.RejectReasons)
	}
}

// ── Combined signal (TTTT pattern) ───────────────────────────────────────────

// TestStructuralReject_TTTTPattern_RejectsOnBothSignals is the canonical
// regression for the TTTT token: serial dev (382 tokens) with no social links.
// Both structural rejects should fire simultaneously.
func TestStructuralReject_TTTTPattern_RejectsOnBothSignals(t *testing.T) {
	rt := runtimeWithEnforcement()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.CreatorPrevTokenCount = 382
	in.SocialLinksKnown = true
	in.HasSocialLinks = false

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("TTTT pattern must be REJECT; got %q (reasons=%v)", out.Decision, out.RejectReasons)
	}
	if !containsString(out.RejectReasons, "serial_launcher") {
		t.Errorf("expected serial_launcher reason; got %v", out.RejectReasons)
	}
	if !containsString(out.RejectReasons, "no_social_links") {
		t.Errorf("expected no_social_links reason; got %v", out.RejectReasons)
	}
}

// ── Unknown total supply structural reject ────────────────────────────────────

// TestStructuralReject_UnknownSupply_RejectsWhenProbeUnhealthy verifies that
// when the LP probe fails to run (TotalSupplyKnown=false) and
// reject_unknown_total_supply=true, the token is structurally rejected.
// This is the LP-probe-failure gap: a 2B supply token must not pass simply
// because the RPC was unhealthy during the probe window.
func TestStructuralReject_UnknownSupply_RejectsWhenProbeUnhealthy(t *testing.T) {
	rt := runtimeWithEnforcement() // RejectUnknownTotalSupply=true, MaxTotalSupply=1B
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.TotalSupplyKnown = false // LP probe didn't run / RPC unhealthy
	// TotalSupply zero-value — probe never populated it

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT when supply unknown (fail-closed), got %q (reasons=%v)",
			out.Decision, out.RejectReasons)
	}
	if !containsString(out.RejectReasons, "unknown_total_supply") {
		t.Errorf("expected unknown_total_supply in RejectReasons, got %v", out.RejectReasons)
	}
}

// TestStructuralReject_UnknownSupply_KnownSupplyNotRejected verifies that
// when the LP probe ran successfully and supply is within threshold,
// the token is NOT rejected on unknown_total_supply.
func TestStructuralReject_UnknownSupply_KnownSupplyNotRejected(t *testing.T) {
	rt := runtimeWithEnforcement()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.TotalSupplyKnown = true
	in.TotalSupply = 500_000_000 // 500M — under 1B threshold

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsString(out.RejectReasons, "unknown_total_supply") {
		t.Errorf("known supply within threshold must not get unknown_total_supply reason; got %v", out.RejectReasons)
	}
	if containsString(out.RejectReasons, "high_total_supply") {
		t.Errorf("500M supply is within 1B threshold, must not get high_total_supply; got %v", out.RejectReasons)
	}
}

// TestStructuralReject_UnknownSupply_HighKnownSupplyRejectsNormally verifies
// that a known 2B supply is still rejected via high_total_supply (the original
// path), not unknown_total_supply.
func TestStructuralReject_UnknownSupply_HighKnownSupplyRejectsNormally(t *testing.T) {
	rt := runtimeWithEnforcement()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.TotalSupplyKnown = true
	in.TotalSupply = 2_000_000_000 // 2B — exceeds 1B threshold

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT for 2B supply, got %q", out.Decision)
	}
	if !containsString(out.RejectReasons, "high_total_supply") {
		t.Errorf("expected high_total_supply reason, got %v", out.RejectReasons)
	}
	if containsString(out.RejectReasons, "unknown_total_supply") {
		t.Errorf("known supply must not produce unknown_total_supply reason; got %v", out.RejectReasons)
	}
}

// TestStructuralReject_UnknownSupply_DisabledByFlag verifies that when
// reject_unknown_total_supply=false, an unknown supply is NOT a hard-reject
// (soft/log path instead).
func TestStructuralReject_UnknownSupply_DisabledByFlag(t *testing.T) {
	rt := runtimeWithEnforcement()
	rt.Thresholds.RejectUnknownTotalSupply = false // operator opt-out
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.TotalSupplyKnown = false
	in.CreatorPrevTokenCount = 0 // ensure no other structural reject fires
	in.SocialLinksKnown = true
	in.HasSocialLinks = true

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsString(out.RejectReasons, "unknown_total_supply") {
		t.Errorf("disabled flag must not produce unknown_total_supply reason; got %v", out.RejectReasons)
	}
}

// TestStructuralReject_UnknownSupply_DisabledByZeroMaxSupply verifies that
// when MaxTotalSupply=0 (check disabled), unknown supply is never rejected.
func TestStructuralReject_UnknownSupply_DisabledByZeroMaxSupply(t *testing.T) {
	rt := runtimeWithEnforcement()
	rt.Thresholds.MaxTotalSupply = 0 // supply check fully disabled
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.TotalSupplyKnown = false
	in.CreatorPrevTokenCount = 0
	in.SocialLinksKnown = true
	in.HasSocialLinks = true

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsString(out.RejectReasons, "unknown_total_supply") {
		t.Errorf("MaxTotalSupply=0 must disable supply checks; got %v", out.RejectReasons)
	}
}

// ── Holder count structural rejects ──────────────────────────────────────────

// TestStructuralReject_InsufficientHolders_Rejects confirms that a rescan
// token (non-new-launch) with HolderCount below MinHolderCount is rejected.
// Root cause: COOKING token had 1 holder after 7 hours — DQ never checked it.
func TestStructuralReject_InsufficientHolders_Rejects(t *testing.T) {
	rt := runtimeWithEnforcement()
	rt.Thresholds.MinHolderCount = 50
	rt.Thresholds.RejectUnknownHolderCount = false
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.EventTopic = "rescan_8h" // NOT a new launch — holder check applies
	in.HolderDistKnown = true
	in.HolderCount = 1 // COOKING had 1 holder after 7 hours

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsString(out.RejectReasons, "insufficient_holders") {
		t.Errorf("expected insufficient_holders in RejectReasons, got %v", out.RejectReasons)
	}
}

// TestStructuralReject_InsufficientHolders_SkippedForNewLaunch confirms the
// brand-new-launch exemption: PumpFunCreate events bypass the holder check
// because holder distribution has not yet settled at creation time.
func TestStructuralReject_InsufficientHolders_SkippedForNewLaunch(t *testing.T) {
	rt := runtimeWithEnforcement()
	rt.Thresholds.MinHolderCount = 50
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.EventTopic = "PumpFunCreate" // new launch — holder check must be skipped
	in.HolderDistKnown = true
	in.HolderCount = 1

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsString(out.RejectReasons, "insufficient_holders") {
		t.Errorf("new launch must not produce insufficient_holders; got %v", out.RejectReasons)
	}
}

// TestStructuralReject_UnknownHolderCount_RejectsWhenFlagEnabled confirms that
// HolderDistKnown=false triggers unknown_holder_count when the flag is set.
func TestStructuralReject_UnknownHolderCount_RejectsWhenFlagEnabled(t *testing.T) {
	rt := runtimeWithEnforcement()
	rt.Thresholds.MinHolderCount = 50
	rt.Thresholds.RejectUnknownHolderCount = true
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.EventTopic = "rescan_4h"
	in.HolderDistKnown = false // probe failed

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsString(out.RejectReasons, "unknown_holder_count") {
		t.Errorf("expected unknown_holder_count in RejectReasons, got %v", out.RejectReasons)
	}
}

// TestStructuralReject_UnknownHolderCount_SkippedWhenFlagDisabled confirms
// that HolderDistKnown=false is silent when RejectUnknownHolderCount=false.
func TestStructuralReject_UnknownHolderCount_SkippedWhenFlagDisabled(t *testing.T) {
	rt := runtimeWithEnforcement()
	rt.Thresholds.MinHolderCount = 50
	rt.Thresholds.RejectUnknownHolderCount = false
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.EventTopic = "rescan_4h"
	in.HolderDistKnown = false

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsString(out.RejectReasons, "unknown_holder_count") {
		t.Errorf("disabled flag must not produce unknown_holder_count; got %v", out.RejectReasons)
	}
}

// TestStructuralReject_HolderCountAboveThreshold_Passes confirms a token with
// sufficient holders passes the check without triggering any holder reject.
func TestStructuralReject_HolderCountAboveThreshold_Passes(t *testing.T) {
	rt := runtimeWithEnforcement()
	rt.Thresholds.MinHolderCount = 50
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := baseNewLaunch()
	in.EventTopic = "rescan_1h"
	in.HolderDistKnown = true
	in.HolderCount = 120 // above threshold

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsString(out.RejectReasons, "insufficient_holders") {
		t.Errorf("token with sufficient holders must not be rejected; got %v", out.RejectReasons)
	}
	if containsString(out.RejectReasons, "unknown_holder_count") {
		t.Errorf("known holder count must not produce unknown_holder_count; got %v", out.RejectReasons)
	}
}
