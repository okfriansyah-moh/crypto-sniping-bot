package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"runtime/debug"
	"time"

	"crypto-sniping-bot/database"
)

// defaultMaxRetries is the fallback retry threshold when callers pass 0.
// On the (maxRetries+1)th failure, the event is moved to dead_letter_events
// instead of being re-released for another retry.
const defaultMaxRetries = 5

// StageHandler processes a single event and returns the output event (if any).
// Implementations must be pure functions — no shared mutable state, no DB calls.
// The orchestrator calls handlers; handlers never call each other.
type StageHandler interface {
	// Process handles an incoming event and returns the output event.
	// Return nil output to not emit any downstream event. When returning nil,
	// handlers SHOULD call RecordDecision(ctx, status, reason) so the
	// stage_completed log line carries an explicit discriminator and remains
	// correlatable to the input event_id (see traceability skill).
	// Return an error to abort processing — the event remains unprocessed for retry.
	Process(ctx context.Context, evt *database.Event) (*database.Event, error)
}

// stageDecisionKey is the context key for the per-Process decision recorder.
// It is unexported and addressed by type identity so no external package can
// collide with or overwrite the recorder out-of-band.
type stageDecisionKey struct{}

// stageDecision is the per-Process record of the handler's no-output
// discriminator. RunWorker allocates a fresh value for every claimed event,
// so there is no shared state across goroutines or events. Idempotency: the
// same input drives the same handler code path which records the same
// (status, reason), so retries log identical stage_completed records.
type stageDecision struct {
	status string
	reason string
}

// RecordDecision attaches an explicit output_status and decision_reason to
// the upcoming stage_completed log line for the current Process call.
// Handlers that intentionally return (nil, nil) — for REJECT, FILTER,
// TERMINAL, or SKIP paths — MUST call this so observability traces remain
// correlatable. status MUST be one of the StageStatus* constants.
//
// Calling this when the handler also returns a non-nil output event is a
// no-op for the status field: the orchestrator overrides status to
// StageStatusEmitted because the canonical correlation is via output_event_id.
//
// Outside of a worker-driven context (e.g. unit tests calling Process
// directly without RunWorker) this function is a no-op.
func RecordDecision(ctx context.Context, status, reason string) {
	if ctx == nil {
		return
	}
	d, ok := ctx.Value(stageDecisionKey{}).(*stageDecision)
	if !ok || d == nil {
		return
	}
	d.status = status
	d.reason = reason
}

// RunWorker runs the generic event worker loop for a stage handler.
// Loop:
//  1. ClaimNextEvent — SELECT FOR UPDATE SKIP LOCKED
//  2. If nil → sleep(idleBackoff), continue
//  3. handler.Process(evt) → output event (or err)
//  4. On err →
//     a. IncrementEventRetry(eventID, group) → newCount
//     b. If newCount > maxRetries → MoveToDLQ (poison-pill barrier)
//     c. Else → ReleaseEventClaim (immediate retry by another worker)
//  5. InsertEvent(output)   — ON CONFLICT DO NOTHING (idempotent)
//  6. MarkEventProcessed(evt.EventID)
//
// maxRetries is the cap on transient failures before an event is dead-lettered.
// Pass 0 to use defaultMaxRetries (5). The threshold is INCLUSIVE: an event
// is dead-lettered on the attempt where retry_count first exceeds maxRetries.
//
// Recovers from handler panics (configured via panic_recovery).
// Stops cleanly when ctx is cancelled.
//
// See docs/reference/db_adapter_spec.md § 9 and architecture.md § 4.10 (failure handling).
func RunWorker(
	ctx context.Context,
	adapter database.Adapter,
	group string,
	eventTypes []string,
	handler StageHandler,
	idleBackoff time.Duration,
	logger *slog.Logger,
	maxRetries ...int,
) error {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	retryCap := defaultMaxRetries
	if len(maxRetries) > 0 && maxRetries[0] > 0 {
		retryCap = maxRetries[0]
	}
	workerLog := logger.With("worker_group", group, "max_retries", retryCap)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		evt, err := adapter.ClaimNextEvent(ctx, group, eventTypes)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			workerLog.Error("claim_event_failed", "error", err)
			time.Sleep(idleBackoff)
			continue
		}
		if evt == nil {
			time.Sleep(idleBackoff)
			continue
		}

		evtLog := workerLog.With(
			"event_id", evt.EventID,
			"event_type", evt.EventType,
			"trace_id", evt.TraceID,
			"correlation_id", evt.CorrelationID,
			"version_id", evt.VersionID,
		)

		// Allocate a fresh decision recorder per event and inject via context.
		// Handlers signal no-output discriminators (rejected/filtered/...)
		// through RecordDecision — see traceability skill (F-8 fix).
		decision := &stageDecision{}
		procCtx := context.WithValue(ctx, stageDecisionKey{}, decision)

		output, stageErr := safeProcess(procCtx, handler, evt, evtLog)
		if stageErr != nil {
			if errors.Is(stageErr, database.ErrLifecycleAlreadyAdvanced) {
				// Idempotent skip: stale event from a prior session — lifecycle already
				// advanced past the expected from-state. Mark it processed to drain
				// the queue cleanly; do not retry or DLQ.
				if markErr := adapter.MarkEventProcessed(ctx, evt.EventID); markErr != nil {
					evtLog.Error("mark_processed_failed", "error", markErr)
					continue
				}
				evtLog.Info("stage_skipped_lifecycle_advanced",
					"lifecycle_error", stageErr.Error(),
					"event_id", evt.EventID,
				)
				continue
			}
			handleStageFailure(ctx, adapter, group, retryCap, evt, stageErr, evtLog)
			continue
		}

		if output != nil {
			if err := adapter.InsertEvent(ctx, *output); err != nil {
				evtLog.Error("insert_output_event_failed", "error", err)
				continue
			}
		}

		if err := adapter.MarkEventProcessed(ctx, evt.EventID); err != nil {
			evtLog.Error("mark_processed_failed", "error", err)
			continue
		}

		status, reason := resolveStageOutcome(output, decision)
		evtLog.Info("stage_completed",
			LogFieldOutputEventID, outputEventID(output),
			LogFieldOutputStatus, status,
			LogFieldDecisionReason, reason,
		)
	}
}

// handleStageFailure records a handler failure, increments the retry counter,
// and either re-releases the claim for another attempt or moves the event to
// the dead_letter_events table when the retry cap is exceeded. This is the
// poison-pill barrier required by architecture.md § 4.10 — without it,
// a permanently-failing event would loop forever and block the pipeline.
func handleStageFailure(
	ctx context.Context,
	adapter database.Adapter,
	group string,
	maxRetries int,
	evt *database.Event,
	stageErr error,
	logger *slog.Logger,
) {
	count, incErr := adapter.IncrementEventRetry(ctx, evt.EventID, group)
	if incErr != nil {
		// Best-effort: log and fall through to release-claim so we don't
		// permanently lose the event because of a transient counter failure.
		logger.Error("increment_retry_failed", "error", incErr, "stage_error", stageErr)
		count = 1
	}

	if count > maxRetries {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		entry := database.DLQEntry{
			EventID:       evt.EventID,
			Chain:         evt.Chain,
			Consumer:      group,
			Reason:        "transient_exceeded",
			ErrorMessage:  stageErr.Error(),
			RetryCount:    count,
			FirstFailedAt: now, // approximate; durable first-failed lives in events.last_failed_at
			LastFailedAt:  now,
			MovedToDLQAt:  now,
			TraceID:       evt.TraceID,
			CorrelationID: evt.CorrelationID,
			CausationID:   evt.CausationID,
			VersionID:     evt.VersionID,
			PayloadJSON:   ensureJSON(evt.Payload),
		}
		if dlqErr := adapter.MoveToDLQ(ctx, entry); dlqErr != nil {
			logger.Error("move_to_dlq_failed",
				"error", dlqErr,
				"retry_count", count,
				"stage_error", stageErr,
				"note", "event will continue retrying — DLQ unavailable",
			)
			// Fall through to release-claim so retries continue rather than
			// silently dropping the event.
			if rErr := adapter.ReleaseEventClaim(ctx, evt.EventID); rErr != nil {
				logger.Error("release_claim_failed", "error", rErr)
			}
			return
		}
		logger.Error("event_moved_to_dlq",
			"retry_count", count,
			"stage_error", stageErr,
			"reason", "transient_exceeded",
		)
		return
	}

	logger.Warn("stage_handler_failed",
		"error", stageErr,
		"retry_count", count,
		"max_retries", maxRetries,
		"note", "releasing claim for immediate retry",
	)
	if releaseErr := adapter.ReleaseEventClaim(ctx, evt.EventID); releaseErr != nil {
		logger.Error("release_claim_failed", "error", releaseErr)
	}
}

// ensureJSON returns a non-nil JSON byte slice. DLQEntry.PayloadJSON is
// declared []byte; callers may pass nil (e.g., synthetic events in tests).
// We coerce nil to a JSON null so the dead_letter_events row is well-formed.
func ensureJSON(b []byte) []byte {
	if len(b) == 0 {
		return []byte("null")
	}
	// Validate that the bytes are valid JSON; if not, wrap them as a JSON string.
	if !json.Valid(b) {
		quoted, err := json.Marshal(string(b))
		if err != nil {
			return []byte("null")
		}
		return quoted
	}
	return b
}

// safeProcess calls handler.Process and recovers from panics.
// Returns database.ErrStagePanic on panic so callers can distinguish
// a handler panic from a legitimate context.DeadlineExceeded timeout.
func safeProcess(ctx context.Context, h StageHandler, evt *database.Event, logger *slog.Logger) (output *database.Event, err error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("stage_handler_panic",
				"panic", r,
				"stack", string(debug.Stack()),
			)
			err = database.ErrStagePanic
		}
	}()
	return h.Process(ctx, evt)
}

func outputEventID(evt *database.Event) string {
	if evt == nil {
		return ""
	}
	return evt.EventID
}

// resolveStageOutcome derives the (output_status, decision_reason) tuple
// logged on stage_completed. Resolution rules:
//
//  1. Output emitted          → StageStatusEmitted, reason="" (correlation
//     is via output_event_id; reason is reserved for non-emit cases).
//  2. No output, recorded     → use the handler-recorded status and reason.
//  3. No output, not recorded → StageStatusFiltered, reason="" — the legacy
//     default for handlers that have not yet adopted RecordDecision.
//
// Deterministic and side-effect-free; safe for tests to call directly.
func resolveStageOutcome(output *database.Event, d *stageDecision) (status, reason string) {
	if output != nil {
		return StageStatusEmitted, ""
	}
	if d != nil && d.status != "" {
		return d.status, d.reason
	}
	return StageStatusFiltered, ""
}
