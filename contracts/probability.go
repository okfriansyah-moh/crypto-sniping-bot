package contracts

// ProbabilityEstimateDTO carries the model output for success probability.
// Emitted by Layer 4 probability model.
//
// Source file: contracts/probability.go
// Producer:    internal/modules/models
// Consumer:    internal/modules/validation
type ProbabilityEstimateDTO struct {
	EventID       string `json:"event_id"`
	TraceID       string `json:"trace_id"`
	CorrelationID string `json:"correlation_id"`
	CausationID   string `json:"causation_id"`
	VersionID     string `json:"version_id"`

	TokenLifecycleID string  `json:"token_lifecycle_id"`
	Probability      float64 `json:"probability"`    // [0.0, 1.0]
	Calibration      float64 `json:"calibration"`    // Brier-style [0.0, 1.0]
	ModelVersionID   string  `json:"model_version_id"`
	EstimatedAt      string  `json:"estimated_at"` // ISO 8601
}

// SlippageEstimateDTO carries the predicted slippage for a trade.
// Emitted by Layer 4 slippage model.
//
// Source file: contracts/probability.go
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
	EstimatedAt      string `json:"estimated_at"` // ISO 8601
}

// LatencyProfileDTO carries the predicted execution latency.
// Emitted by Layer 4 latency model.
//
// Source file: contracts/probability.go
// Producer:    internal/modules/models
// Consumer:    internal/modules/execution
type LatencyProfileDTO struct {
	EventID       string `json:"event_id"`
	TraceID       string `json:"trace_id"`
	CorrelationID string `json:"correlation_id"`
	CausationID   string `json:"causation_id"`
	VersionID     string `json:"version_id"`

	Chain             string `json:"chain"`
	ExpectedP50Ms     int32  `json:"expected_p50_ms"`
	ExpectedP95Ms     int32  `json:"expected_p95_ms"`
	WindowSizeSeconds int32  `json:"window_size_seconds"`
	EstimatedAt       string `json:"estimated_at"` // ISO 8601
}
