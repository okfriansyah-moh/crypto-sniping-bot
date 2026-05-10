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
	Enabled           bool                         `yaml:"enabled"`
	IntervalSeconds   int                          `yaml:"interval_seconds"`
	MaxPerBandPerTick int                          `yaml:"max_per_band_per_tick"`
	SkipOpenPositions bool                         `yaml:"skip_open_positions"`
	Eligibility       RescanEligibility            `yaml:"eligibility"`
	Bands             []RescanBand                 `yaml:"bands"`
	ModeOverrides     map[string]RescanEligibility `yaml:"mode_overrides"`
}

// RescanEligibility defines the DQ sub-score thresholds that a token must
// satisfy to be eligible for a rescan band. Tokens exceeding any threshold
// are permanently excluded, regardless of mode.
//
// Threshold fields use pointers to distinguish "not configured" (nil → use
// default) from an intentional strict-zero threshold (pointer to 0).
type RescanEligibility struct {
	MaxHoneypotScore *float64 `yaml:"max_honeypot_score"` // [0.0, 1.0]; nil = use default 0.5
	MaxRugScore      *float64 `yaml:"max_rug_score"`      // [0.0, 1.0]; nil = use default 0.65
	MaxBuyTaxBps     *int32   `yaml:"max_buy_tax_bps"`    // [0, 10000]; nil = use default 3000
	IncludePassed    bool     `yaml:"include_passed"`     // also rescan PASS/RISKY_PASS tokens
}

// RescanBand defines one age window for rescan eligibility.
// Bands must be sorted by MinAgeSeconds ascending (validated in Validate).
type RescanBand struct {
	Name          string `yaml:"name"` // "15m", "30m", "45m", "1h"
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
	// SkipOpenPositions is a CAPITAL SAFETY enforced invariant — always true.
	// This is not an operator-configurable toggle; it prevents double-entry
	// into open positions. Setting `skip_open_positions: false` in YAML has
	// no effect. See docs/RESCAN_PLAN.md § 3 (capital protection).
	if !r.SkipOpenPositions {
		r.SkipOpenPositions = true
	}

	if r.Eligibility.MaxHoneypotScore == nil {
		v := 0.5
		r.Eligibility.MaxHoneypotScore = &v
	}
	if r.Eligibility.MaxRugScore == nil {
		v := 0.65
		r.Eligibility.MaxRugScore = &v
	}
	if r.Eligibility.MaxBuyTaxBps == nil {
		v := int32(3000)
		r.Eligibility.MaxBuyTaxBps = &v
	}
	// IncludePassed is an enforced default (always true). This ensures the
	// rescan profit hypothesis works out of the box: re-emit PASS/RISKY_PASS
	// tokens whose features matured past their original EV threshold.
	// This field is always forced to true regardless of YAML; it is not
	// a configurable toggle. Mode-specific overrides control per-mode behaviour.
	if !r.Eligibility.IncludePassed {
		r.Eligibility.IncludePassed = true
	}

	if len(r.Bands) == 0 {
		// 14-band design: Phase 1 (early dense, 0–8h) + Phase 2 (recovery, 12–48h).
		// See docs/RESCAN_PLAN.md § Band Design Rationale for data-driven justification.
		r.Bands = []RescanBand{
			// Phase 1 — Early dense (Goal A: catch organic momentum, 0–8h)
			{Name: "15m", MinAgeSeconds: 900, MaxAgeSeconds: 1800, Priority: 80},
			{Name: "30m", MinAgeSeconds: 1800, MaxAgeSeconds: 2700, Priority: 60},
			{Name: "45m", MinAgeSeconds: 2700, MaxAgeSeconds: 3600, Priority: 40},
			{Name: "1h", MinAgeSeconds: 3600, MaxAgeSeconds: 5400, Priority: 30},
			{Name: "1.5h", MinAgeSeconds: 5400, MaxAgeSeconds: 7200, Priority: 28},
			{Name: "2h", MinAgeSeconds: 7200, MaxAgeSeconds: 10800, Priority: 26},
			{Name: "3h", MinAgeSeconds: 10800, MaxAgeSeconds: 14400, Priority: 24},
			{Name: "4h", MinAgeSeconds: 14400, MaxAgeSeconds: 21600, Priority: 22},
			{Name: "6h", MinAgeSeconds: 21600, MaxAgeSeconds: 28800, Priority: 20},
			{Name: "8h", MinAgeSeconds: 28800, MaxAgeSeconds: 43200, Priority: 18},
			// Phase 2 — Recovery checkpoints (Goal B+C: stalled position reversal + CEX catalyst, 12–48h)
			{Name: "12h", MinAgeSeconds: 43200, MaxAgeSeconds: 86400, Priority: 16},
			{Name: "24h", MinAgeSeconds: 86400, MaxAgeSeconds: 129600, Priority: 14},
			{Name: "36h", MinAgeSeconds: 129600, MaxAgeSeconds: 172800, Priority: 12},
			{Name: "48h", MinAgeSeconds: 172800, MaxAgeSeconds: 201600, Priority: 10},
		}
	}

	if r.ModeOverrides == nil {
		r.ModeOverrides = map[string]RescanEligibility{
			"STRICT": {
				MaxHoneypotScore: float64Ptr(0.30),
				MaxRugScore:      float64Ptr(0.50),
				MaxBuyTaxBps:     int32Ptr(1500),
				IncludePassed:    false,
			},
			"BALANCED": {
				MaxHoneypotScore: float64Ptr(0.5),
				MaxRugScore:      float64Ptr(0.65),
				MaxBuyTaxBps:     int32Ptr(3000),
				IncludePassed:    true,
			},
			"EXPLORATION": {
				MaxHoneypotScore: float64Ptr(0.60),
				MaxRugScore:      float64Ptr(0.75),
				MaxBuyTaxBps:     int32Ptr(4500),
				IncludePassed:    true,
			},
			"VERY_EXPLORATION": {
				MaxHoneypotScore: float64Ptr(0.75),
				MaxRugScore:      float64Ptr(0.85),
				MaxBuyTaxBps:     int32Ptr(6000),
				IncludePassed:    true,
			},
		}
	}
}

// float64Ptr returns a pointer to v. Used to express optional float64 config
// thresholds where nil means "use default" and &0.0 means "strict zero".
func float64Ptr(v float64) *float64 { return &v }

// int32Ptr returns a pointer to v. Used to express optional int32 config
// thresholds where nil means "use default" and &0 means "strict zero".
func int32Ptr(v int32) *int32 { return &v }

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
	if r.MaxPerBandPerTick > 10000 {
		return fmt.Errorf("rescan.max_per_band_per_tick must be <= 10000 (DoS guard), got %d", r.MaxPerBandPerTick)
	}
	for i, b := range r.Bands {
		if b.MinAgeSeconds >= b.MaxAgeSeconds {
			return fmt.Errorf("rescan.bands[%d] (%s): min_age_seconds (%d) must be < max_age_seconds (%d)",
				i, b.Name, b.MinAgeSeconds, b.MaxAgeSeconds)
		}
	}
	// Bands must be sorted ascending by min_age, with no duplicate min_age values
	// (deterministic ordering; ties would cause both bands to fire on the same token).
	for i := 1; i < len(r.Bands); i++ {
		if r.Bands[i].MinAgeSeconds <= r.Bands[i-1].MinAgeSeconds {
			return fmt.Errorf("rescan.bands must be sorted by min_age_seconds ascending with no duplicate min_age (band %d <= band %d)",
				i, i-1)
		}
	}
	if r.Eligibility.MaxHoneypotScore != nil && (*r.Eligibility.MaxHoneypotScore < 0 || *r.Eligibility.MaxHoneypotScore > 1.0) {
		return fmt.Errorf("rescan.eligibility.max_honeypot_score must be in [0.0, 1.0], got %f",
			*r.Eligibility.MaxHoneypotScore)
	}
	if r.Eligibility.MaxRugScore != nil && (*r.Eligibility.MaxRugScore < 0 || *r.Eligibility.MaxRugScore > 1.0) {
		return fmt.Errorf("rescan.eligibility.max_rug_score must be in [0.0, 1.0], got %f",
			*r.Eligibility.MaxRugScore)
	}
	if r.Eligibility.MaxBuyTaxBps != nil && (*r.Eligibility.MaxBuyTaxBps < 0 || *r.Eligibility.MaxBuyTaxBps > 10000) {
		return fmt.Errorf("rescan.eligibility.max_buy_tax_bps must be in [0, 10000], got %d",
			*r.Eligibility.MaxBuyTaxBps)
	}
	validModes := map[string]bool{"STRICT": true, "BALANCED": true, "EXPLORATION": true, "VERY_EXPLORATION": true}
	for k, override := range r.ModeOverrides {
		if !validModes[k] {
			return fmt.Errorf("rescan.mode_overrides key %q is not valid (allowed: STRICT, BALANCED, EXPLORATION, VERY_EXPLORATION)", k)
		}
		// Validate pointer fields within each mode override to prevent runtime panics.
		if override.MaxHoneypotScore != nil && (*override.MaxHoneypotScore < 0 || *override.MaxHoneypotScore > 1.0) {
			return fmt.Errorf("rescan.mode_overrides[%s].max_honeypot_score must be in [0.0, 1.0], got %f",
				k, *override.MaxHoneypotScore)
		}
		if override.MaxRugScore != nil && (*override.MaxRugScore < 0 || *override.MaxRugScore > 1.0) {
			return fmt.Errorf("rescan.mode_overrides[%s].max_rug_score must be in [0.0, 1.0], got %f",
				k, *override.MaxRugScore)
		}
		if override.MaxBuyTaxBps != nil && (*override.MaxBuyTaxBps < 0 || *override.MaxBuyTaxBps > 10000) {
			return fmt.Errorf("rescan.mode_overrides[%s].max_buy_tax_bps must be in [0, 10000], got %d",
				k, *override.MaxBuyTaxBps)
		}
	}
	return nil
}
