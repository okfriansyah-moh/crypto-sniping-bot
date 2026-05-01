package orchestrator

// Log field names emitted by RunWorker on the stage_completed structured log
// record. Documented as constants so downstream parsers, dashboards, and
// replay tooling stay in sync with the producer. Any change here must be
// reflected in operator dashboards and trace correlators (see
// docs/architecture.md § 13 Observability and the traceability skill).
const (
	// LogFieldOutputEventID is the EventID of the downstream event emitted by
	// the handler, or "" when no output event was produced.
	LogFieldOutputEventID = "output_event_id"

	// LogFieldOutputStatus is the discriminator for the no-output case: it
	// guarantees that traces with output_event_id="" are still correlatable
	// to their input event_id and a documented decision class. Required by
	// the four-field trace contract.
	LogFieldOutputStatus = "output_status"

	// LogFieldDecisionReason is a short, machine-readable reason code
	// (e.g. comma-joined RejectReasons, vedge.RejectReason) attached when
	// output_status indicates a non-emit decision. Empty when the handler
	// did not record a reason or when output was emitted.
	LogFieldDecisionReason = "decision_reason"
)

// Stage outcome statuses logged on stage_completed. Exactly one is emitted
// per processed event. New values must be added here and documented before
// any handler emits them.
const (
	// StageStatusEmitted — handler produced a downstream output event that was
	// inserted on the bus. output_event_id is non-empty.
	StageStatusEmitted = "emitted"

	// StageStatusRejected — handler intentionally suppressed the output as a
	// hard rejection (e.g. data_quality REJECT, validation EV-gate REJECT).
	StageStatusRejected = "rejected"

	// StageStatusFiltered — handler dropped the event without it being a hard
	// rejection (e.g. dedup miss, exploration-band drop, non-trigger update).
	StageStatusFiltered = "filtered"

	// StageStatusTerminal — handler observed a terminal lifecycle state and
	// no downstream event is expected (e.g. evaluation of an exited position
	// where the result is persisted but no further bus event flows).
	StageStatusTerminal = "terminal"

	// StageStatusSkipped — handler skipped due to an upstream invariant not
	// yet satisfied (e.g. position not exited yet, prerequisite join row
	// missing). The event is marked processed; the next-stage worker will
	// pick up the prerequisite when it lands.
	StageStatusSkipped = "skipped"
)
