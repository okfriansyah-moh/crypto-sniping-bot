package contracts

// EdgeDTO carries the raw trading edge, pre-validation.
// Emitted by Layer 3 signal & edge discovery.
//
// Source file: contracts/edge.go
// Producer:    internal/modules/edge
// Consumer:    internal/modules/validation
type EdgeDTO struct {
EventID       string `json:"event_id"`
TraceID       string `json:"trace_id"`
CorrelationID string `json:"correlation_id"`
CausationID   string `json:"causation_id"`
VersionID     string `json:"version_id"`

TokenLifecycleID string `json:"token_lifecycle_id"`
TokenAddress     string `json:"token_address"`

EdgeType         string  `json:"edge_type"`          // NEW_LAUNCH | MOMENTUM | WALLET_SURGE
EdgeStrength     float64 `json:"edge_strength"`      // [0.0, 1.0]
EdgeConfidence   float64 `json:"edge_confidence"`    // [0.0, 1.0]
MomentumScore    float64 `json:"momentum_score"`     // [0.0, 1.0]
ThresholdApplied float64 `json:"threshold_applied"`

// §8.4 additive: time window within which entry must complete.
// Computed at emission: base_ms * (1 + momentum_factor) from config.
OpportunityWindowMs int32 `json:"opportunity_window_ms"`

ExpiresAt  string `json:"expires_at"`  // ISO 8601 UTC; "" = no expiry
Priority   int32  `json:"priority"`    // higher = processed first; default 0
DetectedAt string `json:"detected_at"` // ISO 8601
}
