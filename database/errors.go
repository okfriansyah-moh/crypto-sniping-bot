package database

import "errors"

// ErrNotImplemented is returned by stub adapter methods that have not yet
// been implemented in any engine. Replace stubs with real implementations
// in the relevant engine package.
var ErrNotImplemented = errors.New("database: method not yet implemented")

// ErrUnknownVersion is returned when a strategy version ID does not exist.
var ErrUnknownVersion = errors.New("database: unknown strategy version")

// ErrInvalidTransition is returned when the CAS guard (state_version or current_state)
// fails on a state machine transition attempt.
var ErrInvalidTransition = errors.New("database: invalid state transition")

// ErrForbiddenTransition is returned when the requested target state is not a
// permitted forward transition from the current state (topology violation).
var ErrForbiddenTransition = errors.New("database: forbidden state transition")

// ErrMissingTraceField is returned when an event is inserted without the
// required trace fields (trace_id, correlation_id, version_id).
var ErrMissingTraceField = errors.New("database: missing required trace field")

// ErrOrphanEvent is returned when an event references a causation_id that
// does not exist in the event bus.
var ErrOrphanEvent = errors.New("database: orphan event — causation_id not found")

// ErrEventExpired is returned when an attempt is made to process an event
// whose expires_at has elapsed.
var ErrEventExpired = errors.New("database: event has expired")

// ErrStaleState is returned by UpsertSystemState when expectedVersion does
// not match the current state_version in the database.
var ErrStaleState = errors.New("database: stale system state version")

// ErrInvalidEnum is returned when an enum field value is not one of the
// allowed values defined in the schema.
var ErrInvalidEnum = errors.New("database: invalid enum value")

// ErrIllegalTransition is returned by SetStrategyVersionStatus when the
// requested status transition is not on the legal strategy version lifecycle graph.
var ErrIllegalTransition = errors.New("database: illegal strategy version status transition")

// ErrEnvelopeBreach is returned when an ExposureSummary update would push
// the portfolio past the envelope limit defined in config.
var ErrEnvelopeBreach = errors.New("database: exposure envelope breach")

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("database: record not found")

// ErrNonceGap is returned when the on-chain nonce is ahead of the local
// nonce manager, indicating a transaction was submitted outside the system.
var ErrNonceGap = errors.New("database: nonce gap detected")

// ErrStagePanic is returned by safeProcess when a stage handler panics.
// Distinct from context.DeadlineExceeded so metrics and alerting can
// distinguish panics from timeouts.
var ErrStagePanic = errors.New("orchestrator: stage handler panicked")

// ErrDrainTimeout is returned by PromoteStrategyVersion when the pipeline
// does not reach idle within drainTimeoutSec.
var ErrDrainTimeout = errors.New("database: drain timeout — pipeline not idle")

// ErrAlreadyClaimed is returned by ClaimExecution when another worker has
// already reserved the execution_id.
var ErrAlreadyClaimed = errors.New("database: execution already claimed by another worker")
