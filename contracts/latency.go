package contracts

// LatencyProfileDTO carries the predicted execution latency.
// Emitted by Layer 4 latency model.
//
// Source file: contracts/latency.go
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

ExpiresAt   string `json:"expires_at"`   // ISO 8601 UTC; "" = no expiry
Priority    int32  `json:"priority"`     // higher = processed first; default 0
EstimatedAt string `json:"estimated_at"` // ISO 8601
}
