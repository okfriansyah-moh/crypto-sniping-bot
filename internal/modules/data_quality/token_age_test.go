package data_quality

import (
	"context"
	"fmt"
	"testing"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// runtimeWithMinAge returns a DataQualityRuntimeConfig with the token-age
// threshold set to the supplied value and all other fields at safe defaults.
func runtimeWithMinAge(minAgeSeconds int32) *config.DataQualityRuntimeConfig {
	return &config.DataQualityRuntimeConfig{
		PassThreshold:   0.50,
		RejectThreshold: 0.80,
		Detectors: config.DataQualityDetectorFlags{
			HoneypotSimulation: false,
			TaxAnomaly:         false,
			LpLock:             false,
			WashTrading:        false,
			RugAuthority:       false,
			DevReputation:      false,
		},
		Thresholds: config.DataQualityDetectorThresholds{
			MinTokenAgeSeconds: minAgeSeconds,
		},
		ModeProfiles: map[string]config.DataQualityModeProfile{
			"balanced": {RejectAbove: 0.80, RiskyPassAbove: 0.50, UnknownFactor: 0.0},
		},
	}
}

// dtoWithBlockTimestamp returns a minimal valid MarketDataDTO whose
// BlockTimestamp is set to the supplied time.
func dtoWithBlockTimestamp(ts time.Time) contracts.MarketDataDTO {
	return contracts.MarketDataDTO{
		EventID:        "age-test-1",
		TraceID:        "trace-age-1",
		CorrelationID:  "corr-age-1",
		VersionID:      "v1",
		TokenAddress:   "SoLToKen1111111111111111111111111111",
		Chain:          "solana",
		EventTopic:     "PumpFunCreate", // skip reserve checks
		BlockTimestamp: ts.UTC().Format(time.RFC3339Nano),
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// tokenAgeSeconds helper tests
// ─────────────────────────────────────────────────────────────────────────────

func TestTokenAgeSeconds_BlockTimestampUsed(t *testing.T) {
	past := time.Now().Add(-10 * time.Minute)
	age := tokenAgeSeconds(past.UTC().Format(time.RFC3339Nano), "")
	if age < 550 || age > 650 {
		t.Errorf("expected age ~600s, got %d", age)
	}
}

func TestTokenAgeSeconds_FallsBackToIngestedAt(t *testing.T) {
	past := time.Now().Add(-5 * time.Minute)
	age := tokenAgeSeconds("", past.UTC().Format(time.RFC3339Nano))
	if age < 250 || age > 350 {
		t.Errorf("expected age ~300s, got %d", age)
	}
}

func TestTokenAgeSeconds_BothEmpty_ReturnsMinusOne(t *testing.T) {
	age := tokenAgeSeconds("", "")
	if age != -1 {
		t.Errorf("expected -1 for empty timestamps, got %d", age)
	}
}

func TestTokenAgeSeconds_UnparsableTimestamp_ReturnsMinusOne(t *testing.T) {
	age := tokenAgeSeconds("not-a-timestamp", "also-bad")
	if age != -1 {
		t.Errorf("expected -1 for unparsable timestamps, got %d", age)
	}
}

func TestTokenAgeSeconds_FutureTimestamp_ReturnsZero(t *testing.T) {
	// Slight clock skew: block timestamp 30 seconds in the future.
	future := time.Now().Add(30 * time.Second)
	age := tokenAgeSeconds(future.UTC().Format(time.RFC3339Nano), "")
	if age != 0 {
		t.Errorf("expected 0 for future timestamp (clock skew guard), got %d", age)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ProcessForMode integration tests — min_token_age_seconds
// ─────────────────────────────────────────────────────────────────────────────

func TestMinTokenAge_TooYoung_Rejected(t *testing.T) {
	// Token created 5 minutes ago, threshold is 15 minutes → REJECT.
	m := New(defaultDQConfig(), nil).WithRuntimeConfig(runtimeWithMinAge(900))
	in := dtoWithBlockTimestamp(time.Now().Add(-5 * time.Minute))

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT for too-young token, got %s", out.Decision)
	}
	if !containsString(out.RejectReasons, "token_too_young") {
		t.Errorf("expected token_too_young in reject reasons, got %v", out.RejectReasons)
	}
}

func TestMinTokenAge_OldEnough_Passes(t *testing.T) {
	// Token created 20 minutes ago, threshold is 15 minutes → PASS.
	m := New(defaultDQConfig(), nil).WithRuntimeConfig(runtimeWithMinAge(900))
	in := dtoWithBlockTimestamp(time.Now().Add(-20 * time.Minute))

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision == "REJECT" && containsString(out.RejectReasons, "token_too_young") {
		t.Errorf("old-enough token should not be rejected as too_young; reasons: %v", out.RejectReasons)
	}
}

func TestMinTokenAge_ExactlyAtThreshold_Passes(t *testing.T) {
	// Token created exactly at the threshold boundary (900 s ago) → should NOT
	// be rejected as too_young (check is age < threshold, not age <=).
	m := New(defaultDQConfig(), nil).WithRuntimeConfig(runtimeWithMinAge(900))
	// Subtract a tiny buffer to land just past the boundary.
	in := dtoWithBlockTimestamp(time.Now().Add(-901 * time.Second))

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsString(out.RejectReasons, "token_too_young") {
		t.Errorf("token exactly past threshold should not have token_too_young; reasons: %v", out.RejectReasons)
	}
}

func TestMinTokenAge_ZeroDisablesCheck(t *testing.T) {
	// MinTokenAgeSeconds=0 → check disabled; brand-new token must not get
	// rejected for age.
	m := New(defaultDQConfig(), nil).WithRuntimeConfig(runtimeWithMinAge(0))
	in := dtoWithBlockTimestamp(time.Now().Add(-10 * time.Second))

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsString(out.RejectReasons, "token_too_young") {
		t.Errorf("age check disabled (0) should not produce token_too_young; reasons: %v", out.RejectReasons)
	}
}

func TestMinTokenAge_EmptyTimestamp_NoAgeReject(t *testing.T) {
	// Both timestamps empty → age unknown → check skipped, no false reject.
	m := New(defaultDQConfig(), nil).WithRuntimeConfig(runtimeWithMinAge(900))
	in := dtoWithBlockTimestamp(time.Time{}) // zero time → produces ""
	in.BlockTimestamp = ""                   // explicitly clear
	in.IngestedAt = ""

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsString(out.RejectReasons, "token_too_young") {
		t.Errorf("unknown age should not produce token_too_young; reasons: %v", out.RejectReasons)
	}
}

func TestMinTokenAge_Determinism(t *testing.T) {
	// Same input with same old-enough timestamp → identical output on two calls.
	m := New(defaultDQConfig(), nil).WithRuntimeConfig(runtimeWithMinAge(900))
	ts := time.Now().Add(-30 * time.Minute)
	in := dtoWithBlockTimestamp(ts)

	out1, err1 := m.ProcessForMode(context.Background(), in, "BALANCED")
	out2, err2 := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected errors: %v / %v", err1, err2)
	}
	if out1.Decision != out2.Decision {
		t.Errorf("non-deterministic: %s vs %s", out1.Decision, out2.Decision)
	}
	if fmt.Sprintf("%v", out1.RejectReasons) != fmt.Sprintf("%v", out2.RejectReasons) {
		t.Errorf("non-deterministic reasons: %v vs %v", out1.RejectReasons, out2.RejectReasons)
	}
}
