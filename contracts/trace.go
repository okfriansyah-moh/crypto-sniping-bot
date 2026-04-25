package contracts

// TraceFields is an embedded struct providing the four mandatory correlation fields
// that every DTO flowing through the event bus must carry.
// See docs/dto_contracts.md § 1 and docs/implementation_roadmap.md § 0.3.
//
// Propagation rules:
//   - TraceID:       copy from input DTO; generated fresh only in Layer 0 (Phase 1).
//   - CorrelationID: copy from input DTO; generated fresh only in Layer 0 (Phase 1).
//   - CausationID:   set to inputEvent.EventID. Empty string "" ONLY for Layer 0 root events.
//   - VersionID:     copy from active StrategyVersion pinned at orchestrator start.
type TraceFields struct {
	TraceID       string `json:"trace_id"`
	CorrelationID string `json:"correlation_id"`
	CausationID   string `json:"causation_id"`
	VersionID     string `json:"version_id"`
}

// NewTraceFields constructs a TraceFields from the given values.
// Callers should not set CausationID to empty string unless this is a Layer 0 root event.
func NewTraceFields(traceID, correlationID, causationID, versionID string) TraceFields {
	return TraceFields{
		TraceID:       traceID,
		CorrelationID: correlationID,
		CausationID:   causationID,
		VersionID:     versionID,
	}
}

// Propagate returns a new TraceFields derived from this one, with the given
// causationID (the EventID of the event being processed) and same versionID.
func (t TraceFields) Propagate(causationID string) TraceFields {
	return TraceFields{
		TraceID:       t.TraceID,
		CorrelationID: t.CorrelationID,
		CausationID:   causationID,
		VersionID:     t.VersionID,
	}
}
