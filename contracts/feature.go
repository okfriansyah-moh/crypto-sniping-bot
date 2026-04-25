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

	ExtractedAt string `json:"extracted_at"` // ISO 8601
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
}
