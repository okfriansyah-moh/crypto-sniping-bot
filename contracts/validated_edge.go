package contracts

// ValidatedEdgeDTO carries the edge after the EV gate in Layer 5.
//
// Source file: contracts/validated_edge.go
// Producer:    internal/modules/validation
// Consumer:    internal/modules/selection (ACCEPT only)
type ValidatedEdgeDTO struct {
	EventID       string `json:"event_id"`
	TraceID       string `json:"trace_id"`
	CorrelationID string `json:"correlation_id"`
	CausationID   string `json:"causation_id"`
	VersionID     string `json:"version_id"`

	TokenLifecycleID string `json:"token_lifecycle_id"`
	TokenAddress     string `json:"token_address"`

	Decision           string  `json:"decision"` // ACCEPT | REJECT
	ExpectedValueBps   int32   `json:"expected_value_bps"`
	ExpectedGainBps    int32   `json:"expected_gain_bps"`
	ExpectedLossBps    int32   `json:"expected_loss_bps"`
	FixedCostsBps      int32   `json:"fixed_costs_bps"`
	ProbabilityUsed    float64 `json:"probability_used"`
	SlippageP95BpsUsed int32   `json:"slippage_p95_bps_used"`
	EvThresholdApplied int32   `json:"ev_threshold_applied"`
	RejectReason       string  `json:"reject_reason"` // empty if ACCEPT

	// §8.5 additive: latency gate outcome.
	// ExpectedLatencyMs = LatencyProfile.P95Ms + Slippage.BuildSubmitP95Ms.
	// If LatencyGatePassed=false, Decision="REJECT" and RejectReason="latency_exceeds_window".
	ExpectedLatencyMs int32 `json:"expected_latency_ms"`
	LatencyGatePassed bool  `json:"latency_gate_passed"`

	// Phase 10 (Reference-Repo Improvements / Task D) — consecutive-pass debounce.
	// ConsecutivePassCount is the count *after* this evaluation. While the
	// count is below the required threshold a PASS evaluation is still
	// reported as Decision="REJECT" with RejectReason="consecutive_pass_pending".
	// The worker persists ConsecutivePassCount + WindowStart in a side table
	// and threads them back into the next Process call.
	ConsecutivePassCount       int32  `json:"consecutive_pass_count,omitempty"`
	ConsecutivePassWindowStart string `json:"consecutive_pass_window_start,omitempty"`

	ExpiresAt   string `json:"expires_at"`   // ISO 8601 UTC; "" = no expiry
	Priority    int32  `json:"priority"`     // higher = processed first; default 0
	ValidatedAt string `json:"validated_at"` // ISO 8601
}
