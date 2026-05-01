package config

// ExecutionQualityConfig holds residual-risk #3 (alpha aggregator) settings.
// Loaded from config/pipeline.yaml under `execution_quality:`.
type ExecutionQualityConfig struct {
	Alpha AlphaAggregatorYAML `yaml:"alpha"`
}

// AlphaAggregatorYAML mirrors execution_quality.AlphaAggregatorConfig but
// stays in the config package so modules don't depend on it.
type AlphaAggregatorYAML struct {
	MinSampleCount    int     `yaml:"min_sample_count"`
	AlphaMin          float64 `yaml:"alpha_min"`
	AlphaMax          float64 `yaml:"alpha_max"`
	EwmaHalflifeSec   int     `yaml:"ewma_halflife_sec"`
	UpdateIntervalSec int     `yaml:"update_interval_sec"`
}
