package database

import "errors"

// Sentinel errors returned by the adapter. Callers use errors.Is() to check.
var (
	// ErrOrphanEvent is returned when InsertEvent is called with a CausationID
	// that does not reference an existing EventID in the events table.
	ErrOrphanEvent = errors.New("database: orphan event — causation_id references non-existent event")

	// ErrInvalidTransition is returned when TransitionState cannot update a row
	// because the CAS guard (state_version or current_state) did not match.
	ErrInvalidTransition = errors.New("database: invalid state transition — CAS check failed")

	// ErrMissingTraceField is returned when InsertEvent is called with an event
	// that is missing one of trace_id, correlation_id, or version_id.
	ErrMissingTraceField = errors.New("database: missing required trace field (trace_id, correlation_id, or version_id)")

	// ErrUnknownVersion is returned when GetStrategyVersion cannot find the
	// requested strategy_version_id.
	ErrUnknownVersion = errors.New("database: unknown strategy version")

	// ErrNonceGap is returned when AllocateNonce detects a gap between the
	// locally tracked nonce and the on-chain value, indicating a reconciliation
	// is needed before proceeding.
	ErrNonceGap = errors.New("database: nonce gap detected — reconcile required")

	// ErrNotFound is returned when a requested record does not exist.
	ErrNotFound = errors.New("database: record not found")

	// ErrNotImplemented is returned for adapter methods not yet implemented
	// in this phase. Methods become fully operational when their migration
	// has been applied.
	ErrNotImplemented = errors.New("database: method not implemented in this phase")
)
