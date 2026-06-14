package features

import "math"

// Two-stage signal normalization (per the signal-normalizer skill):
//
//  1. Z-score over a rolling per-(market, signal) baseline.
//  2. Sigmoid compression to (-1, +1).
//
// Pure functions — no clocks, no globals, no I/O. Same inputs always
// yield identical outputs. Used by the feature extractor to turn raw
// on-chain signals into bounded, distribution-aware scores.

// NormalizerConfig captures the tunables for the two-stage pipeline.
// SigmoidK ∈ (0, ∞) — higher = sharper transitions around z=0.
// Lookback bounds the rolling window used for μ and σ.
// MinSamples controls the cold-start fallback: below this, output uses a
// fixed-bound min/max scaling instead of the z-score baseline.
type NormalizerConfig struct {
	Lookback   int
	SigmoidK   float64
	MinSamples int

	// SigmaFloor prevents division by zero on constant signals.
	SigmaFloor float64
	// RawClamp bounds the z-score before exp() to prevent float64 overflow.
	RawClamp float64
}

// DefaultNormalizerConfig mirrors the signal-normalizer skill defaults.
// Used only as a fallback when a *config.Config is not supplied.
func DefaultNormalizerConfig() NormalizerConfig {
	return NormalizerConfig{
		Lookback:   60,
		SigmoidK:   3.0,
		MinSamples: 20,
		SigmaFloor: 1e-8,
		RawClamp:   10.0,
	}
}

// ZScoreResult carries the intermediate Stage-1 outputs.
type ZScoreResult struct {
	ZScore  float64
	Mean    float64
	Sigma   float64
	N       int
	Floored bool // sigma was floored to SigmaFloor (constant or near-constant)
}

// NormalizeSignalVariance computes the z-score of raw against history.
// history is the rolling window of past raw values for the same signal.
// When history is empty, returns a neutral z-score of 0 with N=0.
func NormalizeSignalVariance(raw float64, history []float64, cfg NormalizerConfig) ZScoreResult {
	if len(history) == 0 {
		return ZScoreResult{ZScore: 0, N: 0}
	}
	window := history
	if cfg.Lookback > 0 && len(history) > cfg.Lookback {
		window = history[len(history)-cfg.Lookback:]
	}
	n := len(window)
	var sum float64
	for _, v := range window {
		sum += v
	}
	mu := sum / float64(n)

	var variance float64
	for _, v := range window {
		d := v - mu
		variance += d * d
	}
	sigma := math.Sqrt(variance / float64(n))

	floored := false
	floor := cfg.SigmaFloor
	if floor <= 0 {
		floor = 1e-8
	}
	if sigma < floor {
		sigma = floor
		floored = true
	}
	return ZScoreResult{
		ZScore:  (raw - mu) / sigma,
		Mean:    mu,
		Sigma:   sigma,
		N:       n,
		Floored: floored,
	}
}

// SigmoidResult carries the Stage-2 output and clamp diagnostics.
type SigmoidResult struct {
	Score   float64 // bounded to (-1, +1)
	Clamped bool
}

// SigmoidNormalize compresses a z-score (or weighted composite) to (-1, +1).
// Raw is clamped to [-RawClamp, +RawClamp] before exp() to prevent overflow.
func SigmoidNormalize(raw float64, cfg NormalizerConfig) SigmoidResult {
	bound := cfg.RawClamp
	if bound <= 0 {
		bound = 10.0
	}
	clamped := false
	if raw > bound {
		raw = bound
		clamped = true
	} else if raw < -bound {
		raw = -bound
		clamped = true
	}
	k := cfg.SigmoidK
	if k <= 0 {
		k = 3.0
	}
	score := (2.0 / (1.0 + math.Exp(-k*raw))) - 1.0
	return SigmoidResult{Score: score, Clamped: clamped}
}

// NormalizedSignal is the canonical bounded output of the two-stage pipeline.
// Score is the [-1, +1] composite. ScoreUnit01 is the same value remapped to
// [0, 1] for FeatureDTO fields whose contract is "[0, 1] normalized".
type NormalizedSignal struct {
	Raw         float64
	ZScore      float64
	Score       float64 // [-1, +1]
	ScoreUnit01 float64 // [0, 1]
	N           int
	ColdStart   bool // true when N < MinSamples — caller should mark confidence low
	SigmaFloor  bool
	Clamped     bool
}

// NormalizeSignal applies the full pipeline:
//
//	raw → z-score (Stage 1) → sigmoid (Stage 2) → [-1, +1]
//
// Cold-start path: when len(history) < MinSamples, the signal is normalized
// with a deterministic min/max-style fallback against the rolling window's
// (or, when empty, a single-sample) mean using a simple subtraction so the
// raw value still moves the output. This avoids returning a constant.
func NormalizeSignal(raw float64, history []float64, cfg NormalizerConfig) NormalizedSignal {
	z := NormalizeSignalVariance(raw, history, cfg)
	coldStart := z.N < cfg.MinSamples

	// Cold-start fallback: when there is not enough history for a stable
	// baseline, pre-compress the raw value through a single sigmoid using
	// (raw - mean) as the input so different inputs still produce
	// different outputs. This deliberately avoids the (raw, σ=∞) case that
	// would saturate the sigmoid — keeps cold-start scores meaningfully
	// distinct without requiring fabricated history.
	zForSigmoid := z.ZScore
	if coldStart {
		zForSigmoid = raw - z.Mean
		if z.N == 0 {
			// No history at all → use the raw value scaled by RawClamp so
			// it still varies with input. SigmoidNormalize will clamp.
			zForSigmoid = raw
		}
	}
	s := SigmoidNormalize(zForSigmoid, cfg)
	return NormalizedSignal{
		Raw:         raw,
		ZScore:      z.ZScore,
		Score:       s.Score,
		ScoreUnit01: 0.5*(s.Score) + 0.5,
		N:           z.N,
		ColdStart:   coldStart,
		SigmaFloor:  z.Floored,
		Clamped:     s.Clamped,
	}
}
