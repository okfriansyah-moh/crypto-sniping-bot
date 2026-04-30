// Probability Models (Layer 4) — runtime config structs.
// These mirror config/probability.yaml so values flow into the
// probability gate per § 0.5 ("no hardcoded magic numbers").
//
// Note: ModelsConfig.Probability (in config.go) holds model *coefficients*;
// this struct holds *EV-gate consumption rules* and is bound to the
// top-level `probability:` YAML key (additive — no legacy key collision).
package config

// ProbabilityRuntimeConfig mirrors config/probability.yaml.
type ProbabilityRuntimeConfig struct {
	UseModelOutput          bool    `yaml:"use_model_output"`
	PriorProbability        float64 `yaml:"prior_probability"`
	MinModelConfidence      float64 `yaml:"min_model_confidence"`
	ProbJoinTimeoutMs       int     `yaml:"prob_join_timeout_ms"`
	RejectOutOfRange        bool    `yaml:"reject_out_of_range"`
	RejectNanOrInf          bool    `yaml:"reject_nan_or_inf"`
	CalibrationWindowTrades int     `yaml:"calibration_window_trades"`
	BrierMax                float64 `yaml:"brier_max"`
	// Fallback observability — alert when validations fall back beyond the
	// configured fraction within the window. Mirrors config/probability.yaml.
	FallbackAlertPct       float64 `yaml:"fallback_alert_pct"`
	FallbackAlertWindowSec int     `yaml:"fallback_alert_window_sec"`
}
