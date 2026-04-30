// Capital Engine (Layer 7) — advanced runtime config structs.
// These mirror config/capital.yaml sub-blocks and are referenced by
// CapitalConfig (config.go) for dynamic Kelly-fraction sizing,
// cohort multipliers, exploration budget, and failure policy.
package config

// CapitalKellyConfig is the Kelly-fraction sub-block of CapitalConfig.
type CapitalKellyConfig struct {
	Cap            float64 `yaml:"cap"`
	CapExploration float64 `yaml:"cap_exploration"`
	CapStrict      float64 `yaml:"cap_strict"`
	PriorGainBps   int32   `yaml:"prior_gain_bps"`
	PriorLossBps   int32   `yaml:"prior_loss_bps"`
	RejectNegative bool    `yaml:"reject_negative"`
}

// CapitalCohortConfig governs cohort multiplier lookup defaults.
type CapitalCohortConfig struct {
	DefaultMultiplier float64 `yaml:"default_multiplier"`
	MinMultiplier     float64 `yaml:"min_multiplier"`
	MaxMultiplier     float64 `yaml:"max_multiplier"`
}

// CapitalExplorationConfig governs the bounded exploration band.
type CapitalExplorationConfig struct {
	Enabled        bool    `yaml:"enabled"`
	MinPctOfTotal  float64 `yaml:"min_pct_of_total"`
	MaxPctOfTotal  float64 `yaml:"max_pct_of_total"`
	DailyBudgetPct float64 `yaml:"daily_budget_pct"`
}

// CapitalFailurePolicyConfig governs deterministic capital-engine failure handling.
type CapitalFailurePolicyConfig struct {
	OnMissingProbability string `yaml:"on_missing_probability"` // reject | fallback_prior
	OnCohortLookupMiss   string `yaml:"on_cohort_lookup_miss"`  // use_default | reject
	OnModeLookupStale    string `yaml:"on_mode_lookup_stale"`   // fallback_balanced | reject
	// FallbackPriorProbability is the probability used when prob is nil and
	// OnMissingProbability="fallback_prior". 0 means "unset" — module code
	// must reject in that case rather than silently picking a hardcoded value.
	FallbackPriorProbability float64 `yaml:"fallback_prior_probability"`
}
