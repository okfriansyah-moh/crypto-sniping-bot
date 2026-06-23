package contracts

// SlippageEstimateDTO carries the predicted slippage for a trade.
// Emitted by Layer 4 slippage model.
//
// Source file: contracts/slippage.go
// Producer:    internal/modules/models
// Consumer:    internal/modules/validation, internal/modules/execution
type SlippageEstimateDTO struct {
	EventID       string `json:"event_id"`
	TraceID       string `json:"trace_id"`
	CorrelationID string `json:"correlation_id"`
	CausationID   string `json:"causation_id"`
	VersionID     string `json:"version_id"`

	TokenLifecycleID string `json:"token_lifecycle_id"`
	ExpectedP50Bps   int32  `json:"expected_p50_bps"`
	ExpectedP95Bps   int32  `json:"expected_p95_bps"`
	ModelVersionID   string `json:"model_version_id"`

	ExpiresAt   string `json:"expires_at"`   // ISO 8601 UTC; "" = no expiry
	Priority    int32  `json:"priority"`     // higher = processed first; default 0
	EstimatedAt string `json:"estimated_at"` // ISO 8601

	// Phase 11 (Reference-Repo Improvements R2 — P/S/L MODELS) — congestion
	// multiplier applied to expected P95. 1.0 = no adjustment (normal).
	// Adapted from mux's congestion-aware slippage. Bounded by the
	// SlippageConfig.MaxCongestionMultiplier YAML value.
	CongestionMultiplier float64 `json:"congestion_multiplier,omitempty"`

	// F-3 fix (CPMM slippage model) — [0,1] confidence derived from input
	// depth (liquidity raw + per-feature confidence). Producers populate
	// it; consumers MAY use it to bias the EV gate. Optional / additive:
	// existing rows persisted before this field was introduced will load
	// as 0.0 which downstream consumers treat as "unknown".
	Confidence float64 `json:"confidence,omitempty"`
}
