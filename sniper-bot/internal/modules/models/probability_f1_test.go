package models

// F-1 (HIGH) regression tests — log-reviewer flagged a constant
// probability_scored.probability across all distinct trace_ids in production.
// The model itself is canonical logistic; the constant output came from
// constant feature inputs (F-2). These tests lock in:
//   - variance under varied inputs (regression bug-driver),
//   - determinism across many runs,
//   - clipping to configured [p_min, p_max],
//   - confidence propagation from FeatureConfidence,
//   - weights/version id presence on every emission,
//   - calibration_bin emission for learning-engine bucketing.

import (
	"context"
	"math"
	"testing"

	"crypto-sniping-bot/shared/contracts"
)

// makeFeature builds a deterministic FeatureDTO from a seed integer so we can
// drive the model with 100 distinct, non-random inputs.
func makeFeature(seed int) contracts.FeatureDTO {
	// Spread each feature across [0, 1] using prime-multiplied modulo so
	// neighbouring seeds don't produce highly-correlated rows.
	f := func(p int) float64 {
		v := float64((seed*p)%101) / 100.0
		if v < 0 {
			v = -v
		}
		return v
	}
	return contracts.FeatureDTO{
		EventID:            "feat-" + intToStr(seed),
		TraceID:            "trace-" + intToStr(seed),
		CorrelationID:      "corr-" + intToStr(seed),
		CausationID:        "dq-" + intToStr(seed),
		VersionID:          "ver-1",
		TokenLifecycleID:   "tok-" + intToStr(seed),
		LiquidityScore:     f(7),
		TxVelocityScore:    f(11),
		HolderDistribution: f(13),
		WalletEntropy:      f(17),
		ContractSafety:     f(19),
		TokenAge:           f(23),
		VolumeMomentum:     f(29),
		PriceMomentum:      f(31),
		Confidence: contracts.FeatureConfidence{
			LiquidityScore:     0.9,
			TxVelocityScore:    0.9,
			HolderDistribution: 0.9,
			WalletEntropy:      0.9,
			ContractSafety:     0.9,
			TokenAge:           0.9,
			VolumeMomentum:     0.9,
			PriceMomentum:      0.9,
		},
		LiquidityUsdRaw: 100_000,
		ExtractedAt:     "2026-04-30T00:00:00Z",
	}
}

func intToStr(i int) string {
	// Tiny stdlib-free int→string to avoid pulling strconv into this test
	// for clarity; correctness only matters up to 1000.
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [12]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// TestProbabilityVariesWithInputs is the F-1 bug-driver regression. With
// 100 distinct FeatureDTOs the model MUST emit at least 20 unique
// probabilities — anything less means the model collapsed (stub or NaN
// trap or constant inputs).
func TestProbabilityVariesWithInputs(t *testing.T) {
	m := NewProbabilityModel(DefaultLogisticConfig())
	unique := make(map[float64]struct{})
	for i := 0; i < 100; i++ {
		out, err := m.Predict(context.Background(), makeFeature(i))
		if err != nil {
			t.Fatalf("predict[%d] err: %v", i, err)
		}
		unique[out.Probability] = struct{}{}
	}
	if len(unique) < 20 {
		t.Fatalf("F-1 regression: expected ≥20 distinct probabilities across 100 inputs, got %d", len(unique))
	}
}

// TestProbabilityDeterministicAcross100Runs locks the determinism invariant.
// Same input + same weights → identical Probability/EventID across 100 calls.
func TestProbabilityDeterministicAcross100Runs(t *testing.T) {
	m := NewProbabilityModel(DefaultLogisticConfig())
	in := makeFeature(42)
	first, err := m.Predict(context.Background(), in)
	if err != nil {
		t.Fatalf("baseline predict err: %v", err)
	}
	for i := 0; i < 100; i++ {
		out, err := m.Predict(context.Background(), in)
		if err != nil {
			t.Fatalf("predict[%d] err: %v", i, err)
		}
		if out.Probability != first.Probability {
			t.Fatalf("non-deterministic at run %d: %.18f vs baseline %.18f", i, out.Probability, first.Probability)
		}
		if out.EventID != first.EventID {
			t.Fatalf("event id drift at run %d: %s vs %s", i, out.EventID, first.EventID)
		}
		if out.CalibrationBin != first.CalibrationBin {
			t.Fatalf("calibration bin drift at run %d", i)
		}
	}
}

// TestProbabilityClipsAtBothBounds covers extreme inputs in both directions.
func TestProbabilityClipsAtBothBounds(t *testing.T) {
	cfg := DefaultLogisticConfig()
	m := NewProbabilityModel(cfg)

	allHigh := makeFeature(1)
	allHigh.LiquidityScore = 1
	allHigh.TxVelocityScore = 1
	allHigh.HolderDistribution = 1
	allHigh.WalletEntropy = 1
	allHigh.ContractSafety = 1
	allHigh.TokenAge = 1
	allHigh.VolumeMomentum = 1
	allHigh.PriceMomentum = 1

	allLow := makeFeature(2)
	allLow.LiquidityScore = -1
	allLow.TxVelocityScore = -1
	allLow.HolderDistribution = -1
	allLow.WalletEntropy = -1
	allLow.ContractSafety = -1
	allLow.TokenAge = -1
	allLow.VolumeMomentum = -1
	allLow.PriceMomentum = -1

	hi, err := m.Predict(context.Background(), allHigh)
	if err != nil {
		t.Fatalf("predict high err: %v", err)
	}
	lo, err := m.Predict(context.Background(), allLow)
	if err != nil {
		t.Fatalf("predict low err: %v", err)
	}
	if hi.Probability > cfg.MaxProbability {
		t.Fatalf("high tail not clipped: %v > %v", hi.Probability, cfg.MaxProbability)
	}
	if lo.Probability < cfg.MinProbability {
		t.Fatalf("low tail not clipped: %v < %v", lo.Probability, cfg.MinProbability)
	}
	if hi.Probability >= 1.0 || lo.Probability <= 0.0 {
		t.Fatalf("probability must never reach 0 or 1: hi=%v lo=%v", hi.Probability, lo.Probability)
	}
}

// TestProbabilityConfidencePropagatesLow — low FeatureConfidence in
// → low ProbabilityEstimateDTO.Confidence out (per skill).
func TestProbabilityConfidencePropagatesLow(t *testing.T) {
	m := NewProbabilityModel(DefaultLogisticConfig())
	in := makeFeature(7)
	in.Confidence = contracts.FeatureConfidence{
		LiquidityScore:     0.9,
		TxVelocityScore:    0.9,
		HolderDistribution: 0.2, // weakest link
		WalletEntropy:      0.9,
		ContractSafety:     0.9,
		TokenAge:           0.9,
		VolumeMomentum:     0.9,
		PriceMomentum:      0.9,
	}
	out, err := m.Predict(context.Background(), in)
	if err != nil {
		t.Fatalf("predict err: %v", err)
	}
	if math.Abs(out.Confidence-0.2) > 1e-9 {
		t.Fatalf("confidence should propagate min FeatureConfidence (0.2), got %v", out.Confidence)
	}
}

// TestProbabilityVersionIDPopulated locks the strategy-versioning invariant:
// every emitted DTO carries a non-empty ModelVersionID (= weights_version_id).
func TestProbabilityVersionIDPopulated(t *testing.T) {
	m := NewProbabilityModel(DefaultLogisticConfig())
	for i := 0; i < 25; i++ {
		out, err := m.Predict(context.Background(), makeFeature(i))
		if err != nil {
			t.Fatalf("predict[%d] err: %v", i, err)
		}
		if out.ModelVersionID == "" {
			t.Fatalf("ModelVersionID empty on emission %d", i)
		}
	}
}

// TestProbabilityCalibrationBinRange verifies the decile is always in [0..9]
// and tracks the probability monotonically.
func TestProbabilityCalibrationBinRange(t *testing.T) {
	m := NewProbabilityModel(DefaultLogisticConfig())
	for i := 0; i < 100; i++ {
		out, err := m.Predict(context.Background(), makeFeature(i))
		if err != nil {
			t.Fatalf("predict[%d] err: %v", i, err)
		}
		if out.CalibrationBin < 0 || out.CalibrationBin > 9 {
			t.Fatalf("calibration_bin out of [0,9]: %d (p=%v)", out.CalibrationBin, out.Probability)
		}
		expected := int32(math.Floor(out.Probability * 10))
		if expected > 9 {
			expected = 9
		}
		if out.CalibrationBin != expected {
			t.Fatalf("calibration_bin mismatch: bin=%d p=%v expected=%d", out.CalibrationBin, out.Probability, expected)
		}
	}
}
