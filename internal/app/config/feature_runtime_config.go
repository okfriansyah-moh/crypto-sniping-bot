// Feature Extraction (Layer 2) — runtime config structs.
// These mirror config/feature.yaml so values flow into the feature
// extractor per § 0.5 ("no hardcoded magic numbers").
package config

// FeatureRuntimeConfig mirrors config/feature.yaml.
type FeatureRuntimeConfig struct {
	FeatureTimeoutMs    int                              `yaml:"feature_timeout_ms"`
	TotalBudgetMs       int                              `yaml:"total_budget_ms"`
	SyncCache           FeatureSyncCacheConfig           `yaml:"sync_cache"`
	TxVelocity          FeatureTxVelocityConfig          `yaml:"tx_velocity"`
	WalletEntropy       FeatureWalletEntropyConfig       `yaml:"wallet_entropy"`
	TokenAge            FeatureTokenAgeConfig            `yaml:"token_age"`
	VolumeMomentum      FeatureMomentumConfig            `yaml:"volume_momentum"`
	PriceMomentum       FeatureMomentumConfig            `yaml:"price_momentum"`
	LiquidityUsd        FeatureLiquidityUsdConfig        `yaml:"liquidity_usd"`
	EthGetLogs          FeatureEthGetLogsConfig          `yaml:"eth_getlogs"`
	Normalization       FeatureNormalizationConfig       `yaml:"normalization"`
	ConfidenceAggregate FeatureConfidenceAggregateConfig `yaml:"confidence_aggregate"`

	// Phase 11 (Reference-Repo Improvements R2 — FEATURES) — additive
	// holder-concentration and social-presence extractors. Both
	// disabled by default (Enabled=false) so legacy behaviour is
	// preserved.
	HolderConcentration FeatureHolderConcentrationConfig `yaml:"holder_concentration"`
	SocialLinks         FeatureSocialLinksConfig         `yaml:"social_links"`
}

// FeatureHolderConcentrationConfig drives the top-N holder pct extractor.
type FeatureHolderConcentrationConfig struct {
	Enabled          bool    `yaml:"enabled"`
	TopN             int     `yaml:"top_n"`              // e.g. 10
	MaxAcceptableBps int32   `yaml:"max_acceptable_bps"` // e.g. 5000 (50 %)
	ConfidenceTarget float64 `yaml:"confidence_target"`
}

// FeatureSocialLinksConfig drives the social-presence flag extractor.
type FeatureSocialLinksConfig struct {
	Enabled          bool    `yaml:"enabled"`
	ConfidenceTarget float64 `yaml:"confidence_target"`
}

// FeatureSyncCacheConfig bounds the per-pool Sync-event ring buffer.
type FeatureSyncCacheConfig struct {
	SizePerPool      int    `yaml:"size_per_pool"`
	EvictionStrategy string `yaml:"eviction_strategy"`
	RefreshOnNewSwap bool   `yaml:"refresh_on_new_swap"`
}

// FeatureTxVelocityConfig drives the swap-rate normalization.
type FeatureTxVelocityConfig struct {
	WindowSec              int     `yaml:"window_sec"`
	SwapCountNormalizeHigh int     `yaml:"swap_count_normalize_high"`
	SwapCountNormalizeLow  int     `yaml:"swap_count_normalize_low"`
	ConfidenceTarget       float64 `yaml:"confidence_target"`
}

// FeatureWalletEntropyConfig drives unique-sender ratio scoring.
type FeatureWalletEntropyConfig struct {
	RecentSwapsWindow   int     `yaml:"recent_swaps_window"`
	ConfidenceTarget    float64 `yaml:"confidence_target"`
	ColdStartDefault    float64 `yaml:"cold_start_default"`
	ColdStartConfidence float64 `yaml:"cold_start_confidence"`
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
	Method              string `yaml:"method"`
	MinFeaturesRequired int    `yaml:"min_features_required"`
}
