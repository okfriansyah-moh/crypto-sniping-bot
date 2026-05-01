package features

import (
	"context"
	"testing"

	"crypto-sniping-bot/contracts"
)

func passedDQ() contracts.DataQualityDTO {
	return contracts.DataQualityDTO{
		EventID:          "dq-1",
		TraceID:          "trace-1",
		CorrelationID:    "corr-1",
		VersionID:        "v1",
		TokenLifecycleID: "lc-1",
		TokenAddress:     "0xTOKEN",
		Decision:         "PASS",
		RiskScore:        0.0,
		IsHoneypot:       false,
		IsRugRisk:        false,
		IsFakeLiquidity:  false,
		ContractVerified: true,
		LpHolderCount:    3,
	}
}

// ── New ──────────────────────────────────────────────────────────────────────

func TestNew_ReturnsModule(t *testing.T) {
	m := New(nil)
	if m == nil {
		t.Fatal("New returned nil")
	}
}

// ── Process happy path ────────────────────────────────────────────────────────

func TestProcess_PassedDQ_EmitsFeatureDTO(t *testing.T) {
	// Arrange
	m := New(nil)
	in := passedDQ()

	// Act
	out, err := m.Process(context.Background(), in)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.TokenAddress != "0xTOKEN" {
		t.Errorf("TokenAddress not propagated: %q", out.TokenAddress)
	}
	if out.LiquidityScore < 0 || out.LiquidityScore > 1 {
		t.Errorf("LiquidityScore out of [0,1]: %f", out.LiquidityScore)
	}
}

func TestProcess_PassedDQ_TraceFieldsPropagated(t *testing.T) {
	m := New(nil)
	in := passedDQ()

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.TraceID != "trace-1" {
		t.Errorf("TraceID not propagated: %q", out.TraceID)
	}
	if out.CorrelationID != "corr-1" {
		t.Errorf("CorrelationID not propagated: %q", out.CorrelationID)
	}
	if out.CausationID != in.EventID {
		t.Errorf("CausationID should equal upstream EventID, got %q", out.CausationID)
	}
}

func TestProcess_HigherLiquidityUsd_HigherLiquidityScore(t *testing.T) {
	m := New(nil)
	in := passedDQ()

	low := MarketSnapshot{Market: "eth-uniswap-v2", LpStatsKnown: true, LiquidityUsd: 1_000}
	high := MarketSnapshot{Market: "eth-uniswap-v2", LpStatsKnown: true, LiquidityUsd: 1_000_000}

	outLow, err := m.ProcessWithContext(context.Background(), in, low, BaselineSnapshot{}, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	outHigh, err := m.ProcessWithContext(context.Background(), in, high, BaselineSnapshot{}, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outHigh.LiquidityScore <= outLow.LiquidityScore {
		t.Errorf("expected LiquidityScore to grow with LiquidityUsd: low=%f high=%f",
			outLow.LiquidityScore, outHigh.LiquidityScore)
	}
	if outLow.LiquidityUsdRaw != 1_000 || outHigh.LiquidityUsdRaw != 1_000_000 {
		t.Errorf("raw liquidity not propagated: low=%f high=%f", outLow.LiquidityUsdRaw, outHigh.LiquidityUsdRaw)
	}
}

func TestProcess_NoMarketSnapshot_LiquidityConfidenceLow(t *testing.T) {
	m := New(nil)
	in := passedDQ()

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Confidence.LiquidityScore >= 0.4 {
		t.Errorf("expected low liquidity confidence with no market snapshot, got %f", out.Confidence.LiquidityScore)
	}
}

func TestProcess_EventIDDeterministic(t *testing.T) {
	m := New(nil)
	in := passedDQ()

	out1, _ := m.Process(context.Background(), in)
	out2, _ := m.Process(context.Background(), in)

	if out1.EventID != out2.EventID {
		t.Errorf("EventID not deterministic: %q vs %q", out1.EventID, out2.EventID)
	}
}

// ── Contract safety score ─────────────────────────────────────────────────────

func TestProcess_HoneypotFlag_ReducesContractSafety(t *testing.T) {
	m := New(nil)
	base := passedDQ()
	base.ContractVerified = true // start at 1.0

	honey := passedDQ()
	honey.IsHoneypot = true
	honey.ContractVerified = true

	outBase, _ := m.Process(context.Background(), base)
	outHoney, _ := m.Process(context.Background(), honey)

	if outHoney.ContractSafety >= outBase.ContractSafety {
		t.Errorf("honeypot should reduce ContractSafety: base=%f honey=%f",
			outBase.ContractSafety, outHoney.ContractSafety)
	}
}

func TestProcess_AllFlagsSet_ContractSafetyFlooredAtZero(t *testing.T) {
	m := New(nil)
	in := passedDQ()
	in.IsHoneypot = true
	in.IsRugRisk = true
	in.IsFakeLiquidity = true
	in.ContractVerified = false

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ContractSafety < 0 {
		t.Errorf("ContractSafety must not go below 0, got %f", out.ContractSafety)
	}
}

func TestProcess_VerifiedContract_HigherSafety(t *testing.T) {
	m := New(nil)
	verified := passedDQ()
	verified.ContractVerified = true

	unverified := passedDQ()
	unverified.EventID = "dq-2"
	unverified.ContractVerified = false

	outV, _ := m.Process(context.Background(), verified)
	outU, _ := m.Process(context.Background(), unverified)

	if outV.ContractSafety <= outU.ContractSafety {
		t.Errorf("verified contract should have higher ContractSafety: verified=%f unverified=%f",
			outV.ContractSafety, outU.ContractSafety)
	}
}

func TestProcess_HolderCountPropagated(t *testing.T) {
	m := New(nil)
	in := passedDQ()
	in.LpHolderCount = 7

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.HolderCountRaw != 7 {
		t.Errorf("expected HolderCountRaw=7, got %d", out.HolderCountRaw)
	}
}

// ── clamp helper ─────────────────────────────────────────────────────────────

func TestClamp_WithinBounds(t *testing.T) {
	if clamp(0.5, 0, 1) != 0.5 {
		t.Error("expected 0.5")
	}
}

func TestClamp_BelowLo(t *testing.T) {
	if clamp(-0.1, 0, 1) != 0 {
		t.Error("expected 0")
	}
}

func TestClamp_AboveHi(t *testing.T) {
	if clamp(1.5, 0, 1) != 1.0 {
		t.Error("expected 1.0")
	}
}
