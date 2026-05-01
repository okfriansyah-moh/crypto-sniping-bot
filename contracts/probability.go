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
	Probability      float64 `json:"probability"` // [p_min, p_max], clipped — never 0 or 1
	Calibration      float64 `json:"calibration"` // Brier-style [0.0, 1.0]
	ModelVersionID   string  `json:"model_version_id"`

	// F-1 (HIGH): probability-modeling skill additions — propagate model
	// input confidence and emit a probability decile bin so the learning
	// engine can compute Brier-score / log-loss buckets per cohort.
	// Both fields are additive; absent producers default them to 0.
	Confidence     float64 `json:"confidence,omitempty"`      // [0.0, 1.0] — min over FeatureConfidence
	CalibrationBin int32   `json:"calibration_bin,omitempty"` // probability decile [0..9]
	ExpiresAt      string  `json:"expires_at"`                // ISO 8601 UTC; "" = no expiry
	Priority       int32   `json:"priority"`                  // higher = processed first; default 0
	EstimatedAt    string  `json:"estimated_at"`              // ISO 8601
}
