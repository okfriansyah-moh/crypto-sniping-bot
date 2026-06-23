package position

import (
	"context"
	"testing"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

func basePos(t *testing.T) contracts.PositionStateDTO {
	t.Helper()
	return contracts.PositionStateDTO{
		EventID:        "ev-base",
		PositionID:     "pos-1",
		Status:         "open",
		EntryPrice:     "1.0",
		EntrySizeUsd:   100.0,
		Tp1Bps:         500,  // +5%
		Tp2Bps:         1500, // +15%
		SlBps:          300,  // -3%
		MaxHoldSeconds: 300,
		OpenedAt:       "2026-01-01T00:00:00Z",
	}
}

// TestPollExit_PartialTp1AndPeak verifies that when cfg.Tp1FilledPctBps>0,
// hitting TP1 emits a partial-fill open snapshot (status stays "open"),
// activates trailing, and seeds the peak price.
func TestPollExit_PartialTp1AndPeak(t *testing.T) {
	cfg := &config.PositionConfig{
		Tp1FilledPctBps:       5000, // sell 50% at TP1
		TrailingStopBps:       1000, // 10% trail
		TrailingActivateAtTp1: true,
	}
	m := New(cfg)

	pos := basePos(t)
	evalAt := time.Date(2026, 1, 1, 0, 0, 30, 0, time.UTC)

	// Price hits TP1 (+5% = 1.05).
	out, err := m.PollExitWithVolume(context.Background(), pos, "1.05", 0, evalAt)
	if err != nil {
		t.Fatalf("PollExit: %v", err)
	}
	if out.Status != "open" {
		t.Errorf("Status = %q, want open (partial TP1, not exited)", out.Status)
	}
	if out.Tp1FilledPctBps != 5000 {
		t.Errorf("Tp1FilledPctBps = %d, want 5000", out.Tp1FilledPctBps)
	}
	if out.TrailingStopBps != 1000 {
		t.Errorf("TrailingStopBps = %d, want 1000 (activated at TP1)", out.TrailingStopBps)
	}
	if out.PeakPrice != "1.05" {
		t.Errorf("PeakPrice = %q, want 1.05", out.PeakPrice)
	}
	if out.ExitReason != "" {
		t.Errorf("ExitReason = %q, want empty (no exit at partial TP1)", out.ExitReason)
	}
}

// TestPollExit_TrailingStopAfterTp1 verifies trailing-stop fires when price
// retraces from peak by TrailingStopBps after a partial TP1 has been taken.
func TestPollExit_TrailingStopAfterTp1(t *testing.T) {
	cfg := &config.PositionConfig{
		Tp1FilledPctBps:       5000,
		TrailingStopBps:       1000, // 10%
		TrailingActivateAtTp1: true,
	}
	m := New(cfg)

	pos := basePos(t)
	pos.Tp1FilledPctBps = 5000 // partial already taken
	pos.TrailingStopBps = 1000 // trailing already active
	pos.PeakPrice = "1.20"     // peak observed at +20%

	evalAt := time.Date(2026, 1, 1, 0, 1, 0, 0, time.UTC)

	// Current price 1.07 = peak * (1 - 0.10) = 1.08 floor; 1.07 < 1.08 → trailing fires.
	out, err := m.PollExitWithVolume(context.Background(), pos, "1.07", 0, evalAt)
	if err != nil {
		t.Fatalf("PollExit: %v", err)
	}
	if out.ExitReason != "TRAILING" {
		t.Errorf("ExitReason = %q, want TRAILING", out.ExitReason)
	}
	if out.Status != "exited" {
		t.Errorf("Status = %q, want exited", out.Status)
	}
}

// TestPollExit_TrailingNotActiveBeforeTp1 verifies trailing stop does NOT
// fire when TP1 has not yet been taken (Tp1FilledPctBps == 0).
func TestPollExit_TrailingNotActiveBeforeTp1(t *testing.T) {
	cfg := &config.PositionConfig{}
	m := New(cfg)

	pos := basePos(t)
	pos.PeakPrice = "1.04" // peak observed below TP1 (+5%)
	// Tp1FilledPctBps = 0 — trailing must NOT fire.

	evalAt := time.Date(2026, 1, 1, 0, 1, 0, 0, time.UTC)

	out, err := m.PollExitWithVolume(context.Background(), pos, "1.02", 0, evalAt)
	if err != nil {
		t.Fatalf("PollExit: %v", err)
	}
	if out.ExitReason == "TRAILING" {
		t.Error("trailing fired before TP1 was taken; want no exit")
	}
}

// TestPollExit_VolumeStalenessTimeExit verifies Task E: when volume between
// successive samples is below the threshold AND staleness window elapsed,
// the position exits with TIME_VOLUME_STALE.
func TestPollExit_VolumeStalenessTimeExit(t *testing.T) {
	cfg := &config.PositionConfig{
		VolumeStalenessSeconds:        60,  // 1 minute window
		VolumeStalenessMinDeltaPctBps: 100, // require ≥1% growth
	}
	m := New(cfg)

	pos := basePos(t)
	pos.LastVolumeUsd = 10000.0 // prior sample
	// Prior sample taken 90s before evalAt — covers the full
	// VolumeStalenessSeconds window so the staleness gate can fire.
	pos.LastVolumeCheckAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).
		Format(time.RFC3339Nano)

	// Evaluate 90s after open; current volume essentially flat (< 1%).
	evalAt := time.Date(2026, 1, 1, 0, 1, 30, 0, time.UTC)

	out, err := m.PollExitWithVolume(context.Background(), pos, "1.01", 10010.0, evalAt)
	if err != nil {
		t.Fatalf("PollExit: %v", err)
	}
	if out.ExitReason != "TIME_VOLUME_STALE" {
		t.Errorf("ExitReason = %q, want TIME_VOLUME_STALE", out.ExitReason)
	}
}

// TestPollExit_PeakIsMonotonic verifies the peak price never decreases
// across successive PollExit calls (skill monitoring-loop-engine).
func TestPollExit_PeakIsMonotonic(t *testing.T) {
	m := New(&config.PositionConfig{})
	pos := basePos(t)
	evalAt := time.Date(2026, 1, 1, 0, 0, 10, 0, time.UTC)

	// First poll at 1.04 — peak becomes 1.04.
	out1, _ := m.PollExitWithVolume(context.Background(), pos, "1.04", 0, evalAt)
	if out1.PeakPrice != "1.04" {
		t.Fatalf("peak after first poll = %q, want 1.04", out1.PeakPrice)
	}
	// Second poll at lower price 1.02 — peak must remain 1.04.
	out2, _ := m.PollExitWithVolume(context.Background(), out1, "1.02", 0, evalAt.Add(5*time.Second))
	if out2.PeakPrice != "1.04" {
		t.Errorf("peak regressed: %q (want 1.04)", out2.PeakPrice)
	}
}
