package contracts

// FeatureDTO is the normalized feature vector with per-feature confidence.
// Emitted by Layer 2 feature extraction.
//
// Source file: contracts/feature.go
// Producer:    internal/modules/features
// Consumer:    internal/modules/edge
type FeatureDTO struct {
	EventID       string `json:"event_id"`
	TraceID       string `json:"trace_id"`
	CorrelationID string `json:"correlation_id"`
	CausationID   string `json:"causation_id"`
	VersionID     string `json:"version_id"`

	TokenLifecycleID string `json:"token_lifecycle_id"`
	TokenAddress     string `json:"token_address"`

	// Market identifies the originating market (e.g. "eth-uniswap-v2").
	// Threaded from the upstream MarketDataDTO.Market for downstream
	// per-market α / latency / cohort lookups (Layer 4 slippage model in
	// particular). Empty when upstream MarketDataDTO is unavailable —
	// downstream models then fall back to cold-start defaults.
	Market string `json:"market,omitempty"`
	// Chain is the chain key (e.g. "eth", "bsc", "solana"). Threaded from
	// the upstream MarketDataDTO.Chain for chain-aware fallback paths.
	Chain string `json:"chain,omitempty"`

	// CohortKey is the historical market cohort this token maps to
	// (e.g. "tier1_legendary", "tier3_pumpfun_era"). Populated by the
	// feature extraction layer via the CohortClassifier (optional; empty
	// when no cohort mapping is available). Consumed by Layer 4
	// (probability prior lookup) and Layer 9 (position timing).
	CohortKey string `json:"cohort_key,omitempty"`

	// Normalized [0.0, 1.0] features
	LiquidityScore     float64 `json:"liquidity_score"`
	TxVelocityScore    float64 `json:"tx_velocity_score"`
	HolderDistribution float64 `json:"holder_distribution"`
	WalletEntropy      float64 `json:"wallet_entropy"`
	ContractSafety     float64 `json:"contract_safety"`
	TokenAge           float64 `json:"token_age"`
	VolumeMomentum     float64 `json:"volume_momentum"`
	PriceMomentum      float64 `json:"price_momentum"`

	// Raw reference values (for audit / learning)
	LiquidityUsdRaw    float64 `json:"liquidity_usd_raw"`
	TxVelocity30sRaw   float64 `json:"tx_velocity_30s_raw"`
	HolderCountRaw     int64   `json:"holder_count_raw"`
	TokenAgeSecondsRaw int64   `json:"token_age_seconds_raw"`

	// Per-feature confidence [0.0, 1.0]
	Confidence FeatureConfidence `json:"confidence"`

	ExpiresAt   string `json:"expires_at"`   // ISO 8601 UTC; "" = no expiry
	Priority    int32  `json:"priority"`     // higher = processed first; default 0
	ExtractedAt string `json:"extracted_at"` // ISO 8601

	// Phase 11 (Reference-Repo Improvements R2 — FEATURES) — holder
	// concentration & social-presence signals. Both are optional and
	// default to zero/false (= "unknown"). Producers populate them only
	// when the underlying RPC sample is available within the feature
	// budget; otherwise FeatureConfidence.HolderTopNPct stays 0.
	//
	// HolderTopNPctBps: combined supply pct (in bps) held by the top N
	// holders (N is config-driven, see feature.yaml). High concentration
	// = rug-prone. Adapted from hexnome's holder distribution sampler.
	// HasSocialLinks: at least one of {website, twitter, telegram} was
	// asserted at token creation. Adapted from AxisBot's social presence
	// flag. Used as a soft positive signal in EdgeDTO scoring.
	HolderTopNPctBps int32 `json:"holder_top_n_pct_bps,omitempty"`
	HasSocialLinks   bool  `json:"has_social_links,omitempty"`

	// P7 — Recent price observations for bottom-detection.
	// Populated by feature extraction from the per-pool swap-event sync
	// cache; ordered oldest-first (ascending block timestamp).
	// Each element is a USD price.  Empty when price history is
	// unavailable — the edge module's bottom detector returns score=0 in
	// that case and the pipeline continues without gating.
	RecentPricesUsd []float64 `json:"recent_prices_usd,omitempty"`
}

// FeatureConfidence holds per-feature confidence scores.
type FeatureConfidence struct {
	LiquidityScore     float64 `json:"liquidity_score"`
	TxVelocityScore    float64 `json:"tx_velocity_score"`
	HolderDistribution float64 `json:"holder_distribution"`
	WalletEntropy      float64 `json:"wallet_entropy"`
	ContractSafety     float64 `json:"contract_safety"`
	TokenAge           float64 `json:"token_age"`
	VolumeMomentum     float64 `json:"volume_momentum"`
	PriceMomentum      float64 `json:"price_momentum"`

	// Phase 11 additive: confidence for the new holder/social signals.
	HolderTopNPct  float64 `json:"holder_top_n_pct,omitempty"`
	HasSocialLinks float64 `json:"has_social_links,omitempty"`
}
