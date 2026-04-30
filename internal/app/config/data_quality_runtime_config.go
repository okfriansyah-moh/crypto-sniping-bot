// Data Quality Engine (Layer 1) — runtime config structs.
// These mirror config/data_quality.yaml so values flow into the
// detector aggregator per § 0.5 ("no hardcoded magic numbers").
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
//
// NOTE: This struct is an *intentional subset* of config/data_quality.yaml
// — only the keys actively consumed by Layer 1 detector code today.
// The YAML file contains additional knobs (e.g. `rug_dangerous_selectors`,
// `solana_require_mint_authority_renounced`,
// `solana_require_freeze_authority_renounced`) that wire to detectors not
// yet implemented. Those keys are silently ignored by yaml.Unmarshal until
// their detectors land — DO NOT remove them from the YAML file.
type DataQualityDetectorThresholds struct {
	HoneypotRatioDeviationMax float64 `yaml:"honeypot_ratio_deviation_max"`
	TaxTotalMaxBps            int32   `yaml:"tax_total_max_bps"`
	TaxBuyMaxBps              int32   `yaml:"tax_buy_max_bps"`
	TaxSellMaxBps             int32   `yaml:"tax_sell_max_bps"`
	WashUniqueRatioMin        float64 `yaml:"wash_unique_ratio_min"`
	WashRecentSwapsWindow     int     `yaml:"wash_recent_swaps_window"`
	LpLockRequired            bool    `yaml:"lp_lock_required"`
	LpLockMinDays             int     `yaml:"lp_lock_min_days"`

	// Phase 10 (Reference-Repo Improvements / Task F) — Solana bonding
	// curve progress filter. Reject MarketDataDTO when
	// BondingCurveProgressBps > MaxBondingCurveProgressBps. 0 disables.
	MaxBondingCurveProgressBps int32 `yaml:"max_bonding_curve_progress_bps"`
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
