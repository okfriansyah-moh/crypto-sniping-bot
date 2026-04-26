package modules_test

import (
	"context"
	"testing"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/position"
)

func positionCfg() *config.PositionConfig {
	return &config.PositionConfig{
		Tp1Bps:         500,  // 5%
		Tp2Bps:         1000, // 10%
		SlBps:          300,  // 3%
		MaxHoldSeconds: 60,
	}
}

func executionResultFixture(success bool) contracts.ExecutionResultDTO {
	return contracts.ExecutionResultDTO{
		EventID:            "exec-001",
		TraceID:            "trace-001",
		CorrelationID:      "corr-001",
		VersionID:          "v1",
		TokenLifecycleID:   "lc-001",
		ExecutionID:        "exec-id-001",
		Status:             map[bool]string{true: "confirmed", false: "reverted"}[success],
		Success:            success,
		WalletAddress:      "0xwallet",
		RealizedEntryPrice: "0.001",
	}
}

func openPositionFixture() contracts.PositionStateDTO {
	return contracts.PositionStateDTO{
		EventID:          "pos-001",
		TraceID:          "trace-001",
		CorrelationID:    "corr-001",
		VersionID:        "v1",
		TokenLifecycleID: "lc-001",
		PositionID:       "pos-id-001",
		ExecutionID:      "exec-id-001",
		TokenAddress:     "0xabc123",
		Chain:            "eth",
		Status:           "open",
		EntryPrice:       "1.0",
		EntrySizeUsd:     10.0,
		Tp1Bps:           500,
		Tp2Bps:           1000,
		SlBps:            300,
		MaxHoldSeconds:   60,
		OpenedAt:         time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func TestOpenPosition_SuccessfulExecution(t *testing.T) {
	mod := position.New(positionCfg())
	pos, err := mod.OpenPosition(context.Background(), executionResultFixture(true), "eth", "0xabc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pos.Status != "open" {
		t.Errorf("expected status=open, got %q", pos.Status)
	}
	if pos.PositionID == "" {
		t.Error("PositionID must be set")
	}
	if pos.Tp1Bps != 500 {
		t.Errorf("expected Tp1Bps=500, got %d", pos.Tp1Bps)
	}
}

func TestOpenPosition_FailedExecution(t *testing.T) {
	mod := position.New(positionCfg())
	pos, err := mod.OpenPosition(context.Background(), executionResultFixture(false), "eth", "0xabc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pos.Status != "failed" {
		t.Errorf("expected status=failed for failed execution, got %q", pos.Status)
	}
}

func TestPollExit_TakeProfit1(t *testing.T) {
	mod := position.New(positionCfg())
	pos := openPositionFixture()
	// Entry = 1.0, TP1 = 5% → trigger at 1.05
	updated, err := mod.PollExit(context.Background(), pos, "1.06", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != "exited" {
		t.Errorf("expected exited, got %q", updated.Status)
	}
	if updated.ExitReason != "TP1" {
		t.Errorf("expected TP1 exit, got %q", updated.ExitReason)
	}
	if updated.PnlPct <= 0 {
		t.Errorf("expected positive PnL at TP1, got %v", updated.PnlPct)
	}
}

func TestPollExit_StopLoss(t *testing.T) {
	mod := position.New(positionCfg())
	pos := openPositionFixture()
	// Entry = 1.0, SL = 3% → trigger at <= 0.97
	updated, _ := mod.PollExit(context.Background(), pos, "0.96", time.Now())
	if updated.Status != "exited" {
		t.Errorf("expected exited, got %q", updated.Status)
	}
	if updated.ExitReason != "SL" {
		t.Errorf("expected SL exit, got %q", updated.ExitReason)
	}
	if updated.PnlPct >= 0 {
		t.Errorf("expected negative PnL at SL, got %v", updated.PnlPct)
	}
}

func TestPollExit_TimeExit(t *testing.T) {
	mod := position.New(positionCfg())
	pos := openPositionFixture()
	// Set opened_at in the past beyond MaxHoldSeconds
	pos.OpenedAt = time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339Nano)
	pos.MaxHoldSeconds = 30 // 30s max hold

	updated, _ := mod.PollExit(context.Background(), pos, "1.0", time.Now())
	if updated.Status != "exited" {
		t.Errorf("expected time exit, got status=%q reason=%q", updated.Status, updated.ExitReason)
	}
	if updated.ExitReason != "TIME" {
		t.Errorf("expected TIME exit, got %q", updated.ExitReason)
	}
}

func TestPollExit_NoExitInRange(t *testing.T) {
	mod := position.New(positionCfg())
	pos := openPositionFixture()
	// Price within TP/SL band
	updated, _ := mod.PollExit(context.Background(), pos, "1.01", time.Now())
	if updated.Status == "exited" {
		t.Errorf("should not exit in range, got status=%q reason=%q", updated.Status, updated.ExitReason)
	}
	if updated.CurrentPrice != "1.01" {
		t.Errorf("CurrentPrice not updated: %q", updated.CurrentPrice)
	}
}

func TestPollExit_TakeProfit2(t *testing.T) {
	mod := position.New(positionCfg())
	pos := openPositionFixture()
	// Entry = 1.0, TP2 = 10% → trigger at >= 1.10; must NOT fire TP1 instead.
	updated, err := mod.PollExit(context.Background(), pos, "1.12", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != "exited" {
		t.Errorf("expected exited, got %q", updated.Status)
	}
	if updated.ExitReason != "TP2" {
		t.Errorf("expected TP2 exit, got %q (TP2 must be checked before TP1)", updated.ExitReason)
	}
	if updated.PnlPct <= 0 {
		t.Errorf("expected positive PnL at TP2, got %v", updated.PnlPct)
	}
}
