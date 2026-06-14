package execution_quality

import (
	"math"
	"testing"
	"time"
)

func cfgT() AlphaAggregatorConfig {
	return AlphaAggregatorConfig{
		MinSampleCount:    5,
		AlphaMin:          0.5,
		AlphaMax:          2.0,
		EwmaHalflifeSec:   3600,
		UpdateIntervalSec: 300,
	}
}

func TestComputeMarketAlpha_NoSamples_ReturnsOne(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	a, ep, er, n := ComputeMarketAlpha(nil, cfgT(), now)
	if a != 1.0 || n != 0 || ep != 0 || er != 0 {
		t.Fatalf("want 1.0/0/0/0, got %v/%v/%v/%v", a, ep, er, n)
	}
}

func TestComputeMarketAlpha_RealizedHigherThanPredicted_AlphaAboveOne(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	samples := []FillSample{
		{PredictedBps: 100, RealizedBps: 150, At: now.Add(-60 * time.Second)},
		{PredictedBps: 100, RealizedBps: 140, At: now.Add(-120 * time.Second)},
		{PredictedBps: 100, RealizedBps: 160, At: now.Add(-30 * time.Second)},
	}
	a, _, _, n := ComputeMarketAlpha(samples, cfgT(), now)
	if n != 3 {
		t.Fatalf("samples=3, got %d", n)
	}
	if a <= 1.0 {
		t.Fatalf("expected α > 1, got %v", a)
	}
	if a > 2.0 {
		t.Fatalf("expected α ≤ 2, got %v", a)
	}
}

func TestComputeMarketAlpha_RealizedLowerThanPredicted_AlphaBelowOne(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	samples := []FillSample{
		{PredictedBps: 100, RealizedBps: 60, At: now.Add(-60 * time.Second)},
		{PredictedBps: 100, RealizedBps: 70, At: now.Add(-120 * time.Second)},
	}
	a, _, _, _ := ComputeMarketAlpha(samples, cfgT(), now)
	if a >= 1.0 {
		t.Fatalf("expected α < 1, got %v", a)
	}
	if a < 0.5 {
		t.Fatalf("expected α ≥ AlphaMin=0.5, got %v", a)
	}
}

func TestComputeMarketAlpha_Clamps_HighRealized(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	samples := []FillSample{
		{PredictedBps: 10, RealizedBps: 1000, At: now},
		{PredictedBps: 10, RealizedBps: 1000, At: now.Add(-1 * time.Second)},
	}
	a, _, _, _ := ComputeMarketAlpha(samples, cfgT(), now)
	if a != 2.0 {
		t.Fatalf("expected α clamped to AlphaMax=2.0, got %v", a)
	}
}

func TestComputeMarketAlpha_Clamps_LowRealized(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	samples := []FillSample{
		{PredictedBps: 1000, RealizedBps: 1, At: now},
	}
	a, _, _, _ := ComputeMarketAlpha(samples, cfgT(), now)
	if a != 0.5 {
		t.Fatalf("expected α clamped to AlphaMin=0.5, got %v", a)
	}
}

func TestComputeMarketAlpha_EwmaWeightsRecentMore(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	cfg := cfgT()
	cfg.EwmaHalflifeSec = 60 // aggressive decay so old sample loses influence

	// Old sample: realized < predicted (would push α down)
	// Recent sample: realized > predicted (should win)
	samples := []FillSample{
		{PredictedBps: 100, RealizedBps: 50, At: now.Add(-3600 * time.Second)},
		{PredictedBps: 100, RealizedBps: 150, At: now},
	}
	a, _, _, _ := ComputeMarketAlpha(samples, cfg, now)
	if a <= 1.0 {
		t.Fatalf("recent sample should dominate (α > 1), got %v", a)
	}
	if math.Abs(a-1.5) > 0.05 {
		t.Fatalf("expected α ≈ 1.5 (recent dominates), got %v", a)
	}
}

func TestComputeMarketAlpha_FiltersNonFiniteAndNonPositive(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	samples := []FillSample{
		{PredictedBps: math.NaN(), RealizedBps: 100, At: now},
		{PredictedBps: 100, RealizedBps: math.Inf(1), At: now},
		{PredictedBps: 0, RealizedBps: 100, At: now},
		{PredictedBps: 100, RealizedBps: -10, At: now},
		{PredictedBps: 100, RealizedBps: 120, At: now},
	}
	_, _, _, n := ComputeMarketAlpha(samples, cfgT(), now)
	if n != 1 {
		t.Fatalf("expected only 1 valid sample counted, got %d", n)
	}
}
