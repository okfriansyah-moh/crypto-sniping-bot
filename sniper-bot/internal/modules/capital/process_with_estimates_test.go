// Phase 9 (Profitability Restoration § 9.4) — ProcessWithEstimates tests.
package capital

import (
	"context"
	"math"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

func phase9Cfg() *config.CapitalConfig {
	return &config.CapitalConfig{
		FixedEntrySizeUsd:      50,
		MaxSizeUsd:             500,
		MinSizeUsd:             1,
		TTLSeconds:             3,
		UseDynamicSizing:       true,
		BaseSizeUsd:            50,
		MinAggregateConfidence: 0.0,
		Kelly: config.CapitalKellyConfig{
			Cap: 0.25, CapStrict: 0.10, CapExploration: 0.05,
			PriorGainBps: 3000, PriorLossBps: 4000, RejectNegative: true,
		},
		ModeMultipliers: map[string]float64{"STRICT": 0.5, "BALANCED": 1.0, "EXPLORATION": 1.3},
		Cohort:          config.CapitalCohortConfig{DefaultMultiplier: 1.0},
		FailurePolicy:   config.CapitalFailurePolicyConfig{OnMissingProbability: "reject"},
	}
}

func selectedInputP9() contracts.SelectionOutputDTO {
	return contracts.SelectionOutputDTO{
		EventID:          "sel1",
		TraceID:          "trace1",
		VersionID:        "v1",
		TokenLifecycleID: "tl1",
		TokenAddress:     "0xabc",
		Selected:         true,
		CombinedScore:    0.8,
	}
}

func featWithConfidence(c float64) *contracts.FeatureDTO {
	return &contracts.FeatureDTO{
		Confidence: contracts.FeatureConfidence{
			LiquidityScore: c, TxVelocityScore: c, ContractSafety: c,
			VolumeMomentum: c, PriceMomentum: c, WalletEntropy: c,
			HolderDistribution: c, TokenAge: c,
		},
	}
}

func TestProcessWithEstimates_DynamicSize(t *testing.T) {
	mod := New(phase9Cfg())
	prob := &contracts.ProbabilityEstimateDTO{Probability: 0.7, Calibration: 0.8}
	feat := featWithConfidence(0.7)

	got, err := mod.ProcessWithEstimates(context.Background(), selectedInputP9(), prob, feat, "BALANCED", "eth", 0)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Rejected {
		t.Fatalf("unexpected rejection: %s", got.RejectReason)
	}
	// base*score*p*conf*kelly_cap*mode = 50*0.8*0.7*0.7*0.25*1.0 = 4.9
	if math.Abs(got.SizeUsd-4.9) > 1e-6 {
		t.Errorf("size mismatch: got %v want ~4.9", got.SizeUsd)
	}
}

func TestProcessWithEstimates_MissingProbability_Rejects(t *testing.T) {
	mod := New(phase9Cfg())
	got, _ := mod.ProcessWithEstimates(context.Background(), selectedInputP9(), nil, nil, "BALANCED", "eth", 0)
	if !got.Rejected || got.RejectReason != "missing_probability" {
		t.Fatalf("expected missing_probability rejection, got rejected=%v reason=%q", got.Rejected, got.RejectReason)
	}
}

func TestProcessWithEstimates_NegativeKelly_Rejects(t *testing.T) {
	mod := New(phase9Cfg())
	prob := &contracts.ProbabilityEstimateDTO{Probability: 0.3} // negative kelly
	got, _ := mod.ProcessWithEstimates(context.Background(), selectedInputP9(), prob, nil, "BALANCED", "eth", 0)
	if !got.Rejected || got.RejectReason != "negative_kelly" {
		t.Fatalf("expected negative_kelly rejection, got rejected=%v reason=%q", got.Rejected, got.RejectReason)
	}
}

func TestProcessWithEstimates_LowAggregateConfidence_Rejects(t *testing.T) {
	cfg := phase9Cfg()
	cfg.MinAggregateConfidence = 0.5
	mod := New(cfg)
	prob := &contracts.ProbabilityEstimateDTO{Probability: 0.7}
	feat := featWithConfidence(0.2) // below threshold
	got, _ := mod.ProcessWithEstimates(context.Background(), selectedInputP9(), prob, feat, "BALANCED", "eth", 0)
	if !got.Rejected || got.RejectReason != "low_aggregate_confidence" {
		t.Fatalf("expected low_aggregate_confidence rejection, got %v %q", got.Rejected, got.RejectReason)
	}
}

func TestProcessWithEstimates_LegacyMode(t *testing.T) {
	cfg := phase9Cfg()
	cfg.UseDynamicSizing = false
	mod := New(cfg)
	got, err := mod.ProcessWithEstimates(context.Background(), selectedInputP9(), nil, nil, "BALANCED", "eth", 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Rejected {
		t.Fatalf("legacy mode should accept: %s", got.RejectReason)
	}
	if got.SizeUsd != cfg.FixedEntrySizeUsd {
		t.Errorf("legacy size: got %v want %v", got.SizeUsd, cfg.FixedEntrySizeUsd)
	}
}

// Phase 9 audit M3 regression: NaN/Inf CombinedScore must not be
// silently coerced to a max-favorable size (fail-open). The pre-fix
// behavior flattened NaN to 0 in clampUnit and then promoted 0 → 1.0.
func TestProcessWithEstimates_InvalidScore_Rejects(t *testing.T) {
	mod := New(phase9Cfg())
	prob := &contracts.ProbabilityEstimateDTO{Probability: 0.7, Calibration: 0.8}
	feat := featWithConfidence(0.7)

	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		in := selectedInputP9()
		in.CombinedScore = bad
		got, err := mod.ProcessWithEstimates(context.Background(), in, prob, feat, "BALANCED", "eth", 0)
		if err != nil {
			t.Fatalf("unexpected err for score %v: %v", bad, err)
		}
		if !got.Rejected || got.RejectReason != "invalid_score" {
			t.Errorf("score=%v: expected invalid_score reject, got rejected=%v reason=%q size=%v",
				bad, got.Rejected, got.RejectReason, got.SizeUsd)
		}
		if got.SizeUsd != 0 {
			t.Errorf("score=%v: rejected allocation must have SizeUsd=0, got %v", bad, got.SizeUsd)
		}
	}
}

func TestProcessWithEstimates_NotSelected(t *testing.T) {
	mod := New(phase9Cfg())
	in := selectedInputP9()
	in.Selected = false
	in.RejectReason = "below_top_k"
	got, _ := mod.ProcessWithEstimates(context.Background(), in, nil, nil, "BALANCED", "eth", 0)
	if !got.Rejected || got.RejectReason != "below_top_k" {
		t.Errorf("expected pass-through rejection: got %v", got)
	}
}

// Phase 9 audit M2 regression: when ModeMultipliers map lacks the active
// mode and FailurePolicy.OnModeLookupStale="reject", engine must reject
// rather than silently falling back to BALANCED (fail-open).
func TestProcessWithEstimates_ModeLookupStaleReject(t *testing.T) {
	cfg := phase9Cfg()
	cfg.FailurePolicy.OnModeLookupStale = "reject"
	cfg.ModeMultipliers = map[string]float64{"BALANCED": 1.0} // STRICT/EXPLORATION absent
	mod := New(cfg)
	prob := &contracts.ProbabilityEstimateDTO{Probability: 0.6}
	feat := &contracts.FeatureDTO{Confidence: contracts.FeatureConfidence{LiquidityScore: 0.9}}
	got, _ := mod.ProcessWithEstimates(context.Background(), selectedInputP9(), prob, feat, "UNKNOWN_MODE", "eth", 0)
	if !got.Rejected || got.RejectReason != "mode_lookup_stale" {
		t.Errorf("expected mode_lookup_stale reject, got rejected=%v reason=%q", got.Rejected, got.RejectReason)
	}
}

// Phase 9 audit M1 regression: NaN entries in FeatureConfidence must not
// poison aggregateConfidence into +Inf and propagate a non-finite size.
func TestProcessWithEstimates_NaNConfidence_NotFailOpen(t *testing.T) {
	mod := New(phase9Cfg())
	prob := &contracts.ProbabilityEstimateDTO{Probability: 0.6}
	feat := &contracts.FeatureDTO{Confidence: contracts.FeatureConfidence{
		LiquidityScore: math.NaN(),
		ContractSafety: math.Inf(1),
		WalletEntropy:  0.7,
	}}
	got, _ := mod.ProcessWithEstimates(context.Background(), selectedInputP9(), prob, feat, "BALANCED", "eth", 0)
	if got.Rejected {
		// Acceptable: rejected for a sane reason
		return
	}
	if math.IsNaN(got.SizeUsd) || math.IsInf(got.SizeUsd, 0) || got.SizeUsd <= 0 {
		t.Errorf("non-finite/zero size leaked through: %v", got.SizeUsd)
	}
}
