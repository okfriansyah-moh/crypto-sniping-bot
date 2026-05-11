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

	// ModeProfiles maps the active operational mode (STRICT / BALANCED /
	// EXPLORATION / VERY_EXPLORATION) onto the threshold band that turns
	// RiskScore into a Decision. Keys MUST be lower-case ("strict",
	// "balanced", "exploration", "very_exploration"); the module up-cases
	// the runtime mode before lookup.
	ModeProfiles map[string]DataQualityModeProfile `yaml:"mode_profiles"`
	// Providers holds configuration for the optional external provider layer
	// (P1 — rugcheck.xyz, social gate, LP lock verification).
	// Disabled by default; all providers boot in shadow_mode: true.
	Providers DataQualityProvidersConfig `yaml:"providers"`
}

// DataQualityModeProfile is the per-mode decision band (Layer 1 fix).
// Applied to the aggregated RiskScore by the Data Quality Engine.
type DataQualityModeProfile struct {
	// RejectAbove: RiskScore ≥ this → Decision = REJECT.
	RejectAbove float64 `yaml:"reject_above"`
	// RiskyPassAbove: RiskScore ≥ this (but below RejectAbove) → Decision = RISKY_PASS.
	RiskyPassAbove float64 `yaml:"risky_pass_above"`
	// UnknownFactor: how a `dq_unknown_<detector>` flag contributes when its
	// upstream input is missing. Multiplied by the detector weight.
	// Canonical: STRICT=0.5 (treat unknown as half-risk), BALANCED=0
	// (neutral), EXPLORATION=0 (ignore).
	UnknownFactor float64 `yaml:"unknown_factor"`
	// MinTokenAgeSeconds overrides the global thresholds.min_token_age_seconds
	// for this specific mode. 0 means "use the global threshold". -1 means
	// "disable the age check entirely for this mode". Positive values set a
	// mode-specific minimum age in seconds.
	// EXPLORATION / VERY_EXPLORATION set this to 0 (disable) because
	// new-launch token sniping targets tokens at the moment of creation.
	MinTokenAgeSeconds int32 `yaml:"min_token_age_seconds"`
}

// DataQualityDetectorFlags toggles individual detectors at runtime.
type DataQualityDetectorFlags struct {
	HoneypotSimulation bool `yaml:"honeypot_simulation"`
	TaxAnomaly         bool `yaml:"tax_anomaly"`
	LpLock             bool `yaml:"lp_lock"`
	WashTrading        bool `yaml:"wash_trading"`
	RugAuthority       bool `yaml:"rug_authority"`
	ContractVerified   bool `yaml:"contract_verified"`
	// DevReputation enables the serial-launcher + no-social-links detector.
	// Requires the solana_creator_reputation probe to populate
	// CreatorPrevTokenCount / SocialLinksKnown on the MarketDataDTO.
	DevReputation bool `yaml:"dev_reputation"`
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

	// MinLiquidityUsd overrides the default thin-liquidity floor used by
	// DetectFakeLiquidity. 0 → module uses built-in default (5000 USD).
	// Set below the market's protocol-defined minimum reserve to avoid
	// false LOW_LIQUIDITY flags (e.g. 3000 for PumpFun 30 SOL virtual reserve).
	MinLiquidityUsd float64 `yaml:"min_liquidity_usd"`

	// MaxTotalSupply rejects tokens whose total supply exceeds this threshold.
	// High supply destroys scarcity — tokens with 1B+ supply are disproportionately
	// likely to be micro-cap rugs with no real value. 0 disables the check.
	// Recommended: 1_000_000_000 (1B).
	MaxTotalSupply float64 `yaml:"max_total_supply"`

	// Dev reputation thresholds (requires dev_reputation detector enabled).
	//
	// MaxCreatorPrevTokenCount — reject when the creator wallet has launched
	// this many or more tokens previously. 0 disables. Canonical default: 5.
	// The $RIBBIT pattern (29 launches, 0 migrations) exceeds this by 5×.
	//
	// NoSocialLinksRiskScore — fixed risk contribution [0,1] applied when
	// SocialLinksKnown=true and HasSocialLinks=false. 0 disables.
	// Canonical default: 0.40 (meaningful contribution without hard-reject).
	MaxCreatorPrevTokenCount int32   `yaml:"max_creator_prev_token_count"`
	NoSocialLinksRiskScore   float64 `yaml:"no_social_links_risk_score"`
	// RejectNoSocialLinks, when true, treats missing social links as a
	// structural hard-reject rather than a risk-score contribution. Applies
	// only when SocialLinksKnown=true AND HasSocialLinks=false (i.e. the
	// metadata probe ran and confirmed there are no social URLs). When
	// SocialLinksKnown=false (metadata probe not run or failed) this flag
	// is NOT triggered — the scoring path's DEV_UNKNOWN_HISTORY handles that.
	RejectNoSocialLinks bool `yaml:"reject_no_social_links"`
	// RejectUnknownTotalSupply, when true, treats TotalSupplyKnown=false as a
	// structural hard-reject when MaxTotalSupply > 0. This closes the LP-probe
	// failure gap: if the probe is unhealthy the token is rejected rather than
	// silently passed with an unknown supply. Only set this false on chains
	// where supply is legitimately not fetchable (set MaxTotalSupply=0 instead
	// to fully disable the supply check for that chain).
	RejectUnknownTotalSupply bool `yaml:"reject_unknown_total_supply"`

	// MinTokenAgeSeconds — hard-reject tokens younger than this threshold.
	// Tokens under this age have incomplete data: holder distribution is not
	// settled, wash patterns are not yet visible, and metadata probes may not
	// have propagated. The rescan pipeline re-evaluates the token once it is
	// old enough. Age is measured from BlockTimestamp (on-chain creation),
	// falling back to IngestedAt. 0 disables. Canonical default: 900 (15 min).
	MinTokenAgeSeconds int32 `yaml:"min_token_age_seconds"`
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
	// FakeLiquidity weight (Layer 1 fix). Optional in YAML; when 0 the
	// default 0.20 contribution from defaultRiskWeights is used.
	FakeLiquidity float64 `yaml:"fake_liquidity"`
	// DevReputation weight. When 0 defaults to 0.25 inside the aggregator.
	DevReputation float64 `yaml:"dev_reputation"`
}

// DataQualityFailurePolicyConfig governs RPC-failure handling.
type DataQualityFailurePolicyConfig struct {
	IndeterminateAsPositive bool `yaml:"indeterminate_as_positive"`
	MaxIndeterminateCount   int  `yaml:"max_indeterminate_count"`
}

// ── External providers (P1 — rugcheck / social / LP lock) ─────────────────

// DataQualityProvidersConfig controls the external provider layer.
// Providers are OPTIONAL — they enhance (not replace) the internal detectors.
// All providers boot in shadow_mode: true by default; flip to false only after
// shadow validation shows no false-positive regression.
type DataQualityProvidersConfig struct {
	// Enabled gates the entire external provider subsystem.
	// false → all providers are skipped (zero cost, 100% backward compat).
	Enabled bool `yaml:"enabled"`

	// ShadowMode: true → external scores are recorded in ProviderFlags on
	// the DataQualityDTO but do NOT blend into the final RiskScore.
	// Use this during shadow validation before activating production impact.
	ShadowMode bool `yaml:"shadow_mode"`

	// ExternalWeight is the fraction [0, 1] of the final RiskScore that comes
	// from the external providers when shadow_mode is false.
	// final_score = (1 - external_weight) * internal_score + external_weight * external_score
	// Recommended starting value: 0.20 (external providers have 20% influence).
	ExternalWeight float64 `yaml:"external_weight"`

	// BudgetMs is the shared wall-clock deadline for all providers in one call.
	// Individual providers must respect the context deadline. Default: 300.
	BudgetMs int `yaml:"budget_ms"`

	// RugCheck configures the rugcheck.xyz provider (Solana-only, free API).
	RugCheck DataQualityRugCheckConfig `yaml:"rugcheck"`

	// SocialGate configures the Twitter/X social gate via DEXScreener (P2).
	SocialGate DataQualitySocialGateConfig `yaml:"social_gate"`

	// BirdEye configures the BirdEye token security provider (P3).
	// API key is read from env BIRDEYE_API_KEY — never stored in YAML.
	BirdEye DataQualityBirdEyeConfig `yaml:"birdeye"`

	// CopyTrade configures the copy-trading signal amplifier (P8).
	// Wallet list is read from env COPY_TRADE_WALLETS — never stored in YAML.
	CopyTrade DataQualityCopyTradeConfig `yaml:"copy_trade"`
}

// DataQualityRugCheckConfig controls the rugcheck.xyz provider.
type DataQualityRugCheckConfig struct {
	// Enabled enables the rugcheck.xyz provider within the aggregator.
	Enabled bool `yaml:"enabled"`

	// ShadowMode overrides the parent ShadowMode for this specific provider.
	// If true, this provider's score only appears in ProviderFlags.
	ShadowMode bool `yaml:"shadow_mode"`

	// Weight is the relative contribution of this provider within the aggregator.
	// The aggregator normalises all non-shadow provider weights to sum to 1.
	Weight float64 `yaml:"weight"`
}

// DataQualitySocialGateConfig controls the Twitter/X social gate (P2).
// Uses the DEXScreener public API — no API key required.
type DataQualitySocialGateConfig struct {
	// Enabled enables the social gate provider within the aggregator.
	Enabled bool `yaml:"enabled"`

	// ShadowMode: if true, the score appears in ProviderFlags only.
	ShadowMode bool `yaml:"shadow_mode"`

	// Weight is the relative contribution within the aggregator (normalised).
	Weight float64 `yaml:"weight"`
}

// DataQualityBirdEyeConfig controls the BirdEye token security provider (P3).
// The BirdEye API key is read from the environment variable BIRDEYE_API_KEY.
// It is never stored in YAML or logged.
type DataQualityBirdEyeConfig struct {
	// Enabled enables the BirdEye provider within the aggregator.
	Enabled bool `yaml:"enabled"`

	// ShadowMode: if true, the score appears in ProviderFlags only.
	ShadowMode bool `yaml:"shadow_mode"`

	// Weight is the relative contribution within the aggregator (normalised).
	Weight float64 `yaml:"weight"`
}

// DataQualityCopyTradeConfig controls the copy-trading signal amplifier (P8).
// A comma-separated list of alpha-wallet addresses is read from the
// COPY_TRADE_WALLETS environment variable — never stored in YAML or logged.
type DataQualityCopyTradeConfig struct {
	// Enabled enables the copy-trade provider within the aggregator.
	Enabled bool `yaml:"enabled"`

	// ShadowMode: if true, the score appears in ProviderFlags only.
	ShadowMode bool `yaml:"shadow_mode"`

	// Weight is the relative contribution within the aggregator (normalised).
	Weight float64 `yaml:"weight"`
}
