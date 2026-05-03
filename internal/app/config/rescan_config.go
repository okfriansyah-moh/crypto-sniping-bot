package config

import "fmt"

// RescanConfig configures the time-banded rescan worker (Phase 10, Layer 0.5).
// The worker periodically re-emits market_data_event for tokens whose first
// scan was temporally unfavourable but not structurally malicious, allowing
// the MOMENTUM_EDGE path to capture alpha that NEW_LAUNCH_EDGE missed.
//
// Disabled by default — operators must set enabled: true in pipeline.yaml.
// See docs/PLAN.md § Task 1 for full design rationale.
type RescanConfig struct {
	Enabled             bool                        `yaml:"enabled"`
	IntervalSeconds     int                         `yaml:"interval_seconds"`
	MaxPerBandPerTick   int                         `yaml:"max_per_band_per_tick"`
	SkipOpenPositions   bool                        `yaml:"skip_open_positions"`
	Eligibility         RescanEligibility           `yaml:"eligibility"`
	Bands               []RescanBand                `yaml:"bands"`
	ModeOverrides       map[string]RescanEligibility `yaml:"mode_overrides"`
}

// RescanEligibility defines the DQ sub-score thresholds that a token must
// satisfy to be eligible for a rescan band. Tokens exceeding any threshold
// are permanently excluded, regardless of mode.
type RescanEligibility struct {
	MaxHoneypotScore float64 `yaml:"max_honeypot_score"` // [0.0, 1.0]
	MaxRugScore      float64 `yaml:"max_rug_score"`      // [0.0, 1.0]
	MaxBuyTaxBps     int32   `yaml:"max_buy_tax_bps"`    // [0, 10000]
	IncludePassed    bool    `yaml:"include_passed"`     // also rescan PASS/RISKY_PASS tokens
}

// RescanBand defines one age window for rescan eligibility.
// Bands must be sorted by MinAgeSeconds ascending (validated in Validate).
type RescanBand struct {
	Name          string `yaml:"name"`           // "15m", "30m", "45m", "1h"
	MinAgeSeconds int    `yaml:"min_age_seconds"`
	MaxAgeSeconds int    `yaml:"max_age_seconds"`
	Priority      int32  `yaml:"priority"` // event priority; later bands = lower
}

// applyRescanDefaults fills zero-value RescanConfig fields with safe defaults.
// Called during config.Load() before Validate().
func applyRescanDefaults(r *RescanConfig) {
	if r.IntervalSeconds == 0 {
		r.IntervalSeconds = 60
	}
	if r.MaxPerBandPerTick == 0 {
		r.MaxPerBandPerTick = 100
	}
	if !r.Enabled {
		// Only set the SkipOpenPositions default when rescan is disabled so
		// an explicit false in YAML is preserved when enabled.
		if !r.SkipOpenPositions {
			r.SkipOpenPositions = true
		}
	}

	if r.Eligibility.MaxHoneypotScore == 0 {
		r.Eligibility.MaxHoneypotScore = 0.5
	}
	if r.Eligibility.MaxRugScore == 0 {
		r.Eligibility.MaxRugScore = 0.65
	}
	if r.Eligibility.MaxBuyTaxBps == 0 {
		r.Eligibility.MaxBuyTaxBps = 3000
	}
	r.Eligibility.IncludePassed = true

	if len(r.Bands) == 0 {
		r.Bands = []RescanBand{
			{Name: "15m", MinAgeSeconds: 900, MaxAgeSeconds: 1800, Priority: 80},
			{Name: "30m", MinAgeSeconds: 1800, MaxAgeSeconds: 2700, Priority: 60},
			{Name: "45m", MinAgeSeconds: 2700, MaxAgeSeconds: 3600, Priority: 40},
			{Name: "1h", MinAgeSeconds: 3600, MaxAgeSeconds: 7200, Priority: 20},
		}
	}

	if r.ModeOverrides == nil {
		r.ModeOverrides = map[string]RescanEligibility{
			"STRICT": {
				MaxHoneypotScore: 0.30,
				MaxRugScore:      0.50,
				MaxBuyTaxBps:     1500,
				IncludePassed:    false,
			},
			"BALANCED": {
				MaxHoneypotScore: 0.5,
				MaxRugScore:      0.65,
				MaxBuyTaxBps:     3000,
				IncludePassed:    true,
			},
			"EXPLORATION": {
				MaxHoneypotScore: 0.60,
				MaxRugScore:      0.75,
				MaxBuyTaxBps:     4500,
				IncludePassed:    true,
			},
		}
	}
}

// validateRescanConfig enforces structural correctness rules.
// Returns a non-nil error describing the first violation found.
// When Enabled is false the worker is dormant so structural checks are skipped.
func validateRescanConfig(r RescanConfig) error {
	if !r.Enabled {
		return nil
	}
	if r.IntervalSeconds < 10 {
		return fmt.Errorf("rescan.interval_seconds must be >= 10, got %d", r.IntervalSeconds)
	}
	for i, b := range r.Bands {
		if b.MinAgeSeconds >= b.MaxAgeSeconds {
			return fmt.Errorf("rescan.bands[%d] (%s): min_age_seconds (%d) must be < max_age_seconds (%d)",
				i, b.Name, b.MinAgeSeconds, b.MaxAgeSeconds)
		}
	}
	// Bands must be sorted ascending by min_age (deterministic ordering).
	for i := 1; i < len(r.Bands); i++ {
		if r.Bands[i].MinAgeSeconds < r.Bands[i-1].MinAgeSeconds {
			return fmt.Errorf("rescan.bands must be sorted by min_age_seconds ascending (band %d < band %d)",
				i, i-1)
		}
	}
	if r.Eligibility.MaxHoneypotScore < 0 || r.Eligibility.MaxHoneypotScore > 1.0 {
		return fmt.Errorf("rescan.eligibility.max_honeypot_score must be in [0.0, 1.0], got %f",
			r.Eligibility.MaxHoneypotScore)
	}
	if r.Eligibility.MaxBuyTaxBps < 0 || r.Eligibility.MaxBuyTaxBps > 10000 {
		return fmt.Errorf("rescan.eligibility.max_buy_tax_bps must be in [0, 10000], got %d",
			r.Eligibility.MaxBuyTaxBps)
	}
	validModes := map[string]bool{"STRICT": true, "BALANCED": true, "EXPLORATION": true}
	for k := range r.ModeOverrides {
		if !validModes[k] {
			return fmt.Errorf("rescan.mode_overrides key %q is not valid (allowed: STRICT, BALANCED, EXPLORATION)", k)
		}
	}
	return nil
}
