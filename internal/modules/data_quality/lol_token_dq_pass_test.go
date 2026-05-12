package data_quality

// lol_token_dq_pass_test.go — end-to-end happy-path and failure-mode tests
// using a realistic Solana token address (LOL: 34q2KmCvapecJgR6ZrtbCTrzZVtkt3a5mHEA3TuEsWYb).
//
// Purpose: verify that the full DQ structural-gate chain CAN pass a well-formed
// token when all probe data is populated correctly, and WILL reject when any
// mandatory criterion fails. This guards against regressions that make the
// pipeline 100%-reject even legitimate tokens.
//
// All tests are pure (no network, no DB) — probes are simulated by pre-populating
// the MarketDataDTO fields that probes would populate.

import (
	"context"
	"testing"
	"time"

	"crypto-sniping-bot/contracts"
)

const lolTokenCA = "34q2KmCvapecJgR6ZrtbCTrzZVtkt3a5mHEA3TuEsWYb"
const lolCreatorAddr = "9RpmMWjS5RGq4sqBjBfBmYhNRQ7KHy9SFZ8UVqQ3bJzE"

// lolTokenDTO returns a MarketDataDTO representing the LOL token after
// successful probe enrichment. All mandatory DQ fields are set to passing
// values that a legitimate first-launch token with social links would have.
func lolTokenDTO() contracts.MarketDataDTO {
	return contracts.MarketDataDTO{
		EventID:       "test-lol-event-1",
		TraceID:       "trace-lol-1",
		CorrelationID: "corr-lol-1",
		VersionID:     "v1",
		TokenAddress:  lolTokenCA,
		Chain:         "solana",
		// PumpFunCreate exempts the token from missing_reserves and
		// insufficient_liquidity checks — reserves not yet seeded.
		EventTopic:     "PumpFunCreate",
		ReserveBaseRaw: "",

		// ── Creator reputation (probe: solana_creator_reputation) ──────────
		// First-time launcher: 0 prior tokens; probe ran and succeeded.
		CreatorAddress:             lolCreatorAddr,
		CreatorPrevTokenCount:      0,
		CreatorPrevTokenCountKnown: true,

		// ── Social links (probe: solana_metadata) ──────────────────────────
		// Probe fetched the Arweave metadata and found Twitter + Telegram.
		MetadataURI:      "https://arweave.net/lol-token-metadata-hash",
		SocialLinksKnown: true,
		HasSocialLinks:   true,

		// ── Token supply (probe: solana_pumpfun_lp) ────────────────────────
		// Supply = 1 000 000 000 — exactly at the 1B threshold; NOT above it.
		TotalSupplyKnown: true,
		TotalSupply:      1_000_000_000,

		// ── Tax (known, zero-tax new launch) ───────────────────────────────
		TaxKnown:   true,
		BuyTaxBps:  0,
		SellTaxBps: 0,

		// ── Age: timestamps are empty, so tokenAgeSeconds returns -1 ───────
		// runtimeWithEnforcement() sets MinTokenAgeSeconds=0 (not enforced).
		// For production age testing see TestDQ_LOLToken_TooYoung below.
		BlockTimestamp: "",
		IngestedAt:     "",
	}
}

// TestDQ_LOLToken_PassesAllGatesInBALANCED verifies the complete happy path:
// all structural gates pass, risk score stays below the BALANCED threshold.
func TestDQ_LOLToken_PassesAllGatesInBALANCED(t *testing.T) {
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(runtimeWithEnforcement())

	out, err := m.ProcessForMode(context.Background(), lolTokenDTO(), "BALANCED")
	if err != nil {
		t.Fatalf("ProcessForMode error: %v", err)
	}
	if out.Decision == "REJECT" {
		t.Errorf("expected PASS or RISKY_PASS for well-formed LOL token, got REJECT; reject_reasons=%v flags=%v",
			out.RejectReasons, out.Flags)
	}
	if len(out.RejectReasons) > 0 {
		t.Errorf("expected empty reject_reasons, got: %v", out.RejectReasons)
	}
}

// TestDQ_LOLToken_PassesInSTRICT verifies the LOL token passes the tighter
// STRICT profile (RejectAbove=0.30) when ALL probe data is populated and
// shows no risk signals.
//
// In STRICT mode UnknownFactor=0.5: each unpopulated detector contributes
// 0.5×weight to the risk score. This test sets all detector-known flags to
// true with clean values so the score stays at 0.0.
func TestDQ_LOLToken_PassesInSTRICT(t *testing.T) {
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(runtimeWithEnforcement())

	dto := lolTokenDTO()
	// Populate all detector inputs so STRICT unknown-factor (0.5) does not
	// inflate the score above the 0.30 threshold.
	dto.HoneypotSimKnown = true // honeypot sim ran — buy+sell succeeded
	dto.BuySimSuccess = true
	dto.SellSimSuccess = true
	dto.LpLockKnown = true // LP lock known — locked
	dto.LpLocked = true
	dto.LpLockStrength = 1.0  // burned/permanent
	dto.WashStatsKnown = true // wash stats known — no suspicious pattern
	dto.TxCount1m = 50
	dto.UniqueWallets1m = 40          // high unique-wallet ratio → clean
	dto.SolanaAuthoritiesKnown = true // authorities known — renounced
	dto.MintAuthorityRenounced = true
	dto.FreezeAuthorityRenounced = true

	out, err := m.ProcessForMode(context.Background(), dto, "STRICT")
	if err != nil {
		t.Fatalf("ProcessForMode error: %v", err)
	}
	if out.Decision == "REJECT" {
		t.Errorf("expected PASS or RISKY_PASS in STRICT mode for fully-known clean LOL token, got REJECT; reject_reasons=%v risk_score=%.3f",
			out.RejectReasons, out.RiskScore)
	}
}

// TestDQ_LOLToken_RejectsWhenNoSocialLinks confirms no_social_links when the
// metadata probe found an empty/DEX-only metadata payload.
func TestDQ_LOLToken_RejectsWhenNoSocialLinks(t *testing.T) {
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(runtimeWithEnforcement())

	dto := lolTokenDTO()
	dto.SocialLinksKnown = true
	dto.HasSocialLinks = false // probe ran, no valid profile URL found

	out, err := m.ProcessForMode(context.Background(), dto, "BALANCED")
	if err != nil {
		t.Fatalf("ProcessForMode error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT when no social links, got %q", out.Decision)
	}
	if !containsString(out.RejectReasons, "no_social_links") {
		t.Errorf("expected no_social_links in reject_reasons, got %v", out.RejectReasons)
	}
}

// TestDQ_LOLToken_RejectsWhenUnknownSocialLinks confirms unknown_social_links
// when the metadata probe failed to fetch the URI (network error / IPFS timeout).
func TestDQ_LOLToken_RejectsWhenUnknownSocialLinks(t *testing.T) {
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(runtimeWithEnforcement())

	dto := lolTokenDTO()
	dto.SocialLinksKnown = false // probe failed — fail-closed
	dto.HasSocialLinks = false

	out, err := m.ProcessForMode(context.Background(), dto, "BALANCED")
	if err != nil {
		t.Fatalf("ProcessForMode error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT when social links unknown, got %q", out.Decision)
	}
	if !containsString(out.RejectReasons, "unknown_social_links") {
		t.Errorf("expected unknown_social_links in reject_reasons, got %v", out.RejectReasons)
	}
}

// TestDQ_LOLToken_RejectsWhenUnknownCreatorCount confirms unknown_creator_count
// when the creator reputation probe failed (pump.fun 530 + Helius DAS timeout).
func TestDQ_LOLToken_RejectsWhenUnknownCreatorCount(t *testing.T) {
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(runtimeWithEnforcement())

	dto := lolTokenDTO()
	dto.CreatorPrevTokenCountKnown = false // both creator probes failed — fail-closed

	out, err := m.ProcessForMode(context.Background(), dto, "BALANCED")
	if err != nil {
		t.Fatalf("ProcessForMode error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT when creator count unknown, got %q", out.Decision)
	}
	if !containsString(out.RejectReasons, "unknown_creator_count") {
		t.Errorf("expected unknown_creator_count in reject_reasons, got %v", out.RejectReasons)
	}
}

// TestDQ_LOLToken_RejectsSerialLauncher confirms serial_launcher when the
// creator already has ≥ 1 prior launch (threshold in runtimeWithEnforcement).
func TestDQ_LOLToken_RejectsSerialLauncher(t *testing.T) {
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(runtimeWithEnforcement())

	dto := lolTokenDTO()
	dto.CreatorPrevTokenCount = 1 // one prior launch → serial_launcher (threshold=1)

	out, err := m.ProcessForMode(context.Background(), dto, "BALANCED")
	if err != nil {
		t.Fatalf("ProcessForMode error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT for serial launcher, got %q", out.Decision)
	}
	if !containsString(out.RejectReasons, "serial_launcher") {
		t.Errorf("expected serial_launcher in reject_reasons, got %v", out.RejectReasons)
	}
}

// TestDQ_LOLToken_RejectsHighSupply confirms high_total_supply when supply > 1B.
func TestDQ_LOLToken_RejectsHighSupply(t *testing.T) {
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(runtimeWithEnforcement())

	dto := lolTokenDTO()
	dto.TotalSupplyKnown = true
	dto.TotalSupply = 1_000_000_001 // one above the 1B threshold

	out, err := m.ProcessForMode(context.Background(), dto, "BALANCED")
	if err != nil {
		t.Fatalf("ProcessForMode error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT for high supply, got %q", out.Decision)
	}
	if !containsString(out.RejectReasons, "high_total_supply") {
		t.Errorf("expected high_total_supply in reject_reasons, got %v", out.RejectReasons)
	}
}

// TestDQ_LOLToken_RejectsUnknownSupply confirms unknown_total_supply when the
// LP probe failed to decode the bonding curve account.
func TestDQ_LOLToken_RejectsUnknownSupply(t *testing.T) {
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(runtimeWithEnforcement())

	dto := lolTokenDTO()
	dto.TotalSupplyKnown = false // LP probe failed — fail-closed

	out, err := m.ProcessForMode(context.Background(), dto, "BALANCED")
	if err != nil {
		t.Fatalf("ProcessForMode error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT when supply unknown, got %q", out.Decision)
	}
	if !containsString(out.RejectReasons, "unknown_total_supply") {
		t.Errorf("expected unknown_total_supply in reject_reasons, got %v", out.RejectReasons)
	}
}

// TestDQ_LOLToken_TooYoung confirms token_too_young when MinTokenAgeSeconds
// is enforced and the token is only 60 seconds old.
func TestDQ_LOLToken_TooYoung(t *testing.T) {
	rt := runtimeWithEnforcement()
	rt.Thresholds.MinTokenAgeSeconds = 900 // mirror production 15-min gate
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	dto := lolTokenDTO()
	// Set timestamps so the token appears 60 seconds old (well under 900s).
	// Use wall-clock relative timestamps so the test stays valid regardless of
	// when it runs (tokenAgeSeconds uses time.Since internally).
	now := time.Now().UTC()
	dto.IngestedAt = now.Format(time.RFC3339)
	dto.BlockTimestamp = now.Add(-60 * time.Second).Format(time.RFC3339)

	out, err := m.ProcessForMode(context.Background(), dto, "BALANCED")
	if err != nil {
		t.Fatalf("ProcessForMode error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT for token_too_young, got %q (reject_reasons=%v)",
			out.Decision, out.RejectReasons)
	}
	if !containsString(out.RejectReasons, "token_too_young") {
		t.Errorf("expected token_too_young in reject_reasons, got %v", out.RejectReasons)
	}
}

// TestDQ_LOLToken_ExactlyAtSupplyThreshold verifies supply=1_000_000_000
// is NOT rejected (threshold is strictly >1B).
func TestDQ_LOLToken_ExactlyAtSupplyThreshold(t *testing.T) {
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(runtimeWithEnforcement())

	dto := lolTokenDTO()
	dto.TotalSupply = 1_000_000_000 // exactly at limit — must NOT reject

	out, err := m.ProcessForMode(context.Background(), dto, "BALANCED")
	if err != nil {
		t.Fatalf("ProcessForMode error: %v", err)
	}
	if containsString(out.RejectReasons, "high_total_supply") {
		t.Errorf("supply=1B exactly should not trigger high_total_supply, got reject_reasons=%v", out.RejectReasons)
	}
}

// TestDQ_LOLToken_AllProbesFailedRejectsWithAllReasons verifies that a token
// where every probe failed accumulates ALL fail-closed reject reasons.
func TestDQ_LOLToken_AllProbesFailedRejectsWithAllReasons(t *testing.T) {
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(runtimeWithEnforcement())

	dto := contracts.MarketDataDTO{
		EventID:        "test-lol-all-probes-failed",
		TokenAddress:   lolTokenCA,
		Chain:          "solana",
		EventTopic:     "PumpFunCreate",
		CreatorAddress: lolCreatorAddr,
		// All probes failed — all Known flags false
		CreatorPrevTokenCountKnown: false,
		SocialLinksKnown:           false,
		TotalSupplyKnown:           false,
	}

	out, err := m.ProcessForMode(context.Background(), dto, "BALANCED")
	if err != nil {
		t.Fatalf("ProcessForMode error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT when all probes failed, got %q", out.Decision)
	}
	for _, want := range []string{"unknown_creator_count", "unknown_social_links", "unknown_total_supply"} {
		if !containsString(out.RejectReasons, want) {
			t.Errorf("expected %q in reject_reasons, got %v", want, out.RejectReasons)
		}
	}
}
