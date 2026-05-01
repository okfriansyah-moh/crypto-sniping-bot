package features

import (
	"context"
	"math"
	"testing"
)

func TestComputeDirectionalConsistency_MonotonicSeriesIsFullyConsistent(t *testing.T) {
	values := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	got := ComputeDirectionalConsistency(values)
	if got.Consistency != 1.0 || got.DominantDirection != "up" {
		t.Errorf("expected fully-consistent up, got %+v", got)
	}
}

func TestComputeDirectionalConsistency_OscillatingSeriesIsLowConsistency(t *testing.T) {
	// Strict alternation produces a consistency near 0.5 (off by 1/n for odd-length series).
	values := []float64{1, 2, 1, 2, 1, 2, 1, 2, 1, 2}
	got := ComputeDirectionalConsistency(values)
	if got.Consistency > 0.6 {
		t.Errorf("alternating series should be near 0.5 consistency, got %f", got.Consistency)
	}
}

func TestComputeDirectionalConsistency_FlagsZeroChangeStaleSeries(t *testing.T) {
	values := []float64{5, 5, 5, 5}
	got := ComputeDirectionalConsistency(values)
	if !got.IsZeroChange {
		t.Error("expected IsZeroChange=true for constant series")
	}
}

func TestCheckFeatureStability_ColdStartTreatedAsStable(t *testing.T) {
	cfg := DefaultStabilityConfig()
	values := []float64{1, 5, 1, 5, 1} // would be unstable if we had >= MinBars
	r := CheckFeatureStability("x", values, cfg)
	if !r.Stable {
		t.Error("cold start (n<MinBars) should be Stable=true")
	}
}

func TestCheckFeatureStability_OscillatingFailsTheGate(t *testing.T) {
	cfg := StabilityConfig{MinConsistency: 0.6, MinBars: 4, Lookback: 50}
	values := []float64{1, 2, 1, 2, 1, 2, 1, 2}
	r := CheckFeatureStability("x", values, cfg)
	if r.Stable {
		t.Errorf("expected Stable=false for oscillating series, got %+v", r)
	}
}

func TestRedistributeWeights_PreservesTotalAndZeroesUnstable(t *testing.T) {
	original := map[string]float64{"a": 0.4, "b": 0.4, "c": 0.2}
	stable := map[string]bool{"a": true, "b": true, "c": false}
	got := RedistributeWeights(original, stable)
	if got["c"] != 0 {
		t.Errorf("unstable weight not zeroed: %f", got["c"])
	}
	total := got["a"] + got["b"] + got["c"]
	if math.Abs(total-1.0) > 1e-12 {
		t.Errorf("total weight not preserved: %f", total)
	}
	// 0.2 redistributed proportionally to a and b in 0.5/0.5 split → +0.1 each
	if math.Abs(got["a"]-0.5) > 1e-12 || math.Abs(got["b"]-0.5) > 1e-12 {
		t.Errorf("redistribution wrong: got %v", got)
	}
}

// Integration: a feature with low directional consistency receives weight=0
// and the residual is redistributed across stable features.
func TestProcessWithContext_StabilityGate_ZeroesUnstableConfidence(t *testing.T) {
	m := New(nil)
	// Force MinBars low so the test history triggers the gate.
	m.stabilityCfg = StabilityConfig{MinConsistency: 0.6, MinBars: 6, Lookback: 50}

	dq := passedDQ()
	snap := snapForToken("AAA")

	// Build history where tx_velocity oscillates (unstable) and the rest
	// trend monotonically (stable).
	osc := make([]float64, 0, 12)
	mono := make([]float64, 0, 12)
	for i := 0; i < 12; i++ {
		osc = append(osc, float64(1+i%2)) // 1,2,1,2,...
		mono = append(mono, float64(i+1)) // 1,2,3,4,...
	}
	base := BaselineSnapshot{
		Market: snap.Market,
		History: map[string][]float64{
			SignalLiquidity:      mono,
			SignalTxVelocity:     osc,
			SignalHolderDist:     mono,
			SignalWalletEntropy:  mono,
			SignalVolumeMomentum: mono,
			SignalPriceMomentum:  mono,
		},
	}

	out, err := m.ProcessWithContext(context.Background(), dq, snap, base, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Confidence.TxVelocityScore != 0 {
		t.Errorf("expected unstable feature confidence to be zeroed, got %f",
			out.Confidence.TxVelocityScore)
	}
	// Redistribution: stable features should each be > the un-gated baseline
	// for at least one of them. Sample LiquidityScore which has a known >=0.4 base.
	if out.Confidence.LiquidityScore <= 0.4 {
		t.Errorf("stable feature did not absorb redistributed weight, got %f",
			out.Confidence.LiquidityScore)
	}
}
