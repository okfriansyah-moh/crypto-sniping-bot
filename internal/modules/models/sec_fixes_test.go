// F-SEC-02 / F-SEC-03 regression tests for the slippage and probability
// models. These tests cover:
//
//   - F-SEC-02 — NaN / ±Inf inputs in numeric / Confidence fields MUST be
//     rejected at the entry point (slippage) and skipped (probability)
//     so they never surface in DTO.Confidence.
//   - F-SEC-03 — slippage EventID is content-addressable on (feature,
//     size) only. Drift in α (a runtime calibration parameter) MUST NOT
//     change EventID.
package models

import (
	"context"
	"math"
	"testing"

	"crypto-sniping-bot/contracts"
)

// secFeature returns a clean, finite, non-degenerate feature input.
func secFeature() contracts.FeatureDTO {
	return contracts.FeatureDTO{
		EventID:          "feat-sec",
		TraceID:          "trace-sec",
		CorrelationID:    "corr-sec",
		VersionID:        "v1",
		TokenLifecycleID: "lc-sec",
		LiquidityUsdRaw:  100_000,
		LiquidityScore:   0.6,
		PriceMomentum:    0.5,
		Confidence: contracts.FeatureConfidence{
			LiquidityScore: 0.8,
		},
	}
}

// TestSlippage_NaNLiquidityScoreConfidence_Rejects (F-SEC-02): the entry
// point now treats a non-finite LiquidityScore confidence as an invalid
// input and refuses to emit an estimate. Previously NaN passed through
// clipFloat and surfaced in DTO.Confidence.
func TestSlippage_NaNLiquidityScoreConfidence_Rejects(t *testing.T) {
	m := NewSlippageModel(DefaultSlippageConfig())
	feat := secFeature()
	feat.Confidence.LiquidityScore = math.NaN()

	if _, err := m.Estimate(context.Background(), feat, 100); err != ErrInvalidSlippageInput {
		t.Fatalf("expected ErrInvalidSlippageInput for NaN confidence; got %v", err)
	}
}

// TestSlippage_FiniteConfidenceAlwaysFinite (F-SEC-02): even when an
// upstream extreme value reaches clipFloat, the emitted DTO.Confidence
// must be finite and within [0, 1].
func TestSlippage_FiniteConfidenceAlwaysFinite(t *testing.T) {
	m := NewSlippageModel(DefaultSlippageConfig())
	feat := secFeature()
	// Finite but large; clipFloat must not propagate anything pathological.
	feat.Confidence.LiquidityScore = 1.0

	out, err := m.Estimate(context.Background(), feat, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.IsNaN(out.Confidence) || math.IsInf(out.Confidence, 0) {
		t.Errorf("DTO.Confidence must be finite; got %v", out.Confidence)
	}
	if out.Confidence < 0 || out.Confidence > 1 {
		t.Errorf("DTO.Confidence must be in [0, 1]; got %v", out.Confidence)
	}
}

// TestProbability_NaNFeatureConfidence_NotPropagated (F-SEC-02):
// minFeatureConfidence treats NaN/Inf as missing rather than letting them
// poison the emitted ProbabilityEstimateDTO.Confidence.
func TestProbability_NaNFeatureConfidence_NotPropagated(t *testing.T) {
	m := NewProbabilityModel(DefaultLogisticConfig())
	in := contracts.FeatureDTO{
		EventID:         "feat-prob-sec",
		TraceID:         "trace-prob-sec",
		LiquidityScore:  0.5,
		TxVelocityScore: 0.5,
		ContractSafety:  0.7,
		Confidence: contracts.FeatureConfidence{
			LiquidityScore:  math.NaN(),
			TxVelocityScore: math.Inf(1),
			ContractSafety:  0.6,
		},
	}

	out, err := m.Predict(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.IsNaN(out.Confidence) || math.IsInf(out.Confidence, 0) {
		t.Errorf("Confidence must not be NaN/Inf; got %v", out.Confidence)
	}
	// The only finite positive slot is ContractSafety=0.6 → must surface.
	if out.Confidence != 0.6 {
		t.Errorf("expected min finite confidence 0.6; got %v", out.Confidence)
	}
}

// stubAlpha is a SlippageAlphaProvider returning a fixed α.
type stubAlpha struct{ v float64 }

func (s *stubAlpha) GetSlippageAlpha(_ context.Context, _ string) (float64, error) {
	return s.v, nil
}

// TestSlippage_EventIDIndependentOfAlpha (F-SEC-03): identical
// (feature.EventID, size) MUST yield identical EventID regardless of α.
// Previously α was hashed into the eventID — any future α-learning update
// would mass-rewrite identities of otherwise-equivalent emissions.
func TestSlippage_EventIDIndependentOfAlpha(t *testing.T) {
	feat := secFeature()
	const size = 250.0

	m1 := NewSlippageModelWithAlpha(DefaultSlippageConfig(), &stubAlpha{v: 1.0})
	m2 := NewSlippageModelWithAlpha(DefaultSlippageConfig(), &stubAlpha{v: 2.0})

	out1, err := m1.EstimateForMarket(context.Background(), feat, size, "eth-uniswap-v2")
	if err != nil {
		t.Fatalf("m1 estimate: %v", err)
	}
	out2, err := m2.EstimateForMarket(context.Background(), feat, size, "eth-uniswap-v2")
	if err != nil {
		t.Fatalf("m2 estimate: %v", err)
	}
	if out1.EventID != out2.EventID {
		t.Errorf("EventID must be α-independent (F-SEC-03): %q vs %q", out1.EventID, out2.EventID)
	}
	// Sanity: α actually moved the bps output, otherwise the test is
	// vacuous (would also pass if α were silently ignored).
	if out1.ExpectedP50Bps == out2.ExpectedP50Bps {
		t.Errorf("expected α to change ExpectedP50Bps; both are %d", out1.ExpectedP50Bps)
	}
}
