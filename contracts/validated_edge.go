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

	Decision           string  `json:"decision"`             // ACCEPT | REJECT
	ExpectedValueBps   int32   `json:"expected_value_bps"`
	ExpectedGainBps    int32   `json:"expected_gain_bps"`
	ExpectedLossBps    int32   `json:"expected_loss_bps"`
	FixedCostsBps      int32   `json:"fixed_costs_bps"`
	ProbabilityUsed    float64 `json:"probability_used"`
	SlippageP95BpsUsed int32   `json:"slippage_p95_bps_used"`
	EvThresholdApplied int32   `json:"ev_threshold_applied"`
	RejectReason       string  `json:"reject_reason"` // empty if ACCEPT
	ValidatedAt        string  `json:"validated_at"`  // ISO 8601
}
