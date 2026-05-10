package edge_test

import (
	"testing"

	"crypto-sniping-bot/internal/modules/edge"
)

// helpers

func slots(prices ...float64) []edge.PriceSlot {
	s := make([]edge.PriceSlot, len(prices))
	for i, p := range prices {
		s[i] = edge.PriceSlot{PriceUsd: p, SlotIndex: i}
	}
	return s
}

// TestAnalyzeBottom_InsufficientSlots expects score=0 when < 3 slots.
func TestAnalyzeBottom_InsufficientSlots(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prices []float64
	}{
		{"nil", nil},
		{"empty", []float64{}},
		{"one slot", []float64{1.0}},
		{"two slots", []float64{1.0, 0.9}},
	} {
		sig := edge.AnalyzeBottom(slots(tc.prices...), 20)
		if sig.BottomDetectionScore != 0 {
			t.Errorf("[%s] expected score=0, got %f", tc.name, sig.BottomDetectionScore)
		}
	}
}

// TestAnalyzeBottom_StillDescending_ZeroScore: trough at the last slot.
func TestAnalyzeBottom_StillDescending_ZeroScore(t *testing.T) {
	// Monotonically decreasing — trough = last slot.
	sig := edge.AnalyzeBottom(slots(1.0, 0.9, 0.8, 0.7, 0.6), 20)
	if sig.BottomDetectionScore != 0 {
		t.Errorf("still descending: expected score=0, got %f", sig.BottomDetectionScore)
	}
}

// TestAnalyzeBottom_PerfectVShape: deep trough in the middle, full recovery.
func TestAnalyzeBottom_PerfectVShape(t *testing.T) {
	// 0→0.5→0→0.5→1.0 expressed as prices 1.0 → 0.5 → 0.01 → 0.5 → 1.0
	sig := edge.AnalyzeBottom(slots(1.0, 0.5, 0.1, 0.5, 1.0), 20)
	if sig.BottomDetectionScore <= 0.7 {
		t.Errorf("perfect V-shape: expected score > 0.7, got %f", sig.BottomDetectionScore)
	}
	if sig.TroughDepthBps <= 0 {
		t.Errorf("trough depth should be > 0, got %d", sig.TroughDepthBps)
	}
	if sig.RecoveryBps <= 0 {
		t.Errorf("recovery bps should be > 0, got %d", sig.RecoveryBps)
	}
}

// TestAnalyzeBottom_NoRecovery_ZeroScore: trough found but no price increase.
func TestAnalyzeBottom_NoRecovery_ZeroScore(t *testing.T) {
	// Descend and then plateau at trough level.
	sig := edge.AnalyzeBottom(slots(1.0, 0.8, 0.6, 0.6, 0.6), 20)
	if sig.BottomDetectionScore != 0 {
		t.Errorf("no recovery: expected score=0, got %f", sig.BottomDetectionScore)
	}
}

// TestAnalyzeBottom_SmallRecovery_LowScore.
func TestAnalyzeBottom_SmallRecovery_LowScore(t *testing.T) {
	// Falls 20%, recovers only 1% — small recovery → score < 0.5.
	sig := edge.AnalyzeBottom(slots(1.0, 0.9, 0.8, 0.808), 20)
	if sig.BottomDetectionScore >= 0.5 {
		t.Errorf("small recovery: expected score < 0.5, got %f", sig.BottomDetectionScore)
	}
}

// TestAnalyzeBottom_WindowTrimming: only the last maxSlots are used.
func TestAnalyzeBottom_WindowTrimming(t *testing.T) {
	// 25 slots; the first 5 are a noisy climb; the next 20 form a V-shape.
	prices := make([]float64, 25)
	// first 5: climbing noise
	for i := 0; i < 5; i++ {
		prices[i] = 0.5 + float64(i)*0.1
	}
	// remaining 20: V-shape
	prices[5] = 2.0
	prices[6] = 1.5
	prices[7] = 1.0
	prices[8] = 0.5 // trough
	prices[9] = 0.8
	prices[10] = 1.2
	for i := 11; i < 25; i++ {
		prices[i] = 1.5
	}

	sig := edge.AnalyzeBottom(slots(prices...), 20)
	// maxSlots=20 trims the first 5 — only the V-shape portion is scored.
	if sig.SlotsAnalyzed != 20 {
		t.Errorf("expected 20 slots analysed, got %d", sig.SlotsAnalyzed)
	}
	if sig.BottomDetectionScore <= 0 {
		t.Errorf("V-shape within window: expected score > 0, got %f", sig.BottomDetectionScore)
	}
}

// TestAnalyzeBottom_DefaultMaxSlots: maxSlots=0 uses the 20-slot default.
func TestAnalyzeBottom_DefaultMaxSlots(t *testing.T) {
	// 30 slots — all flat at 1.0 except a V at positions 25-29.
	prices := make([]float64, 30)
	for i := range prices {
		prices[i] = 1.0
	}
	prices[25] = 0.8
	prices[26] = 0.6 // trough
	prices[27] = 0.8
	prices[28] = 1.0
	prices[29] = 1.2

	sig := edge.AnalyzeBottom(slots(prices...), 0) // maxSlots=0 → default 20
	if sig.SlotsAnalyzed != 20 {
		t.Errorf("expected 20 slots with default window, got %d", sig.SlotsAnalyzed)
	}
}

// TestAnalyzeBottom_Deterministic: same input always yields same output.
func TestAnalyzeBottom_Deterministic(t *testing.T) {
	input := slots(1.0, 0.8, 0.6, 0.4, 0.6, 0.8, 1.0)
	a := edge.AnalyzeBottom(input, 20)
	b := edge.AnalyzeBottom(input, 20)
	if a.BottomDetectionScore != b.BottomDetectionScore {
		t.Errorf("non-deterministic: %f vs %f", a.BottomDetectionScore, b.BottomDetectionScore)
	}
}

// TestAnalyzeBottom_ScoreInRange: score must always be in [0, 1].
func TestAnalyzeBottom_ScoreInRange(t *testing.T) {
	inputs := [][]float64{
		{1.0, 0.5, 0.1, 0.5, 1.0},
		{0.1, 0.05, 0.01, 0.1, 0.5, 1.0},
		{1.0, 1.1, 1.2, 1.3}, // only ascending — trough at start
		{1.0, 0.9, 0.8, 0.7, 0.8, 0.9},
	}
	for _, prices := range inputs {
		sig := edge.AnalyzeBottom(slots(prices...), 20)
		if sig.BottomDetectionScore < 0 || sig.BottomDetectionScore > 1 {
			t.Errorf("score %f out of [0,1] for input %v", sig.BottomDetectionScore, prices)
		}
	}
}
