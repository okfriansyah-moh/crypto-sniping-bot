package data_quality

import (
	"context"
	"errors"
	"testing"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// ── CreatorProfileReader stub ─────────────────────────────────────────────────

// stubCreatorProfileReader implements CreatorProfileReader for tests.
type stubCreatorProfileReader struct {
	count int32
	known bool
	err   error
}

func (s *stubCreatorProfileReader) GetCount(_ context.Context, _, _ string) (int32, bool, error) {
	return s.count, s.known, s.err
}

// runtimeForProfileTests returns a minimal runtime config with the
// serial-launcher threshold set to 1 (production default) so that a
// CreatorPrevTokenCount ≥ 1 triggers a REJECT in BALANCED mode.
func runtimeForProfileTests() *config.DataQualityRuntimeConfig {
	rt := &config.DataQualityRuntimeConfig{}
	rt.Detectors.DevReputation = true
	rt.Thresholds.MaxCreatorPrevTokenCount = 1
	rt.Thresholds.RejectUnknownCreatorCount = false // keep tests focused on profile path
	return rt
}

// baseNewLaunchForProfileTest mirrors baseNewLaunch but avoids the social-link
// and supply hard-rejects so the serial-launcher check is the only gate under test.
func baseNewLaunchForProfileTest() contracts.MarketDataDTO {
	return contracts.MarketDataDTO{
		EventID:                    "evt-profile-1",
		TraceID:                    "trace-1",
		CorrelationID:              "corr-1",
		VersionID:                  "v1",
		TokenAddress:               "profileToken1",
		Chain:                      "solana",
		EventTopic:                 "PumpFunCreate",
		ReserveBaseRaw:             "0",
		CreatorAddress:             "creatorWalletA",
		CreatorPrevTokenCountKnown: false, // probe did not run
		CreatorPrevTokenCount:      0,
		SocialLinksKnown:           true,
		HasSocialLinks:             true,
		TotalSupplyKnown:           true,
		TotalSupply:                500_000_000, // below 1B max
	}
}

// ── TestProcessForMode_UsesProfileCountWhenAvailable ─────────────────────────

// TestProcessForMode_UsesProfileCountWhenAvailable verifies that when the
// creator_profiles reader returns a count ≥ threshold, ProcessForMode rejects
// the token with serial_launcher even though the probe did not set Known=true.
func TestProcessForMode_UsesProfileCountWhenAvailable(t *testing.T) {
	rt := runtimeForProfileTests()
	reader := &stubCreatorProfileReader{count: 3, known: true}
	m := New(DefaultConfig(nil), nil).
		WithRuntimeConfig(rt).
		WithCreatorProfileReader(reader)

	in := baseNewLaunchForProfileTest()
	// Probe did not fire — CreatorPrevTokenCountKnown=false, count=0.
	// Profile reader returns count=3, known=true → module must use 3.

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT (profile count=3 ≥ threshold=1), got Decision=%q reasons=%v",
			out.Decision, out.RejectReasons)
	}
	if !containsString(out.RejectReasons, "serial_launcher") {
		t.Errorf("expected serial_launcher in RejectReasons, got %v", out.RejectReasons)
	}
}

// TestProcessForMode_UsesLargerOfProbeAndProfileCount verifies that when both
// probe and profile are known, the larger count is used for the check.
func TestProcessForMode_UsesLargerOfProbeAndProfileCount(t *testing.T) {
	rt := runtimeForProfileTests()
	rt.Thresholds.MaxCreatorPrevTokenCount = 5                 // raise bar so only the larger triggers
	reader := &stubCreatorProfileReader{count: 7, known: true} // profile > probe
	m := New(DefaultConfig(nil), nil).
		WithRuntimeConfig(rt).
		WithCreatorProfileReader(reader)

	in := baseNewLaunchForProfileTest()
	in.CreatorPrevTokenCountKnown = true
	in.CreatorPrevTokenCount = 2 // probe says 2; profile says 7 → use 7

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT (profile count=7 ≥ threshold=5), got Decision=%q reasons=%v",
			out.Decision, out.RejectReasons)
	}
	if !containsString(out.RejectReasons, "serial_launcher") {
		t.Errorf("expected serial_launcher in RejectReasons, got %v", out.RejectReasons)
	}
}

// TestProcessForMode_ProbeCountWinsWhenLarger verifies that when the probe
// count is larger than the profile count, the probe count is used (no regression).
func TestProcessForMode_ProbeCountWinsWhenLarger(t *testing.T) {
	rt := runtimeForProfileTests()
	rt.Thresholds.MaxCreatorPrevTokenCount = 5
	reader := &stubCreatorProfileReader{count: 2, known: true} // profile < probe
	m := New(DefaultConfig(nil), nil).
		WithRuntimeConfig(rt).
		WithCreatorProfileReader(reader)

	in := baseNewLaunchForProfileTest()
	in.CreatorPrevTokenCountKnown = true
	in.CreatorPrevTokenCount = 8 // probe says 8 → 8 ≥ 5 → REJECT

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT (probe count=8 ≥ threshold=5), got Decision=%q reasons=%v",
			out.Decision, out.RejectReasons)
	}
}

// ── TestProcessForMode_FallsBackToProbeWhenProfileUnknown ────────────────────

// TestProcessForMode_FallsBackToProbeWhenProfileUnknown verifies that when
// the reader returns known=false, existing probe-based semantics are
// unchanged — if the probe also didn't set Known, the token is not rejected
// by the serial-launcher check.
func TestProcessForMode_FallsBackToProbeWhenProfileUnknown(t *testing.T) {
	rt := runtimeForProfileTests()
	rt.Thresholds.RejectUnknownCreatorCount = false // explicitly disable fail-closed for this test
	reader := &stubCreatorProfileReader{count: 0, known: false}
	m := New(DefaultConfig(nil), nil).
		WithRuntimeConfig(rt).
		WithCreatorProfileReader(reader)

	in := baseNewLaunchForProfileTest()
	// Both probe and profile unknown → serial_launcher check does not fire.

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containsString(out.RejectReasons, "serial_launcher") {
		t.Errorf("serial_launcher must not fire when both probe and profile are unknown, got reasons=%v",
			out.RejectReasons)
	}
}

// TestProcessForMode_FallsBackToProbeKnownWhenProfileUnknown verifies that a
// probe-known count is still used when the profile reader returns known=false.
func TestProcessForMode_FallsBackToProbeKnownWhenProfileUnknown(t *testing.T) {
	rt := runtimeForProfileTests()
	reader := &stubCreatorProfileReader{count: 0, known: false}
	m := New(DefaultConfig(nil), nil).
		WithRuntimeConfig(rt).
		WithCreatorProfileReader(reader)

	in := baseNewLaunchForProfileTest()
	in.CreatorPrevTokenCountKnown = true
	in.CreatorPrevTokenCount = 3 // probe says 3 ≥ threshold 1 → REJECT

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT via probe path when profile unknown, got Decision=%q reasons=%v",
			out.Decision, out.RejectReasons)
	}
	if !containsString(out.RejectReasons, "serial_launcher") {
		t.Errorf("expected serial_launcher in RejectReasons, got %v", out.RejectReasons)
	}
}

// ── TestProcessForMode_FailClosedWhenReaderErrors ─────────────────────────────

// TestProcessForMode_FailClosedWhenReaderErrors verifies that when the profile
// reader returns an error, CreatorPrevTokenCountKnown is left unchanged and
// the serial-launcher check falls back to probe-only semantics — no panic,
// no spurious reject from the error itself.
func TestProcessForMode_FailClosedWhenReaderErrors(t *testing.T) {
	rt := runtimeForProfileTests()
	rt.Thresholds.RejectUnknownCreatorCount = false
	reader := &stubCreatorProfileReader{err: errors.New("db connection error")}
	m := New(DefaultConfig(nil), nil).
		WithRuntimeConfig(rt).
		WithCreatorProfileReader(reader)

	in := baseNewLaunchForProfileTest()
	// Probe also not set — all unknown. Error must not cause REJECT.

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error from ProcessForMode: %v", err)
	}
	if containsString(out.RejectReasons, "serial_launcher") {
		t.Errorf("serial_launcher must not fire on reader error when probe also unknown, got reasons=%v",
			out.RejectReasons)
	}
}

// TestProcessForMode_ReaderErrorDoesNotChangeKnownProbeCount verifies that
// a reader error does not clobber a probe-set count — probe-REJECT is preserved.
func TestProcessForMode_ReaderErrorDoesNotChangeKnownProbeCount(t *testing.T) {
	rt := runtimeForProfileTests()
	reader := &stubCreatorProfileReader{err: errors.New("timeout")}
	m := New(DefaultConfig(nil), nil).
		WithRuntimeConfig(rt).
		WithCreatorProfileReader(reader)

	in := baseNewLaunchForProfileTest()
	in.CreatorPrevTokenCountKnown = true
	in.CreatorPrevTokenCount = 5 // probe says 5 ≥ 1 → should still REJECT

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error from ProcessForMode: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT via probe path even when reader errors, got Decision=%q reasons=%v",
			out.Decision, out.RejectReasons)
	}
	if !containsString(out.RejectReasons, "serial_launcher") {
		t.Errorf("expected serial_launcher in RejectReasons, got %v", out.RejectReasons)
	}
}

// ── TestWithCreatorProfileReader_NilReader_NoPanic ────────────────────────────

// TestWithCreatorProfileReader_NilReader_NoPanic verifies that WithCreatorProfileReader(nil)
// leaves the module operating on probe-only semantics without panicking.
func TestWithCreatorProfileReader_NilReader_NoPanic(t *testing.T) {
	m := New(DefaultConfig(nil), nil).WithCreatorProfileReader(nil)
	in := baseNewLaunchForProfileTest()
	in.CreatorPrevTokenCountKnown = true
	in.CreatorPrevTokenCount = 0

	out, err := m.ProcessForMode(context.Background(), in, "BALANCED")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = out // no panic is the assertion
}
