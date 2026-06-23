// Phase 9 (Profitability Restoration § 9.4) — Kelly fraction tests.
package capital

import (
	"math"
	"testing"

	"crypto-sniping-bot/internal/app/config"
)

func kellyCfg() config.CapitalKellyConfig {
	return config.CapitalKellyConfig{
		Cap:            0.25,
		CapExploration: 0.05,
		CapStrict:      0.10,
		PriorGainBps:   3000,
		PriorLossBps:   4000,
		RejectNegative: true,
	}
}

func TestKellyFraction_Positive(t *testing.T) {
	k := kellyCfg()
	// p=0.7, R=3000/4000=0.75: f=(0.7*0.75 - 0.3)/0.75 = 0.225/0.75 = 0.3, capped at 0.25.
	got := KellyFraction(0.7, k, k.Cap)
	if math.Abs(got-0.25) > 1e-9 {
		t.Fatalf("expected cap=0.25, got %v", got)
	}
}

func TestKellyFraction_Negative(t *testing.T) {
	k := kellyCfg()
	got := KellyFraction(0.3, k, k.Cap)
	if got >= 0 {
		t.Fatalf("expected negative kelly for p=0.3, got %v", got)
	}
}

func TestKellyFraction_InvalidInputs(t *testing.T) {
	k := kellyCfg()
	cases := []float64{math.NaN(), math.Inf(1), math.Inf(-1), 0, 1, -0.1, 1.1}
	for _, p := range cases {
		if got := KellyFraction(p, k, k.Cap); got != 0 {
			t.Errorf("p=%v expected 0, got %v", p, got)
		}
	}
}

func TestKellyFraction_ZeroPriorLoss(t *testing.T) {
	k := kellyCfg()
	k.PriorLossBps = 0
	if got := KellyFraction(0.7, k, k.Cap); got != 0 {
		t.Fatalf("expected 0 for zero PriorLossBps, got %v", got)
	}
}

func TestKellyCapForMode(t *testing.T) {
	k := kellyCfg()
	if got := KellyCapForMode("STRICT", k); got != 0.10 {
		t.Errorf("STRICT cap: got %v want 0.10", got)
	}
	if got := KellyCapForMode("EXPLORATION", k); got != 0.05 {
		t.Errorf("EXPLORATION cap: got %v want 0.05", got)
	}
	if got := KellyCapForMode("BALANCED", k); got != 0.25 {
		t.Errorf("BALANCED cap: got %v want 0.25", got)
	}
	if got := KellyCapForMode("UNKNOWN", k); got != 0.25 {
		t.Errorf("unknown mode falls back to default cap: got %v", got)
	}
}

func TestModeMultiplier(t *testing.T) {
	cfg := &config.CapitalConfig{
		ModeMultipliers: map[string]float64{"STRICT": 0.5, "BALANCED": 1.0, "EXPLORATION": 1.3},
	}
	if v, fb := ModeMultiplier("STRICT", cfg); v != 0.5 || fb {
		t.Errorf("STRICT: got %v fb=%v", v, fb)
	}
	if v, fb := ModeMultiplier("UNKNOWN", cfg); v != 1.0 || !fb {
		t.Errorf("UNKNOWN should fall back: got %v fb=%v", v, fb)
	}
	if v, fb := ModeMultiplier("STRICT", nil); v != 1.0 || !fb {
		t.Errorf("nil cfg should fall back: got %v fb=%v", v, fb)
	}
}

func TestCohortMultiplier(t *testing.T) {
	cfg := &config.CapitalConfig{
		Cohort: config.CapitalCohortConfig{DefaultMultiplier: 1.0, MinMultiplier: 0.5, MaxMultiplier: 2.0},
	}
	if got := CohortMultiplier("anything", cfg); got != 1.0 {
		t.Errorf("got %v want 1.0", got)
	}
	cfg.Cohort.DefaultMultiplier = 5.0 // > Max
	if got := CohortMultiplier("x", cfg); got != 2.0 {
		t.Errorf("clamp to max: got %v want 2.0", got)
	}
	cfg.Cohort.DefaultMultiplier = 0.1 // < Min
	if got := CohortMultiplier("x", cfg); got != 0.5 {
		t.Errorf("clamp to min: got %v want 0.5", got)
	}
}

func TestExplorationBand(t *testing.T) {
	cfg := &config.CapitalConfig{
		Exploration: config.CapitalExplorationConfig{
			Enabled: true, MinPctOfTotal: 0.01, MaxPctOfTotal: 0.05,
		},
	}
	// 1000 portfolio: floor=10, ceil=50.
	if got := ExplorationBand(5, "EXPLORATION", 1000, cfg); got != 10 {
		t.Errorf("below floor: got %v want 10", got)
	}
	if got := ExplorationBand(80, "EXPLORATION", 1000, cfg); got != 50 {
		t.Errorf("above ceil: got %v want 50", got)
	}
	if got := ExplorationBand(20, "EXPLORATION", 1000, cfg); got != 20 {
		t.Errorf("in band: got %v want 20", got)
	}
	if got := ExplorationBand(20, "BALANCED", 1000, cfg); got != 20 {
		t.Errorf("non-EXPLORATION mode passthrough: got %v", got)
	}
	cfg.Exploration.Enabled = false
	if got := ExplorationBand(5, "EXPLORATION", 1000, cfg); got != 5 {
		t.Errorf("disabled passthrough: got %v", got)
	}
}
