package contracts

// EvaluationDTO carries per-version performance metrics over a time window.
// Emitted by Layer 10 learning engine.
//
// Source file: contracts/evaluation.go
// Producer:    internal/modules/learning
// Consumer:    internal/modules/learning (triggers weight updates)
type EvaluationDTO struct {
	EventID       string `json:"event_id"`
	TraceID       string `json:"trace_id"`
	CorrelationID string `json:"correlation_id"`
	CausationID   string `json:"causation_id"`
	VersionID     string `json:"version_id"` // version being evaluated

	EvaluationID string `json:"evaluation_id"`
	WindowStart  string `json:"window_start"` // ISO 8601
	WindowEnd    string `json:"window_end"`   // ISO 8601
	SampleSize   int32  `json:"sample_size"`

	TruePositiveCount  int32 `json:"true_positive_count"`
	FalsePositiveCount int32 `json:"false_positive_count"`
	TrueNegativeCount  int32 `json:"true_negative_count"`
	FalseNegativeCount int32 `json:"false_negative_count"`

	Expectancy          float64 `json:"expectancy"`
	MaxDrawdownPct      float64 `json:"max_drawdown_pct"`
	BrierScore          float64 `json:"brier_score"`
	PredictionErrorMean float64 `json:"prediction_error_mean"`

	ExpiresAt   string `json:"expires_at"`   // ISO 8601 UTC; "" = no expiry
	Priority    int32  `json:"priority"`     // higher = processed first; default 0
	EvaluatedAt string `json:"evaluated_at"` // ISO 8601

	// Phase 11 (Reference-Repo Improvements R2 — EVALUATE) — simulated vs
	// realized execution diff. Adapted from AxisBot's pre/post-trade
	// simulation. ExecutionVarianceBps = (realizedOut - simulatedOut) /
	// simulatedOut * 10000; negative means we got LESS than simulated.
	// Populated only when the orchestrator captures a pre-trade simulation
	// for the trade; otherwise zero.
	SimulatedExpectedOutAmount string `json:"simulated_expected_out_amount,omitempty"` // decimal string
	RealizedOutAmount          string `json:"realized_out_amount,omitempty"`           // decimal string
	ExecutionVarianceBps       int32  `json:"execution_variance_bps,omitempty"`        // can be negative
}
