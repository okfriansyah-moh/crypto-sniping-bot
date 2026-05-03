// confidence_gate_test.go tests the confidence gate logic in ProcessWithEstimates
// and the EV int32 overflow clamping (clipInt32):
//   - low Confidence triggers prior fallback ("low_model_confidence")
//   - Confidence==0 (cold-start) must NOT trigger fallback — model probability used directly
//   - high Confidence uses model probability with no fallback
//   - F-SEC-07: EV int32 cast is clamped to [MinInt32+1, MaxInt32-1]
package validation

import (
	"context"
	"crypto-sniping-bot/contracts"
	"math"
	"testing"
)

// B3a — Confidence (populated by B2) is read instead of Calibration.
func TestProcessWithEstimates_LowConfidenceField_FallsBack(t *testing.T) {
	mod := New(validationCfg()).WithProbabilityRuntime(phase9ProbCfg())
	// Confidence below MinModelConfidence (0.40); Calibration high (would
	// have passed under the old code path). The new reader must look at
	// Confidence and fall back to the prior.
	prob := &contracts.ProbabilityEstimateDTO{
		Probability: 0.7,
		Confidence:  0.10,
		Calibration: 0.99,
	}
	got, _ := mod.ProcessWithEstimates(context.Background(), goodEdge(), prob, nil, nil)
	if math.Abs(got.ProbabilityUsed-0.55) > 1e-9 {
		t.Fatalf("expected fallback to prior 0.55; got %v (Confidence path not taken)", got.ProbabilityUsed)
	}
	if got.Decision == "ACCEPT" && got.RejectReason != "" {
		t.Fatalf("RejectReason must be empty on ACCEPT; got %q", got.RejectReason)
	}
}

// B3a — when Confidence==0 (cold-start, no feature-confidence data), the
// model probability MUST be used directly. Calibration (BrierCalibration) is
// a model-accuracy metric (lower=better) and MUST NOT be used as a confidence
// proxy — doing so caused all cold-start tokens to fail the gate.
func TestProcessWithEstimates_ZeroConfidenceUsesProbability(t *testing.T) {
	mod := New(validationCfg()).WithProbabilityRuntime(phase9ProbCfg())
	// Confidence==0 (cold-start), Calibration low (BrierCalibration = 0.18).
	// The gate must NOT fire: p must be set to prob.Probability, not the prior.
	prob := &contracts.ProbabilityEstimateDTO{
		Probability: 0.7,
		Confidence:  0,
		Calibration: 0.10,
	}
	got, _ := mod.ProcessWithEstimates(context.Background(), goodEdge(), prob, nil, nil)
	if math.Abs(got.ProbabilityUsed-0.7) > 1e-9 {
		t.Fatalf("cold-start (Confidence=0) must use model probability 0.7; got ProbabilityUsed=%v (prior fallback regression)", got.ProbabilityUsed)
	}
}

// B3a — Confidence high → model probability used (no fallback).
func TestProcessWithEstimates_HighConfidenceUsesModel(t *testing.T) {
	mod := New(validationCfg()).WithProbabilityRuntime(phase9ProbCfg())
	prob := &contracts.ProbabilityEstimateDTO{
		Probability: 0.7,
		Confidence:  0.80,
		Calibration: 0.05, // would have triggered fallback under old reader
	}
	got, _ := mod.ProcessWithEstimates(context.Background(), goodEdge(), prob, nil, nil)
	if math.Abs(got.ProbabilityUsed-0.7) > 1e-9 {
		t.Fatalf("expected model probability 0.7; got %v", got.ProbabilityUsed)
	}
}

// B3b / F-SEC-07 — clipInt32 clamps overflow values to int32 saturation
// bounds rather than wrapping. The previous int32() cast on a float
// exceeding ±2^31 would silently produce a wraparound value, allowing
// rejected trades to report nonsense ExpectedValueBps.
func TestEV_OverflowClamped(t *testing.T) {
	cases := []struct {
		name string
		in   float64
		want int32
	}{
		{"large_positive", 5e9, int32(math.MaxInt32 - 1)},
		{"large_negative", -5e9, int32(math.MinInt32 + 1)},
		{"pos_inf", math.Inf(1), int32(math.MaxInt32 - 1)},
		{"neg_inf", math.Inf(-1), int32(math.MinInt32 + 1)},
		{"nan", math.NaN(), 0},
		{"in_range_rounded", 123.6, 124},
		{"in_range_negative", -42.4, -42},
	}
	for _, tc := range cases {
		if got := clipInt32(tc.in); got != tc.want {
			t.Errorf("clipInt32(%v) = %d; want %d", tc.in, got, tc.want)
		}
	}
}
