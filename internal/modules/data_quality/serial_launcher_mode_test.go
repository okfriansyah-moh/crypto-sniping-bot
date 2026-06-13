package data_quality

// serial_launcher_mode_test.go — Task 13
//
// Tests for mode-aware serial launcher logic in ProcessForMode.
// Verifies the contract for all four operational modes:
//
//   STRICT/BALANCED: hard REJECT for known serial launchers and fail-closed
//   REJECT for unknown creator history (unchanged pre-Task-13 behaviour).
//
//   EXPLORATION/VERY_EXPLORATION: RISKY_PASS+serial_launcher_monitored when all
//   quality gates pass; SKIP+serial_launcher_skipped when any gate fails.
//   Unknown creator history → SKIP (not a quality failure).
//
// Each test case asserts Decision, Flags, and RejectionReasons exclusively.
// It does not assert exact RiskScore values because detector weights are config-driven.

import (
	"context"
	"testing"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// runtimeWithSerialLauncherProfiles returns a runtime config with:
//   - Global MaxCreatorPrevTokenCount=1 (STRICT/BALANCED threshold)
//   - RejectUnknownCreatorCount=true (mandatory fail-closed for STRICT/BALANCED)
//   - EXPLORATION profile: MaxCreatorPrevTokenCount=5, gates as per YAML Task 12
//   - VERY_EXPLORATION profile: MaxCreatorPrevTokenCount=10, slightly looser gates
//   - Mode profiles include RejectAbove/RiskyPassAbove so resolveProfile uses YAML override
func runtimeWithSerialLauncherProfiles() *config.DataQualityRuntimeConfig {
	rt := runtimeWithProfiles()
	rt.Thresholds.MaxCreatorPrevTokenCount = 1
	rt.Thresholds.RejectUnknownCreatorCount = true
	rt.ModeProfiles = map[string]config.DataQualityModeProfile{
		"strict": {
			RejectAbove:              0.30,
			RiskyPassAbove:           0.15,
			UnknownFactor:            0.5,
			MaxCreatorPrevTokenCount: 0, // sentinel → use global=1
		},
		"balanced": {
			RejectAbove:              0.50,
			RiskyPassAbove:           0.25,
			UnknownFactor:            0.0,
			MaxCreatorPrevTokenCount: 0, // sentinel → use global=1
		},
		"exploration": {
			RejectAbove:                       0.65,
			RiskyPassAbove:                    0.35,
			UnknownFactor:                     0.0,
			MinTokenAgeSeconds:                -1,
			MaxCreatorPrevTokenCount:          5,
			SerialLauncherRequiresSocialLinks: true,
			SerialLauncherMaxRiskScore:        0.40,
			SerialLauncherMinHolderCount:      50,
		},
		"very_exploration": {
			RejectAbove:                       0.75,
			RiskyPassAbove:                    0.45,
			UnknownFactor:                     0.0,
			MinTokenAgeSeconds:                -1,
			MaxCreatorPrevTokenCount:          10,
			SerialLauncherRequiresSocialLinks: true,
			SerialLauncherMaxRiskScore:        0.45,
			SerialLauncherMinHolderCount:      25,
		},
	}
	return rt
}

// cleanSerialLauncherToken returns a MarketDataDTO for a token whose creator
// has launched previous tokens but that otherwise passes all quality gates.
// Used as the base fixture for EXPLORATION-mode tests.
func cleanSerialLauncherToken(creatorCount int32) contracts.MarketDataDTO {
	return contracts.MarketDataDTO{
		EventID:        "evt-serial-1",
		TraceID:        "trace-sl-1",
		CorrelationID:  "corr-sl-1",
		VersionID:      "v1",
		TokenAddress:   "tokenSerialLauncher1",
		Chain:          "solana",
		EventTopic:     "PumpFunCreate",
		ReserveBaseRaw: "0",
		// Serial launcher: creator has launched N prior tokens
		CreatorAddress:             "serialDevWallet1",
		CreatorPrevTokenCountKnown: true,
		CreatorPrevTokenCount:      creatorCount,
		// Quality gates: social links present
		SocialLinksKnown: true,
		HasSocialLinks:   true,
		// Quality gates: holder distribution confirmed, count above EXPLORATION threshold
		HolderDistKnown: true,
		HolderCount:     100, // above EXPLORATION threshold (50) and VERY_EXPLORATION (25)
		// Clean token data: no honeypot, no tax anomaly, no wash, no rug
		HoneypotSimKnown: true,
		BuySimSuccess:    true,
		SellSimSuccess:   true,
		TaxKnown:         true,
		BuyTaxBps:        0,
		SellTaxBps:       0,
		TotalSupplyKnown: true,
		TotalSupply:      100_000_000, // 100M — below 1B limit
	}
}

// ── STRICT and BALANCED: unchanged hard-reject behaviour ──────────────────────

// TestSerialLauncherMode_StrictHardReject verifies that a known serial
// launcher (count=1 = global threshold) is always hard-rejected in STRICT mode.
func TestSerialLauncherMode_StrictHardReject(t *testing.T) {
	rt := runtimeWithSerialLauncherProfiles()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := cleanSerialLauncherToken(1) // count=1 ≥ global threshold=1
	out, err := m.ProcessForMode(context.Background(), in, "STRICT")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionReject {
		t.Errorf("STRICT: want REJECT for serial launcher, got %q (reasons=%v)", out.Decision, out.RejectReasons)
	}
	if !containsString(out.RejectReasons, "serial_launcher") {
		t.Errorf("STRICT: want serial_launcher in RejectReasons, got %v", out.RejectReasons)
	}
}

// TestSerialLauncherMode_BalancedHardReject verifies that a known serial
// launcher is hard-rejected in BALANCED mode even with social links present.
func TestSerialLauncherMode_BalancedHardReject(t *testing.T) {
	rt := runtimeWithSerialLauncherProfiles()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := cleanSerialLauncherToken(382) // extreme case: 382 prior launches
	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionReject {
		t.Errorf("BALANCED: want REJECT for serial launcher (382 tokens), got %q", out.Decision)
	}
	if !containsString(out.RejectReasons, "serial_launcher") {
		t.Errorf("BALANCED: want serial_launcher in RejectReasons, got %v", out.RejectReasons)
	}
}

// TestSerialLauncherMode_StrictUnknownCreatorReject verifies fail-closed REJECT
// for unknown creator history in STRICT mode.
func TestSerialLauncherMode_StrictUnknownCreatorReject(t *testing.T) {
	rt := runtimeWithSerialLauncherProfiles()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := cleanSerialLauncherToken(0)
	in.CreatorPrevTokenCountKnown = false // probe timed out

	out, err := m.ProcessForMode(context.Background(), in, "STRICT")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionReject {
		t.Errorf("STRICT: want REJECT for unknown creator, got %q", out.Decision)
	}
	if !containsString(out.RejectReasons, "unknown_creator_count") {
		t.Errorf("STRICT: want unknown_creator_count in RejectReasons, got %v", out.RejectReasons)
	}
}

// ── EXPLORATION: mode-aware routing ──────────────────────────────────────────

// TestSerialLauncherMode_ExplorationQualityGatesPass_RiskyPass verifies that a
// serial-launcher token in EXPLORATION mode with all quality gates satisfied
// receives RISKY_PASS + serial_launcher_monitored flag (not REJECT, not SKIP).
func TestSerialLauncherMode_ExplorationQualityGatesPass_RiskyPass(t *testing.T) {
	rt := runtimeWithSerialLauncherProfiles()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := cleanSerialLauncherToken(5) // count=5 = EXPLORATION threshold
	out, err := m.ProcessForMode(context.Background(), in, "EXPLORATION")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionRiskyPass {
		t.Errorf("EXPLORATION gates pass: want RISKY_PASS, got %q (flags=%v, reasons=%v)",
			out.Decision, out.Flags, out.RejectReasons)
	}
	if !containsString(out.Flags, contracts.FlagSerialLauncherMonitored) {
		t.Errorf("EXPLORATION gates pass: want serial_launcher_monitored flag, got %v", out.Flags)
	}
	if len(out.RejectReasons) != 0 {
		t.Errorf("EXPLORATION gates pass: want empty RejectReasons, got %v", out.RejectReasons)
	}
}

// TestSerialLauncherMode_ExplorationNoSocialLinks_Skip verifies that a
// serial-launcher token without confirmed social links in EXPLORATION mode
// receives SKIP + serial_launcher_skipped (not REJECT).
func TestSerialLauncherMode_ExplorationNoSocialLinks_Skip(t *testing.T) {
	rt := runtimeWithSerialLauncherProfiles()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := cleanSerialLauncherToken(5)
	in.SocialLinksKnown = true
	in.HasSocialLinks = false // no social links → quality gate fails

	out, err := m.ProcessForMode(context.Background(), in, "EXPLORATION")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionSkip {
		t.Errorf("EXPLORATION no social links: want SKIP, got %q", out.Decision)
	}
	if !containsString(out.Flags, contracts.FlagSerialLauncherSkipped) {
		t.Errorf("EXPLORATION no social links: want serial_launcher_skipped flag, got %v", out.Flags)
	}
	if len(out.RejectReasons) != 0 {
		t.Errorf("EXPLORATION SKIP: want nil RejectReasons (not a quality failure), got %v", out.RejectReasons)
	}
}

// TestSerialLauncherMode_ExplorationSocialLinksUnknown_Skip verifies that
// unknown social link status (probe timed out) triggers SKIP in EXPLORATION mode.
func TestSerialLauncherMode_ExplorationSocialLinksUnknown_Skip(t *testing.T) {
	rt := runtimeWithSerialLauncherProfiles()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := cleanSerialLauncherToken(5)
	in.SocialLinksKnown = false // probe timed out → unknown

	out, err := m.ProcessForMode(context.Background(), in, "EXPLORATION")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionSkip {
		t.Errorf("EXPLORATION social links unknown: want SKIP, got %q", out.Decision)
	}
	if !containsString(out.Flags, contracts.FlagSerialLauncherSkipped) {
		t.Errorf("EXPLORATION social links unknown: want serial_launcher_skipped flag, got %v", out.Flags)
	}
}

// TestSerialLauncherMode_ExplorationInsufficientHolders_Skip verifies that a
// serial-launcher token with fewer than 50 holders in EXPLORATION mode receives SKIP.
func TestSerialLauncherMode_ExplorationInsufficientHolders_Skip(t *testing.T) {
	rt := runtimeWithSerialLauncherProfiles()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := cleanSerialLauncherToken(5)
	in.HolderDistKnown = true
	in.HolderCount = 20 // below EXPLORATION threshold (50) → quality gate fails

	out, err := m.ProcessForMode(context.Background(), in, "EXPLORATION")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionSkip {
		t.Errorf("EXPLORATION insufficient holders: want SKIP, got %q", out.Decision)
	}
	if !containsString(out.Flags, contracts.FlagSerialLauncherSkipped) {
		t.Errorf("EXPLORATION insufficient holders: want serial_launcher_skipped flag, got %v", out.Flags)
	}
}

// TestSerialLauncherMode_ExplorationUnknownCreator_Skip verifies that an
// unknown creator in EXPLORATION mode produces SKIP, not REJECT.
// Unknown history is not a quality failure in exploration mode.
func TestSerialLauncherMode_ExplorationUnknownCreator_Skip(t *testing.T) {
	rt := runtimeWithSerialLauncherProfiles()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := cleanSerialLauncherToken(0)
	in.CreatorPrevTokenCountKnown = false // probe timed out

	out, err := m.ProcessForMode(context.Background(), in, "EXPLORATION")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionSkip {
		t.Errorf("EXPLORATION unknown creator: want SKIP, got %q", out.Decision)
	}
	if !containsString(out.Flags, contracts.FlagSerialLauncherSkipped) {
		t.Errorf("EXPLORATION unknown creator: want serial_launcher_skipped flag, got %v", out.Flags)
	}
	// SKIP must not have reject reasons.
	if len(out.RejectReasons) != 0 {
		t.Errorf("EXPLORATION SKIP: want nil RejectReasons, got %v", out.RejectReasons)
	}
}

// TestSerialLauncherMode_ExplorationBelowEffectiveMax_NormalProcessing verifies
// that a creator with 2 prior tokens (below EXPLORATION threshold=5) is NOT
// treated as a serial launcher in EXPLORATION mode and proceeds to normal scoring.
func TestSerialLauncherMode_ExplorationBelowEffectiveMax_NormalProcessing(t *testing.T) {
	rt := runtimeWithSerialLauncherProfiles()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := cleanSerialLauncherToken(2) // 2 < EXPLORATION threshold(5) → not serial
	out, err := m.ProcessForMode(context.Background(), in, "EXPLORATION")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Decision must not be SKIP (serial launcher check does not trigger).
	if out.Decision == contracts.DecisionSkip {
		t.Errorf("EXPLORATION count<threshold: must not produce SKIP (serial launcher should not trigger), got %q", out.Decision)
	}
	// Decision must not be REJECT for serial_launcher reason.
	if containsString(out.RejectReasons, "serial_launcher") {
		t.Errorf("EXPLORATION count<threshold: must not have serial_launcher reject reason, got %v", out.RejectReasons)
	}
	// Must not have serial_launcher flags.
	if containsString(out.Flags, contracts.FlagSerialLauncherMonitored) || containsString(out.Flags, contracts.FlagSerialLauncherSkipped) {
		t.Errorf("EXPLORATION count<threshold: must not have serial_launcher flags, got %v", out.Flags)
	}
}

// ── VERY_EXPLORATION: looser thresholds ──────────────────────────────────────

// TestSerialLauncherMode_VeryExplorationQualityGatesPass_RiskyPass verifies that
// a serial-launcher token (count=10) in VERY_EXPLORATION mode with all quality
// gates satisfied receives RISKY_PASS + serial_launcher_monitored.
func TestSerialLauncherMode_VeryExplorationQualityGatesPass_RiskyPass(t *testing.T) {
	rt := runtimeWithSerialLauncherProfiles()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := cleanSerialLauncherToken(10) // count=10 = VERY_EXPLORATION threshold
	out, err := m.ProcessForMode(context.Background(), in, "VERY_EXPLORATION")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionRiskyPass {
		t.Errorf("VERY_EXPLORATION gates pass: want RISKY_PASS, got %q (flags=%v, reasons=%v)",
			out.Decision, out.Flags, out.RejectReasons)
	}
	if !containsString(out.Flags, contracts.FlagSerialLauncherMonitored) {
		t.Errorf("VERY_EXPLORATION gates pass: want serial_launcher_monitored flag, got %v", out.Flags)
	}
}

// TestSerialLauncherMode_VeryExplorationInsufficientHolders_Skip verifies that
// a serial-launcher token with fewer than 25 holders in VERY_EXPLORATION mode
// receives SKIP (threshold is 25, vs 50 in EXPLORATION).
func TestSerialLauncherMode_VeryExplorationInsufficientHolders_Skip(t *testing.T) {
	rt := runtimeWithSerialLauncherProfiles()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := cleanSerialLauncherToken(10)
	in.HolderDistKnown = true
	in.HolderCount = 10 // below VERY_EXPLORATION threshold (25)

	out, err := m.ProcessForMode(context.Background(), in, "VERY_EXPLORATION")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionSkip {
		t.Errorf("VERY_EXPLORATION insufficient holders: want SKIP, got %q", out.Decision)
	}
	if !containsString(out.Flags, contracts.FlagSerialLauncherSkipped) {
		t.Errorf("VERY_EXPLORATION insufficient holders: want serial_launcher_skipped flag, got %v", out.Flags)
	}
}

// TestSerialLauncherMode_VeryExplorationUnknownCreator_Skip verifies SKIP
// for unknown creator in VERY_EXPLORATION mode.
func TestSerialLauncherMode_VeryExplorationUnknownCreator_Skip(t *testing.T) {
	rt := runtimeWithSerialLauncherProfiles()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := cleanSerialLauncherToken(0)
	in.CreatorPrevTokenCountKnown = false

	out, err := m.ProcessForMode(context.Background(), in, "VERY_EXPLORATION")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionSkip {
		t.Errorf("VERY_EXPLORATION unknown creator: want SKIP, got %q", out.Decision)
	}
}

// ── Traceability and DTO contract ────────────────────────────────────────────

// TestSerialLauncherMode_SkipDTOContract verifies that a SKIP result has
// the required structural properties: no RejectReasons, serial_launcher_skipped
// flag, and all four traceability fields propagated from the input DTO.
func TestSerialLauncherMode_SkipDTOContract(t *testing.T) {
	rt := runtimeWithSerialLauncherProfiles()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := cleanSerialLauncherToken(5)
	in.TraceID = "trace-contract-1"
	in.CorrelationID = "corr-contract-1"
	in.VersionID = "v-contract-1"
	in.SocialLinksKnown = true
	in.HasSocialLinks = false // force SKIP

	out, err := m.ProcessForMode(context.Background(), in, "EXPLORATION")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionSkip {
		t.Fatalf("want SKIP, got %q", out.Decision)
	}
	// Traceability fields must be propagated.
	if out.TraceID != in.TraceID {
		t.Errorf("TraceID: want %q, got %q", in.TraceID, out.TraceID)
	}
	if out.CorrelationID != in.CorrelationID {
		t.Errorf("CorrelationID: want %q, got %q", in.CorrelationID, out.CorrelationID)
	}
	if out.VersionID != in.VersionID {
		t.Errorf("VersionID: want %q, got %q", in.VersionID, out.VersionID)
	}
	// CausationID must be the input EventID.
	if out.CausationID != in.EventID {
		t.Errorf("CausationID: want input EventID %q, got %q", in.EventID, out.CausationID)
	}
	// SKIP must not have reject reasons.
	if len(out.RejectReasons) != 0 {
		t.Errorf("SKIP RejectReasons must be nil, got %v", out.RejectReasons)
	}
	// Token address and chain must be propagated.
	if out.TokenAddress != in.TokenAddress {
		t.Errorf("TokenAddress: want %q, got %q", in.TokenAddress, out.TokenAddress)
	}
	if out.Chain != in.Chain {
		t.Errorf("Chain: want %q, got %q", in.Chain, out.Chain)
	}
}

// TestSerialLauncherMode_RiskyPassFlagInFlags verifies that the RISKY_PASS
// result from EXPLORATION mode carries serial_launcher_monitored in Flags
// and that it is NOT in RejectReasons.
func TestSerialLauncherMode_RiskyPassFlagInFlags(t *testing.T) {
	rt := runtimeWithSerialLauncherProfiles()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := cleanSerialLauncherToken(5)
	out, err := m.ProcessForMode(context.Background(), in, "EXPLORATION")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionRiskyPass {
		t.Fatalf("want RISKY_PASS, got %q", out.Decision)
	}
	if containsString(out.RejectReasons, "serial_launcher") {
		t.Errorf("RISKY_PASS: serial_launcher must not appear in RejectReasons, got %v", out.RejectReasons)
	}
	if !containsString(out.Flags, contracts.FlagSerialLauncherMonitored) {
		t.Errorf("RISKY_PASS: want serial_launcher_monitored in Flags, got %v", out.Flags)
	}
}

// ── VERY_EXPLORATION Task 22 hotfix: relaxed quality gates ───────────────────

// runtimeWithRelaxedVeryExplorationProfiles returns a runtime config where the
// VERY_EXPLORATION profile has Task 22 hotfix values applied:
//   - SerialLauncherRequiresSocialLinks: false (gate inert)
//   - SerialLauncherMinHolderCount: 0 (gate inert)
//
// EXPLORATION, STRICT, and BALANCED are unchanged.
func runtimeWithRelaxedVeryExplorationProfiles() *config.DataQualityRuntimeConfig {
	rt := runtimeWithSerialLauncherProfiles()
	rt.ModeProfiles["very_exploration"] = config.DataQualityModeProfile{
		RejectAbove:                       0.75,
		RiskyPassAbove:                    0.45,
		UnknownFactor:                     0.0,
		MinTokenAgeSeconds:                -1,
		MaxCreatorPrevTokenCount:          10,
		SerialLauncherRequiresSocialLinks: false, // Task 22 hotfix: gate inert
		SerialLauncherMaxRiskScore:        0.45,
		SerialLauncherMinHolderCount:      0, // Task 22 hotfix: gate inert
	}
	return rt
}

// TestSerialLauncherMode_VeryExploration_NoSocialLinks_RiskyPassAfterTask22Hotfix
// verifies that after the Task 22 hotfix (serial_launcher_requires_social_links:
// false in VERY_EXPLORATION), a serial-launcher token with no confirmed social
// links receives RISKY_PASS + serial_launcher_monitored instead of SKIP.
// This is the core regression test for the hotfix — absence of social links
// must NOT gate the token in VERY_EXPLORATION mode.
func TestSerialLauncherMode_VeryExploration_NoSocialLinks_RiskyPassAfterTask22Hotfix(t *testing.T) {
	rt := runtimeWithRelaxedVeryExplorationProfiles()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := cleanSerialLauncherToken(10)
	in.SocialLinksKnown = true
	in.HasSocialLinks = false // no social links — gate disabled in VERY_EXPLORATION post-hotfix

	out, err := m.ProcessForMode(context.Background(), in, "VERY_EXPLORATION")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionRiskyPass {
		t.Errorf("VERY_EXPLORATION hotfix no-social-links: want RISKY_PASS (gate disabled), got %q (flags=%v, reasons=%v)",
			out.Decision, out.Flags, out.RejectReasons)
	}
	if !containsString(out.Flags, contracts.FlagSerialLauncherMonitored) {
		t.Errorf("VERY_EXPLORATION hotfix no-social-links: want serial_launcher_monitored, got %v", out.Flags)
	}
	if len(out.RejectReasons) != 0 {
		t.Errorf("VERY_EXPLORATION hotfix no-social-links: want empty RejectReasons, got %v", out.RejectReasons)
	}
}

// TestSerialLauncherMode_VeryExploration_ZeroHolders_RiskyPassAfterTask22Hotfix
// verifies that after the Task 22 hotfix (serial_launcher_min_holder_count: 0
// in VERY_EXPLORATION), a serial-launcher token with zero or few holders
// receives RISKY_PASS instead of SKIP. Simulates solana_holder_dist probe
// timeout where HolderDistKnown=true but HolderCount=0 (worst case).
func TestSerialLauncherMode_VeryExploration_ZeroHolders_RiskyPassAfterTask22Hotfix(t *testing.T) {
	rt := runtimeWithRelaxedVeryExplorationProfiles()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := cleanSerialLauncherToken(10)
	in.HolderDistKnown = true
	in.HolderCount = 0 // holder-count gate disabled in VERY_EXPLORATION post-hotfix

	out, err := m.ProcessForMode(context.Background(), in, "VERY_EXPLORATION")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionRiskyPass {
		t.Errorf("VERY_EXPLORATION hotfix zero-holders: want RISKY_PASS (gate disabled), got %q (flags=%v, reasons=%v)",
			out.Decision, out.Flags, out.RejectReasons)
	}
	if !containsString(out.Flags, contracts.FlagSerialLauncherMonitored) {
		t.Errorf("VERY_EXPLORATION hotfix zero-holders: want serial_launcher_monitored, got %v", out.Flags)
	}
}

// TestSerialLauncherMode_VeryExploration_NoSocialLinksAndZeroHolders_RiskyPassAfterTask22Hotfix
// verifies the combined case: both gates disabled simultaneously in VERY_EXPLORATION.
// A token with no social links AND zero holders must still produce RISKY_PASS.
func TestSerialLauncherMode_VeryExploration_NoSocialLinksAndZeroHolders_RiskyPassAfterTask22Hotfix(t *testing.T) {
	rt := runtimeWithRelaxedVeryExplorationProfiles()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := cleanSerialLauncherToken(10)
	in.SocialLinksKnown = true
	in.HasSocialLinks = false // no social links
	in.HolderDistKnown = true
	in.HolderCount = 0 // zero holders

	out, err := m.ProcessForMode(context.Background(), in, "VERY_EXPLORATION")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionRiskyPass {
		t.Errorf("VERY_EXPLORATION hotfix both-gates-disabled: want RISKY_PASS, got %q (flags=%v, reasons=%v)",
			out.Decision, out.Flags, out.RejectReasons)
	}
}

// TestSerialLauncherMode_ExplorationGatesUnchangedByTask22Hotfix verifies that
// the Task 22 hotfix does NOT relax EXPLORATION mode — social-links gate and
// holder-count gate remain active in EXPLORATION (only VERY_EXPLORATION relaxed).
func TestSerialLauncherMode_ExplorationGatesUnchangedByTask22Hotfix(t *testing.T) {
	// Use the relaxed VERY_EXPLORATION config — EXPLORATION profile is identical.
	rt := runtimeWithRelaxedVeryExplorationProfiles()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := cleanSerialLauncherToken(5)
	in.SocialLinksKnown = true
	in.HasSocialLinks = false // no social links → must still SKIP in EXPLORATION

	out, err := m.ProcessForMode(context.Background(), in, "EXPLORATION")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionSkip {
		t.Errorf("EXPLORATION must still SKIP for no-social-links after Task 22 hotfix, got %q", out.Decision)
	}
	if !containsString(out.Flags, contracts.FlagSerialLauncherSkipped) {
		t.Errorf("EXPLORATION: want serial_launcher_skipped flag after hotfix, got %v", out.Flags)
	}
}

// runtimeWithTask28TightenedProfiles returns a runtime config where the
// VERY_EXPLORATION serial-launcher gates have been re-tightened per Task 28:
// requires_social_links=true and min_holder_count=25.
func runtimeWithTask28TightenedProfiles() *config.DataQualityRuntimeConfig {
	rt := runtimeWithRelaxedVeryExplorationProfiles()
	vp := rt.ModeProfiles["very_exploration"]
	vp.SerialLauncherRequiresSocialLinks = true
	vp.SerialLauncherMinHolderCount = 25
	rt.ModeProfiles["very_exploration"] = vp
	return rt
}

// TestSerialLauncherMode_VeryExploration_NoSocialLinks_SkipAfterTask28 verifies
// that after Task 28 re-tightens the VERY_EXPLORATION gates, a serial-launcher
// token with no confirmed social links receives SKIP (not RISKY_PASS).
func TestSerialLauncherMode_VeryExploration_NoSocialLinks_SkipAfterTask28(t *testing.T) {
	rt := runtimeWithTask28TightenedProfiles()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := cleanSerialLauncherToken(10)
	in.SocialLinksKnown = true
	in.HasSocialLinks = false // no social links — gate re-enabled in Task 28

	out, err := m.ProcessForMode(context.Background(), in, "VERY_EXPLORATION")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionSkip {
		t.Errorf("VERY_EXPLORATION Task28 no-social-links: want SKIP (gate re-enabled), got %q (flags=%v, reasons=%v)",
			out.Decision, out.Flags, out.RejectReasons)
	}
	if !containsString(out.Flags, contracts.FlagSerialLauncherSkipped) {
		t.Errorf("VERY_EXPLORATION Task28 no-social-links: want serial_launcher_skipped flag, got %v", out.Flags)
	}
}

// TestSerialLauncherMode_VeryExploration_LowHolderCount_SkipAfterTask28 verifies
// that after Task 28 re-tightens the holder-count floor to 25, a serial-launcher
// token with holder_count < 25 receives SKIP in VERY_EXPLORATION mode.
func TestSerialLauncherMode_VeryExploration_LowHolderCount_SkipAfterTask28(t *testing.T) {
	rt := runtimeWithTask28TightenedProfiles()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := cleanSerialLauncherToken(10)
	in.SocialLinksKnown = true
	in.HasSocialLinks = true
	in.HolderDistKnown = true
	in.HolderCount = 10 // below re-tightened floor of 25

	out, err := m.ProcessForMode(context.Background(), in, "VERY_EXPLORATION")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionSkip {
		t.Errorf("VERY_EXPLORATION Task28 low-holder-count: want SKIP (floor=25, count=10), got %q (flags=%v, reasons=%v)",
			out.Decision, out.Flags, out.RejectReasons)
	}
}

// TestSerialLauncherMode_VeryExploration_SufficientHolders_RiskyPassAfterTask28
// verifies that a serial-launcher token with ≥ 25 holders AND social links
// still receives RISKY_PASS in VERY_EXPLORATION after Task 28 re-tighten.
func TestSerialLauncherMode_VeryExploration_SufficientHolders_RiskyPassAfterTask28(t *testing.T) {
	rt := runtimeWithTask28TightenedProfiles()
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := cleanSerialLauncherToken(10)
	in.SocialLinksKnown = true
	in.HasSocialLinks = true
	in.HolderDistKnown = true
	in.HolderCount = 30 // meets re-tightened floor of 25

	out, err := m.ProcessForMode(context.Background(), in, "VERY_EXPLORATION")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != contracts.DecisionRiskyPass {
		t.Errorf("VERY_EXPLORATION Task28 sufficient-holders: want RISKY_PASS, got %q (flags=%v, reasons=%v)",
			out.Decision, out.Flags, out.RejectReasons)
	}
	if !containsString(out.Flags, contracts.FlagSerialLauncherMonitored) {
		t.Errorf("VERY_EXPLORATION Task28 sufficient-holders: want serial_launcher_monitored flag, got %v", out.Flags)
	}
}
