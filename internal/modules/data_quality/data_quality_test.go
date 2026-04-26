package data_quality

import (
	"context"
	"testing"

	"crypto-sniping-bot/contracts"
)

func defaultDQConfig() Config {
	return Config{
		MaxBuyTaxBps:      1000,
		MaxSellTaxBps:     1500,
		MinLPHolderCount:  1,
		MinReserveBaseWei: "1000000000000000",
	}
}

func validMarketData() contracts.MarketDataDTO {
	return contracts.MarketDataDTO{
		EventID:        "mkt-1",
		TraceID:        "trace-1",
		CorrelationID:  "corr-1",
		VersionID:      "v1",
		TokenAddress:   "0xTOKEN",
		Chain:          "eth",
		ReserveBaseRaw: "2000000000000000", // above minimum
		Reorged:        false,
	}
}

// ── New ──────────────────────────────────────────────────────────────────────

func TestNew_NilLogger_UsesDefault(t *testing.T) {
	m := New(defaultDQConfig(), nil)
	if m == nil {
		t.Fatal("New returned nil module")
	}
}

// ── Process happy path ────────────────────────────────────────────────────────

func TestProcess_ValidInput_DecisionPASS(t *testing.T) {
	// Arrange
	m := New(defaultDQConfig(), nil)
	in := validMarketData()

	// Act
	out, err := m.Process(context.Background(), in)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "PASS" {
		t.Errorf("expected Decision=PASS, got %q", out.Decision)
	}
	if len(out.RejectReasons) != 0 {
		t.Errorf("expected no reject reasons, got %v", out.RejectReasons)
	}
}

func TestProcess_ValidInput_TraceFieldsPropagated(t *testing.T) {
	m := New(defaultDQConfig(), nil)
	in := validMarketData()

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.TraceID != "trace-1" {
		t.Errorf("TraceID not propagated: %q", out.TraceID)
	}
	if out.CausationID != in.EventID {
		t.Errorf("CausationID should equal upstream EventID: got %q", out.CausationID)
	}
	if out.TokenAddress != "0xTOKEN" {
		t.Errorf("TokenAddress not propagated: %q", out.TokenAddress)
	}
}

func TestProcess_ValidInput_EventIDDeterministic(t *testing.T) {
	m := New(defaultDQConfig(), nil)
	in := validMarketData()

	out1, _ := m.Process(context.Background(), in)
	out2, _ := m.Process(context.Background(), in)

	if out1.EventID != out2.EventID {
		t.Errorf("EventID not deterministic: %q vs %q", out1.EventID, out2.EventID)
	}
}

// ── Process rejection paths ───────────────────────────────────────────────────

func TestProcess_MissingReserve_DecisionREJECT(t *testing.T) {
	m := New(defaultDQConfig(), nil)
	in := validMarketData()
	in.ReserveBaseRaw = ""

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected Decision=REJECT for missing reserve, got %q", out.Decision)
	}
	if !contains(out.RejectReasons, "missing_reserves") {
		t.Errorf("expected 'missing_reserves' in RejectReasons: %v", out.RejectReasons)
	}
}

func TestProcess_ZeroReserve_DecisionREJECT(t *testing.T) {
	m := New(defaultDQConfig(), nil)
	in := validMarketData()
	in.ReserveBaseRaw = "0"

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected Decision=REJECT for zero reserve, got %q", out.Decision)
	}
}

func TestProcess_InsufficientReserve_FakeLiquiditySet(t *testing.T) {
	m := New(defaultDQConfig(), nil)
	in := validMarketData()
	in.ReserveBaseRaw = "100" // well below 1000000000000000

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.IsFakeLiquidity {
		t.Error("expected IsFakeLiquidity=true for insufficient reserve")
	}
	if !contains(out.RejectReasons, "insufficient_liquidity") {
		t.Errorf("expected 'insufficient_liquidity' in RejectReasons: %v", out.RejectReasons)
	}
}

func TestProcess_ReorgedEvent_DecisionREJECT(t *testing.T) {
	m := New(defaultDQConfig(), nil)
	in := validMarketData()
	in.Reorged = true

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected Decision=REJECT for reorged event, got %q", out.Decision)
	}
	if !contains(out.RejectReasons, "reorged_event") {
		t.Errorf("expected 'reorged_event' in RejectReasons: %v", out.RejectReasons)
	}
}

func TestProcess_MissingTokenAddress_DecisionREJECT(t *testing.T) {
	m := New(defaultDQConfig(), nil)
	in := validMarketData()
	in.TokenAddress = ""

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected Decision=REJECT for missing token address, got %q", out.Decision)
	}
	if !contains(out.RejectReasons, "missing_token_address") {
		t.Errorf("expected 'missing_token_address' in RejectReasons: %v", out.RejectReasons)
	}
}

func TestProcess_RejectReasonsSorted(t *testing.T) {
	// Multiple failures — reasons must be sorted for determinism.
	m := New(defaultDQConfig(), nil)
	in := validMarketData()
	in.TokenAddress = ""
	in.Reorged = true

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 1; i < len(out.RejectReasons); i++ {
		if out.RejectReasons[i-1] > out.RejectReasons[i] {
			t.Errorf("RejectReasons not sorted: %v", out.RejectReasons)
		}
	}
}

func TestProcess_RiskScoreClamped(t *testing.T) {
	m := New(defaultDQConfig(), nil)
	in := validMarketData()
	// All 4 checks fail.
	in.TokenAddress = ""
	in.ReserveBaseRaw = "0"
	in.Reorged = true

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.RiskScore < 0 || out.RiskScore > 1 {
		t.Errorf("RiskScore out of [0,1]: %f", out.RiskScore)
	}
}

func TestProcess_TokenLifecycleIDSet(t *testing.T) {
	m := New(defaultDQConfig(), nil)
	in := validMarketData()

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.TokenLifecycleID == "" {
		t.Error("TokenLifecycleID must not be empty")
	}
}

// helpers

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
