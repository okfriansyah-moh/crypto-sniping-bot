package position

import (
	"context"
	"testing"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

func defaultPosCfg() *config.PositionConfig {
	return &config.PositionConfig{
		Tp1Bps:         500,  // +5%
		Tp2Bps:         1000, // +10%
		SlBps:          300,  // -3%
		MaxHoldSeconds: 300,
	}
}

func successfulExecution() contracts.ExecutionResultDTO {
	return contracts.ExecutionResultDTO{
		EventID:            "exec-evt-1",
		TraceID:            "trace-1",
		CorrelationID:      "corr-1",
		VersionID:          "v1",
		TokenLifecycleID:   "lc-1",
		ExecutionID:        "exec-id-1",
		Success:            true,
		RealizedEntryPrice: "1.0",
	}
}

func openPosition() contracts.PositionStateDTO {
	return contracts.PositionStateDTO{
		EventID:          "pos-evt-1",
		TraceID:          "trace-1",
		CorrelationID:    "corr-1",
		VersionID:        "v1",
		TokenLifecycleID: "lc-1",
		PositionID:       "pos-id-1",
		ExecutionID:      "exec-id-1",
		TokenAddress:     "0xTOKEN",
		Chain:            "eth",
		Status:           "open",
		EntryPrice:       "1.0",
		EntrySizeUsd:     100.0,
		CurrentPrice:     "1.0",
		Tp1Bps:           500,
		Tp2Bps:           1000,
		SlBps:            300,
		MaxHoldSeconds:   300,
		OpenedAt:         time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// ── New ──────────────────────────────────────────────────────────────────────

func TestNew_NilConfig_UsesDefaults(t *testing.T) {
	m := New(nil)
	if m == nil {
		t.Fatal("New returned nil")
	}
	if m.cfg.Tp1Bps == 0 {
		t.Error("expected non-zero Tp1Bps default")
	}
}

// ── OpenPosition ─────────────────────────────────────────────────────────────

func TestOpenPosition_Success_StatusOpen(t *testing.T) {
	// Arrange
	m := New(defaultPosCfg())
	in := successfulExecution()

	// Act
	out, err := m.OpenPosition(context.Background(), in, "eth", "0xTOKEN")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != "open" {
		t.Errorf("expected Status=open, got %q", out.Status)
	}
	if out.PositionID == "" {
		t.Error("PositionID must not be empty")
	}
	if out.EntryPrice != "1.0" {
		t.Errorf("EntryPrice not propagated: %q", out.EntryPrice)
	}
}

func TestOpenPosition_Success_TraceFieldsPropagated(t *testing.T) {
	m := New(defaultPosCfg())
	in := successfulExecution()

	out, err := m.OpenPosition(context.Background(), in, "eth", "0xTOKEN")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.TraceID != "trace-1" {
		t.Errorf("TraceID not propagated: %q", out.TraceID)
	}
	if out.Chain != "eth" {
		t.Errorf("Chain not set: %q", out.Chain)
	}
	if out.TokenAddress != "0xTOKEN" {
		t.Errorf("TokenAddress not set: %q", out.TokenAddress)
	}
}

func TestOpenPosition_Success_PositionIDDeterministic(t *testing.T) {
	// Same ExecutionID → same PositionID.
	m := New(defaultPosCfg())
	in := successfulExecution()

	out1, _ := m.OpenPosition(context.Background(), in, "eth", "0xTOKEN")
	out2, _ := m.OpenPosition(context.Background(), in, "eth", "0xTOKEN")

	if out1.PositionID != out2.PositionID {
		t.Errorf("PositionID not deterministic: %q vs %q", out1.PositionID, out2.PositionID)
	}
}

func TestOpenPosition_Failed_StatusFailed(t *testing.T) {
	m := New(defaultPosCfg())
	in := successfulExecution()
	in.Success = false

	out, err := m.OpenPosition(context.Background(), in, "eth", "0xTOKEN")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != "failed" {
		t.Errorf("expected Status=failed for failed execution, got %q", out.Status)
	}
}

func TestOpenPosition_Success_ExitParamsFromConfig(t *testing.T) {
	m := New(defaultPosCfg())
	in := successfulExecution()

	out, err := m.OpenPosition(context.Background(), in, "eth", "0xTOKEN")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Tp1Bps != 500 {
		t.Errorf("expected Tp1Bps=500, got %d", out.Tp1Bps)
	}
	if out.Tp2Bps != 1000 {
		t.Errorf("expected Tp2Bps=1000, got %d", out.Tp2Bps)
	}
	if out.SlBps != 300 {
		t.Errorf("expected SlBps=300, got %d", out.SlBps)
	}
}

// ── PollExit ─────────────────────────────────────────────────────────────────

func TestPollExit_NoExit_StatusOpen(t *testing.T) {
	// Arrange: price unchanged — no exit condition met.
	m := New(defaultPosCfg())
	pos := openPosition()
	evalAt := time.Now().UTC()

	// Act
	out, err := m.PollExit(context.Background(), pos, "1.0", evalAt)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status == "exited" {
		t.Error("expected no exit when price unchanged")
	}
	if out.ExitReason != "" {
		t.Errorf("expected empty ExitReason, got %q", out.ExitReason)
	}
}

func TestPollExit_TP1Triggered(t *testing.T) {
	m := New(defaultPosCfg())
	pos := openPosition()
	evalAt := time.Now().UTC()

	// +5% gain hits TP1 (500 bps = 5%)
	out, err := m.PollExit(context.Background(), pos, "1.05", evalAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ExitReason != "TP1" {
		t.Errorf("expected ExitReason=TP1, got %q", out.ExitReason)
	}
	if out.Status != "exited" {
		t.Errorf("expected Status=exited, got %q", out.Status)
	}
}

func TestPollExit_TP2Triggered(t *testing.T) {
	m := New(defaultPosCfg())
	pos := openPosition()
	evalAt := time.Now().UTC()

	// +10% gain hits TP2 (1000 bps = 10%)
	out, err := m.PollExit(context.Background(), pos, "1.10", evalAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ExitReason != "TP2" {
		t.Errorf("expected ExitReason=TP2, got %q", out.ExitReason)
	}
}

func TestPollExit_StopLossTriggered(t *testing.T) {
	m := New(defaultPosCfg())
	pos := openPosition()
	evalAt := time.Now().UTC()

	// -3% loss hits SL (300 bps = 3%)
	out, err := m.PollExit(context.Background(), pos, "0.97", evalAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ExitReason != "SL" {
		t.Errorf("expected ExitReason=SL, got %q", out.ExitReason)
	}
}

func TestPollExit_TimeExitTriggered(t *testing.T) {
	m := New(defaultPosCfg())
	pos := openPosition()
	// OpenedAt = 600 seconds ago, MaxHoldSeconds = 300 → time expired.
	pos.OpenedAt = time.Now().UTC().Add(-600 * time.Second).Format(time.RFC3339Nano)
	evalAt := time.Now().UTC()

	out, err := m.PollExit(context.Background(), pos, "1.0", evalAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ExitReason != "TIME" {
		t.Errorf("expected ExitReason=TIME, got %q", out.ExitReason)
	}
}

func TestPollExit_InvalidEntryPrice_NoExit(t *testing.T) {
	// Empty entry price should not crash — returns price-only snapshot.
	m := New(defaultPosCfg())
	pos := openPosition()
	pos.EntryPrice = ""
	evalAt := time.Now().UTC()

	out, err := m.PollExit(context.Background(), pos, "1.05", evalAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ExitReason != "" {
		t.Errorf("expected no exit for invalid entry price, got %q", out.ExitReason)
	}
}

func TestPollExit_InvalidCurrentPrice_NoExit(t *testing.T) {
	// Invalid current price should not crash.
	m := New(defaultPosCfg())
	pos := openPosition()
	evalAt := time.Now().UTC()

	out, err := m.PollExit(context.Background(), pos, "not-a-number", evalAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ExitReason != "" {
		t.Errorf("expected no exit for invalid current price, got %q", out.ExitReason)
	}
}

func TestPollExit_Exit_PnlCalculated(t *testing.T) {
	m := New(defaultPosCfg())
	pos := openPosition()
	pos.EntrySizeUsd = 100.0
	evalAt := time.Now().UTC()

	// +10% → TP2; PnlUsd ≈ 10.0
	out, err := m.PollExit(context.Background(), pos, "1.10", evalAt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.PnlUsd != 10.0 {
		t.Errorf("expected PnlUsd=10.0, got %f", out.PnlUsd)
	}
	if out.PnlPct <= 0 {
		t.Errorf("expected positive PnlPct for profitable exit, got %f", out.PnlPct)
	}
}

func TestPollExit_SnapshotEventIDDeterministic(t *testing.T) {
	// Same position + same evalAt → same EventID.
	m := New(defaultPosCfg())
	pos := openPosition()
	evalAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	out1, _ := m.PollExit(context.Background(), pos, "1.0", evalAt)
	out2, _ := m.PollExit(context.Background(), pos, "1.0", evalAt)

	if out1.EventID != out2.EventID {
		t.Errorf("EventID not deterministic: %q vs %q", out1.EventID, out2.EventID)
	}
}
