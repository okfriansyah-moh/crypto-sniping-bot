// Phase 9 (Profitability Restoration) — runtime config structs.
// These mirror config/data_quality.yaml, config/feature.yaml, and
// config/probability.yaml so values flow into modules per § 0.5
// ("no hardcoded magic numbers").
package config

// DataQualityRuntimeConfig mirrors config/data_quality.yaml.
// Used by the Layer 1 detector aggregator and per-detector cache.
// All values must be sourced from YAML — no module-side defaults.
type DataQualityRuntimeConfig struct {
	DetectorTimeoutMs int                            `yaml:"detector_timeout_ms"`
	TotalBudgetMs     int                            `yaml:"total_budget_ms"`
	MaxInflight       int                            `yaml:"max_inflight_detectors"`
	PassThreshold     float64                        `yaml:"pass_threshold"`
	RejectThreshold   float64                        `yaml:"reject_threshold"`
	Detectors         DataQualityDetectorFlags       `yaml:"detectors"`
	Thresholds        DataQualityDetectorThresholds  `yaml:"thresholds"`
	Cache             DataQualityCacheConfig         `yaml:"cache"`
	RiskWeights       DataQualityRiskWeights         `yaml:"risk_weights"`
	FailurePolicy     DataQualityFailurePolicyConfig `yaml:"failure_policy"`
}

// DataQualityDetectorFlags toggles individual detectors at runtime.
type DataQualityDetectorFlags struct {
	HoneypotSimulation bool `yaml:"honeypot_simulation"`
	TaxAnomaly         bool `yaml:"tax_anomaly"`
	LpLock             bool `yaml:"lp_lock"`
	WashTrading        bool `yaml:"wash_trading"`
	RugAuthority       bool `yaml:"rug_authority"`
	ContractVerified   bool `yaml:"contract_verified"`
}

// DataQualityDetectorThresholds gates per-detector verdict math.
type DataQualityDetectorThresholds struct {
	HoneypotRatioDeviationMax float64 `yaml:"honeypot_ratio_deviation_max"`
	TaxTotalMaxBps            int32   `yaml:"tax_total_max_bps"`
	TaxBuyMaxBps              int32   `yaml:"tax_buy_max_bps"`
	TaxSellMaxBps             int32   `yaml:"tax_sell_max_bps"`
	WashUniqueRatioMin        float64 `yaml:"wash_unique_ratio_min"`
	WashRecentSwapsWindow     int     `yaml:"wash_recent_swaps_window"`
	LpLockRequired            bool    `yaml:"lp_lock_required"`
	LpLockMinDays             int     `yaml:"lp_lock_min_days"`
}

// DataQualityCacheConfig bounds per-detector cache footprints.
type DataQualityCacheConfig struct {
	MaxEntries             int `yaml:"max_entries"`
	HoneypotTTLSec         int `yaml:"honeypot_ttl_sec"`
	TaxAnomalyTTLSec       int `yaml:"tax_anomaly_ttl_sec"`
	LpLockTTLSec           int `yaml:"lp_lock_ttl_sec"`
	WashTradingTTLSec      int `yaml:"wash_trading_ttl_sec"`
	RugAuthorityTTLSec     int `yaml:"rug_authority_ttl_sec"`
	ContractVerifiedTTLSec int `yaml:"contract_verified_ttl_sec"`
}

// DataQualityRiskWeights aggregates detector verdicts into RiskScore.
type DataQualityRiskWeights struct {
	Honeypot           float64 `yaml:"honeypot"`
	TaxAnomaly         float64 `yaml:"tax_anomaly"`
	RugAuthority       float64 `yaml:"rug_authority"`
	LpLockMissing      float64 `yaml:"lp_lock_missing"`
	WashTrading        float64 `yaml:"wash_trading"`
	ContractUnverified float64 `yaml:"contract_unverified"`
}

// DataQualityFailurePolicyConfig governs RPC-failure handling.
type DataQualityFailurePolicyConfig struct {
	IndeterminateAsPositive bool `yaml:"indeterminate_as_positive"`
	MaxIndeterminateCount   int  `yaml:"max_indeterminate_count"`
}

// FeatureRuntimeConfig mirrors config/feature.yaml.
type FeatureRuntimeConfig struct {
	FeatureTimeoutMs   int                              `yaml:"feature_timeout_ms"`
	TotalBudgetMs      int                              `yaml:"total_budget_ms"`
	SyncCache          FeatureSyncCacheConfig           `yaml:"sync_cache"`
	TxVelocity         FeatureTxVelocityConfig          `yaml:"tx_velocity"`
	WalletEntropy      FeatureWalletEntropyConfig       `yaml:"wallet_entropy"`
	TokenAge           FeatureTokenAgeConfig            `yaml:"token_age"`
	VolumeMomentum     FeatureMomentumConfig            `yaml:"volume_momentum"`
	PriceMomentum      FeatureMomentumConfig            `yaml:"price_momentum"`
	LiquidityUsd       FeatureLiquidityUsdConfig        `yaml:"liquidity_usd"`
	EthGetLogs         FeatureEthGetLogsConfig          `yaml:"eth_getlogs"`
	Normalization      FeatureNormalizationConfig       `yaml:"normalization"`
	ConfidenceAggregate FeatureConfidenceAggregateConfig `yaml:"confidence_aggregate"`
}

// FeatureSyncCacheConfig bounds the per-pool Sync-event ring buffer.
type FeatureSyncCacheConfig struct {
	SizePerPool       int    `yaml:"size_per_pool"`
	EvictionStrategy  string `yaml:"eviction_strategy"`
	RefreshOnNewSwap  bool   `yaml:"refresh_on_new_swap"`
}

// FeatureTxVelocityConfig drives the swap-rate normalization.
type FeatureTxVelocityConfig struct {
	WindowSec               int     `yaml:"window_sec"`
	SwapCountNormalizeHigh  int     `yaml:"swap_count_normalize_high"`
	SwapCountNormalizeLow   int     `yaml:"swap_count_normalize_low"`
	ConfidenceTarget        float64 `yaml:"confidence_target"`
}

// FeatureWalletEntropyConfig drives unique-sender ratio scoring.
type FeatureWalletEntropyConfig struct {
	RecentSwapsWindow    int     `yaml:"recent_swaps_window"`
	ConfidenceTarget     float64 `yaml:"confidence_target"`
	ColdStartDefault     float64 `yaml:"cold_start_default"`
	ColdStartConfidence  float64 `yaml:"cold_start_confidence"`
}

// FeatureTokenAgeConfig drives the piecewise age scoring.
type FeatureTokenAgeConfig struct {
	TooNewThresholdSec int     `yaml:"too_new_threshold_sec"`
	SweetSpotMinSec    int     `yaml:"sweet_spot_min_sec"`
	SweetSpotMaxSec    int     `yaml:"sweet_spot_max_sec"`
	ConfidenceTarget   float64 `yaml:"confidence_target"`
}

// FeatureMomentumConfig is shared by volume_momentum and price_momentum blocks.
type FeatureMomentumConfig struct {
	ShortWindowSec      int     `yaml:"short_window_sec"`
	LongWindowSec       int     `yaml:"long_window_sec"`
	SyncEventPairMin    int     `yaml:"sync_event_pair_min"`
	ColdStartDefault    float64 `yaml:"cold_start_default"`
	ColdStartConfidence float64 `yaml:"cold_start_confidence"`
	ConfidenceTarget    float64 `yaml:"confidence_target"`
}

// FeatureLiquidityUsdConfig governs native-price freshness for USD conversion.
type FeatureLiquidityUsdConfig struct {
	NativePriceTTLSec      int     `yaml:"native_price_ttl_sec"`
	NativePriceMaxStaleSec int     `yaml:"native_price_max_stale_sec"`
	ConfidenceTarget       float64 `yaml:"confidence_target"`
	OnStaleDefault         float64 `yaml:"on_stale_default"`
	OnStaleConfidence      float64 `yaml:"on_stale_confidence"`
}

// FeatureEthGetLogsConfig caps EVM log walk-back distance.
type FeatureEthGetLogsConfig struct {
	ChunkBlocks   int `yaml:"chunk_blocks"`
	MaxWalkBlocks int `yaml:"max_walk_blocks"`
}

// FeatureNormalizationConfig configures the signal-normalizer pipeline.
type FeatureNormalizationConfig struct {
	UseZScore        bool    `yaml:"use_zscore"`
	UseSigmoid       bool    `yaml:"use_sigmoid"`
	SigmoidSteepness float64 `yaml:"sigmoid_steepness"`
	ZScoreMinSamples int     `yaml:"zscore_min_samples"`
}

// FeatureConfidenceAggregateConfig configures per-feature aggregation.
type FeatureConfidenceAggregateConfig struct {
	Method               string `yaml:"method"`
	MinFeaturesRequired  int    `yaml:"min_features_required"`
}

// ProbabilityRuntimeConfig mirrors config/probability.yaml. Note: the
// existing top-level ModelsConfig.Probability holds *coefficients*; this
// struct holds *EV-gate consumption rules* and is bound to the top-level
// `probability:` YAML key (additive — no legacy key collision).
type ProbabilityRuntimeConfig struct {
	UseModelOutput          bool    `yaml:"use_model_output"`
	PriorProbability        float64 `yaml:"prior_probability"`
	MinModelConfidence      float64 `yaml:"min_model_confidence"`
	ProbJoinTimeoutMs       int     `yaml:"prob_join_timeout_ms"`
	RejectOutOfRange        bool    `yaml:"reject_out_of_range"`
	RejectNanOrInf          bool    `yaml:"reject_nan_or_inf"`
	CalibrationWindowTrades int     `yaml:"calibration_window_trades"`
	BrierMax                float64 `yaml:"brier_max"`
}

// CapitalKellyConfig is the Kelly-fraction sub-block of CapitalConfig.
type CapitalKellyConfig struct {
	Cap             float64 `yaml:"cap"`
	CapExploration  float64 `yaml:"cap_exploration"`
	CapStrict       float64 `yaml:"cap_strict"`
	PriorGainBps    int32   `yaml:"prior_gain_bps"`
	PriorLossBps    int32   `yaml:"prior_loss_bps"`
	RejectNegative  bool    `yaml:"reject_negative"`
}

// CapitalCohortConfig governs cohort multiplier lookup defaults.
type CapitalCohortConfig struct {
	DefaultMultiplier float64 `yaml:"default_multiplier"`
	MinMultiplier     float64 `yaml:"min_multiplier"`
	MaxMultiplier     float64 `yaml:"max_multiplier"`
}

// CapitalExplorationConfig governs the bounded exploration band.
type CapitalExplorationConfig struct {
	Enabled         bool    `yaml:"enabled"`
	MinPctOfTotal   float64 `yaml:"min_pct_of_total"`
	MaxPctOfTotal   float64 `yaml:"max_pct_of_total"`
	DailyBudgetPct  float64 `yaml:"daily_budget_pct"`
}

// CapitalFailurePolicyConfig governs deterministic capital-engine failure handling.
type CapitalFailurePolicyConfig struct {
	OnMissingProbability string `yaml:"on_missing_probability"` // reject | fallback_prior
	OnCohortLookupMiss   string `yaml:"on_cohort_lookup_miss"`  // use_default | reject
	OnModeLookupStale    string `yaml:"on_mode_lookup_stale"`   // fallback_balanced | reject
}
