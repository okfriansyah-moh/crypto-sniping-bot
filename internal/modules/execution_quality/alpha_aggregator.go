// Package execution_quality computes post-trade execution quality metrics.
// AlphaAggregator derives the per-market slippage α calibration coefficient
// from realized fills, closing residual risk #3 (the GetSlippageAlpha stub).
//
// α = ewma_realized_bps / ewma_predicted_bps, EWMA-weighted toward recent
// samples, clamped to [AlphaMin, AlphaMax]. Pure function — no I/O.
package execution_quality

import (
	"math"
	"time"
)

// FillSample is one realized-vs-predicted slippage observation.
type FillSample struct {
	PredictedBps float64
	RealizedBps  float64
	At           time.Time
}

// AlphaAggregatorConfig governs α computation. Loaded from
// config/pipeline.yaml under execution_quality.alpha.
type AlphaAggregatorConfig struct {
	MinSampleCount    int     `yaml:"min_sample_count"`    // default 30
	AlphaMin          float64 `yaml:"alpha_min"`           // default 0.5
	AlphaMax          float64 `yaml:"alpha_max"`           // default 2.0
	EwmaHalflifeSec   int     `yaml:"ewma_halflife_sec"`   // default 3600
	UpdateIntervalSec int     `yaml:"update_interval_sec"` // default 300
}

// DefaultAlphaAggregatorConfig returns conservative defaults.
func DefaultAlphaAggregatorConfig() AlphaAggregatorConfig {
	return AlphaAggregatorConfig{
		MinSampleCount:    30,
		AlphaMin:          0.5,
		AlphaMax:          2.0,
		EwmaHalflifeSec:   3600,
		UpdateIntervalSec: 300,
	}
}

func withAlphaDefaults(cfg AlphaAggregatorConfig) AlphaAggregatorConfig {
	d := DefaultAlphaAggregatorConfig()
	if cfg.MinSampleCount <= 0 {
		cfg.MinSampleCount = d.MinSampleCount
	}
	if cfg.AlphaMin <= 0 {
		cfg.AlphaMin = d.AlphaMin
	}
	if cfg.AlphaMax <= 0 {
		cfg.AlphaMax = d.AlphaMax
	}
	if cfg.AlphaMin >= cfg.AlphaMax {
		cfg.AlphaMin = d.AlphaMin
		cfg.AlphaMax = d.AlphaMax
	}
	if cfg.EwmaHalflifeSec <= 0 {
		cfg.EwmaHalflifeSec = d.EwmaHalflifeSec
	}
	if cfg.UpdateIntervalSec <= 0 {
		cfg.UpdateIntervalSec = d.UpdateIntervalSec
	}
	return cfg
}

// ComputeMarketAlpha computes α for one market from realized-fill samples.
// Pure: no I/O, deterministic, `now` is injected.
//
//   - α = clamp(ewma_realized / ewma_predicted, AlphaMin, AlphaMax)
//   - Empty / all-invalid input → α=1.0, ewmaPred=ewmaReal=0, sampleCount=0.
//   - Below MinSampleCount sample-count gating is the caller's job — this
//     function returns whatever α the data implies.
func ComputeMarketAlpha(
	samples []FillSample,
	cfg AlphaAggregatorConfig,
	now time.Time,
) (alpha float64, ewmaPred float64, ewmaReal float64, sampleCount int) {
	cfg = withAlphaDefaults(cfg)

	// λ = ln(2) / halflife. Decay weight = exp(-λ * age_seconds).
	lambda := math.Ln2 / float64(cfg.EwmaHalflifeSec)

	var sumWPred, sumWReal, sumW float64
	for _, s := range samples {
		if !isFinite(s.PredictedBps) || !isFinite(s.RealizedBps) {
			continue
		}
		if s.PredictedBps <= 0 || s.RealizedBps <= 0 {
			continue
		}
		ageSec := now.Sub(s.At).Seconds()
		if ageSec < 0 {
			ageSec = 0 // future-dated samples treated as "now"
		}
		w := math.Exp(-lambda * ageSec)
		if !isFinite(w) || w <= 0 {
			continue
		}
		sumW += w
		sumWPred += w * s.PredictedBps
		sumWReal += w * s.RealizedBps
		sampleCount++
	}

	if sampleCount == 0 || sumW <= 0 {
		return 1.0, 0, 0, 0
	}

	ewmaPred = sumWPred / sumW
	ewmaReal = sumWReal / sumW

	if ewmaPred <= 0 || !isFinite(ewmaPred) || !isFinite(ewmaReal) {
		return 1.0, ewmaPred, ewmaReal, sampleCount
	}

	alpha = ewmaReal / ewmaPred
	if !isFinite(alpha) {
		return 1.0, ewmaPred, ewmaReal, sampleCount
	}
	if alpha < cfg.AlphaMin {
		alpha = cfg.AlphaMin
	}
	if alpha > cfg.AlphaMax {
		alpha = cfg.AlphaMax
	}
	return alpha, ewmaPred, ewmaReal, sampleCount
}

func isFinite(x float64) bool {
	return !math.IsNaN(x) && !math.IsInf(x, 0)
}
