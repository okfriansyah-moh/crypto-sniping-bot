package features

import (
	"math"
	"testing"
)

func TestSigmoidNormalize_BoundedAndSymmetric(t *testing.T) {
	cfg := DefaultNormalizerConfig()
	if r := SigmoidNormalize(0, cfg); math.Abs(r.Score) > 1e-12 {
		t.Errorf("sigmoid(0) must be 0, got %f", r.Score)
	}
	pos := SigmoidNormalize(2, cfg).Score
	neg := SigmoidNormalize(-2, cfg).Score
	if pos <= 0 || neg >= 0 {
		t.Errorf("sigmoid sign incorrect: pos=%f neg=%f", pos, neg)
	}
	if math.Abs(pos+neg) > 1e-12 {
		t.Errorf("sigmoid not antisymmetric: pos=%f neg=%f", pos, neg)
	}
}

func TestSigmoidNormalize_ClampsLargeInputs(t *testing.T) {
	cfg := DefaultNormalizerConfig()
	r := SigmoidNormalize(1e6, cfg)
	if !r.Clamped {
		t.Error("expected Clamped=true for large raw input")
	}
	if r.Score >= 1.0 || r.Score <= 0.99 {
		t.Errorf("score should approach but not reach 1, got %f", r.Score)
	}
}

func TestNormalizeSignalVariance_FloorsConstantSeries(t *testing.T) {
	cfg := DefaultNormalizerConfig()
	hist := []float64{5, 5, 5, 5, 5}
	r := NormalizeSignalVariance(5, hist, cfg)
	if !r.Floored {
		t.Error("expected sigma floor for constant series")
	}
	if r.ZScore != 0 {
		t.Errorf("z-score for raw=mean must be 0, got %f", r.ZScore)
	}
}

func TestNormalizeSignal_ColdStartProducesVaryingOutputs(t *testing.T) {
	// With no history, raw=1 and raw=10 must NOT produce the same score.
	cfg := DefaultNormalizerConfig()
	a := NormalizeSignal(1, nil, cfg)
	b := NormalizeSignal(10, nil, cfg)
	if !a.ColdStart || !b.ColdStart {
		t.Error("expected ColdStart=true for empty history")
	}
	if a.Score == b.Score {
		t.Errorf("cold-start collapsed to a constant: %f vs %f", a.Score, b.Score)
	}
}

func TestNormalizeSignal_HighZScoreSaturatesMonotonically(t *testing.T) {
	cfg := DefaultNormalizerConfig()
	hist := []float64{1, 1.1, 0.9, 1.05, 0.95, 1.0, 1.02, 0.98, 1.01, 0.99,
		1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0,
		1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0}
	low := NormalizeSignal(1.05, hist, cfg)
	high := NormalizeSignal(50, hist, cfg)
	if high.Score <= low.Score {
		t.Errorf("higher raw must produce higher score: low=%f high=%f", low.Score, high.Score)
	}
	if !high.Clamped {
		t.Error("expected raw clamp on extreme z-score")
	}
}

func TestNormalizeSignal_ScoreUnit01IsAffineMapOfScore(t *testing.T) {
	cfg := DefaultNormalizerConfig()
	r := NormalizeSignal(2, []float64{0, 1, 2, 3}, cfg)
	expected := 0.5*r.Score + 0.5
	if math.Abs(r.ScoreUnit01-expected) > 1e-12 {
		t.Errorf("ScoreUnit01 should be (Score+1)/2: got %f want %f", r.ScoreUnit01, expected)
	}
}
