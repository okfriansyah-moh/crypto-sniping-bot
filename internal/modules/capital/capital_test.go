package capital

import (
	"context"
	"testing"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

func defaultCapitalCfg() *config.CapitalConfig {
	return &config.CapitalConfig{
		FixedEntrySizeUsd: 10.0,
		MaxSizeUsd:        100.0,
		TTLSeconds:        3,
		WalletAddress:     "0xWALLET",
	}
}

func selectedInput() contracts.SelectionOutputDTO {
	return contracts.SelectionOutputDTO{
		EventID:          "sel-evt-1",
		TraceID:          "trace-1",
		CorrelationID:    "corr-1",
		VersionID:        "v1",
		TokenLifecycleID: "lc-1",
		TokenAddress:     "0xTOKEN",
		Selected:         true,
		RejectReason:     "",
	}
}

// ── New ──────────────────────────────────────────────────────────────────────

func TestNew_NilConfig_UsesDefaults(t *testing.T) {
	m := New(nil)
	if m == nil {
		t.Fatal("New returned nil")
	}
	if m.cfg.FixedEntrySizeUsd == 0 {
		t.Error("expected non-zero FixedEntrySizeUsd default")
	}
}

func TestNew_CustomConfig_Stored(t *testing.T) {
	cfg := defaultCapitalCfg()
	m := New(cfg)
	if m.cfg.WalletAddress != "0xWALLET" {
		t.Errorf("expected wallet address, got %q", m.cfg.WalletAddress)
	}
}

// ── Process happy path ────────────────────────────────────────────────────────

func TestProcess_Selected_EmitsAllocation(t *testing.T) {
	// Arrange
	m := New(defaultCapitalCfg())
	in := selectedInput()

	// Act
	out, err := m.Process(context.Background(), in, "eth")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Rejected {
		t.Error("expected Rejected=false for selected trade")
	}
	if out.SizeUsd != 10.0 {
		t.Errorf("expected SizeUsd=10.0, got %f", out.SizeUsd)
	}
	if out.ExecutionID == "" {
		t.Error("ExecutionID must not be empty for selected trade")
	}
	if out.TokenAddress != "0xTOKEN" {
		t.Errorf("TokenAddress not propagated: %q", out.TokenAddress)
	}
	if out.Chain != "eth" {
		t.Errorf("Chain not set: %q", out.Chain)
	}
}

func TestProcess_Selected_TraceFieldsPropagated(t *testing.T) {
	m := New(defaultCapitalCfg())
	in := selectedInput()

	out, err := m.Process(context.Background(), in, "eth")
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
		t.Errorf("CausationID should equal upstream EventID: got %q", out.CausationID)
	}
}

func TestProcess_Selected_ContentAddressableExecutionID(t *testing.T) {
	// Same inputs must produce the same ExecutionID (idempotency).
	m := New(defaultCapitalCfg())
	in := selectedInput()

	out1, _ := m.Process(context.Background(), in, "eth")
	out2, _ := m.Process(context.Background(), in, "eth")

	if out1.ExecutionID != out2.ExecutionID {
		t.Errorf("ExecutionID not deterministic: %q vs %q", out1.ExecutionID, out2.ExecutionID)
	}
}

func TestProcess_Selected_SizeCappedByMaxSizeUsd(t *testing.T) {
	// FixedEntrySizeUsd > MaxSizeUsd → output capped at MaxSizeUsd.
	cfg := &config.CapitalConfig{
		FixedEntrySizeUsd: 200.0,
		MaxSizeUsd:        50.0,
		TTLSeconds:        3,
	}
	m := New(cfg)
	in := selectedInput()

	out, err := m.Process(context.Background(), in, "eth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.SizeUsd != 50.0 {
		t.Errorf("expected SizeUsd capped to 50.0, got %f", out.SizeUsd)
	}
}

// ── Process rejection path ────────────────────────────────────────────────────

func TestProcess_NotSelected_EmitsRejectedAllocation(t *testing.T) {
	m := New(defaultCapitalCfg())
	in := selectedInput()
	in.Selected = false
	in.RejectReason = "max_open_positions_reached:1"

	out, err := m.Process(context.Background(), in, "eth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Rejected {
		t.Error("expected Rejected=true for unselected trade")
	}
	if out.SizeUsd != 0 {
		t.Errorf("expected SizeUsd=0 for rejected trade, got %f", out.SizeUsd)
	}
	if out.ExecutionID != "" {
		t.Errorf("ExecutionID should be empty for rejected trade, got %q", out.ExecutionID)
	}
	if out.RejectReason != "max_open_positions_reached:1" {
		t.Errorf("RejectReason not propagated: %q", out.RejectReason)
	}
}

func TestProcess_NotSelected_EventIDDeterministic(t *testing.T) {
	m := New(defaultCapitalCfg())
	in := selectedInput()
	in.Selected = false

	out1, _ := m.Process(context.Background(), in, "eth")
	out2, _ := m.Process(context.Background(), in, "eth")

	if out1.EventID != out2.EventID {
		t.Errorf("EventID not deterministic for rejection: %q vs %q", out1.EventID, out2.EventID)
	}
}

func TestProcess_Selected_MaxSlippageBpsSet(t *testing.T) {
	m := New(defaultCapitalCfg())
	in := selectedInput()

	out, err := m.Process(context.Background(), in, "eth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.MaxSlippageBps != 200 {
		t.Errorf("expected MaxSlippageBps=200, got %d", out.MaxSlippageBps)
	}
}

func TestProcess_Selected_ExpiresAtSet(t *testing.T) {
	m := New(defaultCapitalCfg())
	in := selectedInput()

	out, err := m.Process(context.Background(), in, "eth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ExpiresAt == "" {
		t.Error("ExpiresAt must not be empty for selected allocation")
	}
	if out.AllocatedAt == "" {
		t.Error("AllocatedAt must not be empty")
	}
}
