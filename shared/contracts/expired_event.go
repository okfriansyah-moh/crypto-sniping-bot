package contracts

// ExpiredEventDTO is emitted by a worker when it observes a DTO whose TTL
// has elapsed before it could be processed. Used by the learning engine as
// a shadow false-negative candidate.
// EventID = SHA256(original_event_id || "expired")[:16].
//
// Source file: contracts/expired_event.go
// Producer:    any worker (generic loop) / TTL sweeper
// Consumer:    internal/modules/learning
// event_type:  expired_event
type ExpiredEventDTO struct {
EventID       string `json:"event_id"`
TraceID       string `json:"trace_id"`
CorrelationID string `json:"correlation_id"`
CausationID   string `json:"causation_id"` // = original event_id that expired
VersionID     string `json:"version_id"`

OriginalEventType string `json:"original_event_type"`
Stage             string `json:"stage"`         // worker group name
ExpiredAtIso      string `json:"expired_at_iso"`
DelayMs           int64  `json:"delay_ms"` // (now - original.ExpiresAt) in ms

ExpiresAt string `json:"expires_at"` // "" — expired events never themselves expire
Priority  int32  `json:"priority"`   // 0 — drained at idle
EmittedAt string `json:"emitted_at"` // ISO 8601
}
