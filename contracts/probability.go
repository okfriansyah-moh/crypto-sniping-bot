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
Probability      float64 `json:"probability"`     // [0.0, 1.0]
Calibration      float64 `json:"calibration"`     // Brier-style [0.0, 1.0]
ModelVersionID   string  `json:"model_version_id"`

ExpiresAt   string `json:"expires_at"`   // ISO 8601 UTC; "" = no expiry
Priority    int32  `json:"priority"`     // higher = processed first; default 0
EstimatedAt string `json:"estimated_at"` // ISO 8601
}
