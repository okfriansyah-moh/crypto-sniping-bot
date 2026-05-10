package contracts

import "time"

// HistoricalMarketProfileDTO holds cohort-level statistical priors derived
// from historical token data (seed population: 100 representative tokens
// across 10 tiers). Produced by the `hydrate` CLI command and persisted in
// the historical_market_profiles table.
//
// Consumers:
//   - Layer 1 (DQ):          liquidity_min_usd — per-cohort liquidity floor
//   - Layer 4 (Probability): prior_probability, ath_multiple_p50 — calibrated priors
//   - Layer 9 (Position):    time_to_rug_p10_sec — cohort-aware stop-loss timing
//
// Source file: contracts/historical_profile.go
// Producer:    cmd/hydrate
// Consumer:    internal/modules/data_quality, internal/modules/probability,
//
//	internal/modules/position (via orchestrator startup load)
type HistoricalMarketProfileDTO struct {
	// CohortKey uniquely identifies the market cohort (e.g. "tier1_legendary").
	CohortKey string `json:"cohort_key"`

	// TokenCount is the number of tokens in the seed set for this cohort.
	TokenCount int `json:"token_count"`

	// Liquidity percentile stats (USD) across the seed population.
	LiquidityUsdP10 float64 `json:"liquidity_usd_p10"`
	LiquidityUsdP50 float64 `json:"liquidity_usd_p50"`
	LiquidityUsdP90 float64 `json:"liquidity_usd_p90"`

	// Volume (24h) percentile stats (USD).
	Volume24hP10 float64 `json:"volume_24h_p10"`
	Volume24hP50 float64 `json:"volume_24h_p50"`
	Volume24hP90 float64 `json:"volume_24h_p90"`

	// TxVelocity percentile stats (transactions per hour).
	TxVelocityP10 float64 `json:"tx_velocity_p10"`
	TxVelocityP50 float64 `json:"tx_velocity_p50"`
	TxVelocityP90 float64 `json:"tx_velocity_p90"`

	// BuySellRatio percentile stats (buys / sells). Values > 1 mean more buys.
	BuySellRatioP10    float64 `json:"buy_sell_ratio_p10"`
	BuySellRatioMedian float64 `json:"buy_sell_ratio_median"`
	BuySellRatioP90    float64 `json:"buy_sell_ratio_p90"`

	// ATHMultiple percentile estimates (price at ATH / price at launch).
	// Derived from cohort_definitions in historical_seeds.yaml.
	ATHMultipleP10 float64 `json:"ath_multiple_p10"`
	ATHMultipleP50 float64 `json:"ath_multiple_p50"`
	ATHMultipleP90 float64 `json:"ath_multiple_p90"`

	// TimeToRug percentile estimates (seconds from launch to rug event).
	// Zero means the cohort is not expected to rug.
	TimeToRugP10Sec float64 `json:"time_to_rug_p10_sec"`
	TimeToRugP50Sec float64 `json:"time_to_rug_p50_sec"`

	// LiquidityMinUsd is the minimum liquidity floor for this cohort (USD).
	// Tokens below this floor in Layer 1 get a LOW_LIQUIDITY flag.
	LiquidityMinUsd float64 `json:"liquidity_min_usd"`

	// PriorProbability is the calibrated base P(success) for this cohort.
	// Layer 4 probability model uses this as the Bayesian prior.
	PriorProbability float64 `json:"prior_probability"`

	// SocialPresenceRate is the fraction of seed tokens with at least one
	// social link. Used as a signal confidence multiplier.
	SocialPresenceRate float64 `json:"social_presence_rate"`

	// ProfileVersion is a semantic tag for the seed dataset (e.g. "seed_v0").
	ProfileVersion string `json:"profile_version"`

	// ComputedAt is when this profile was last computed by `hydrate`.
	ComputedAt time.Time `json:"computed_at"`
}
