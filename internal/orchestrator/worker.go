package orchestrator

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"crypto-sniping-bot/database"
)

// StageHandler processes a single event and returns the output event (if any).
// Implementations must be pure functions — no shared mutable state, no DB calls.
// The orchestrator calls handlers; handlers never call each other.
type StageHandler interface {
	// Process handles an incoming event and returns the output event.
	// Return nil output to not emit any downstream event.
	// Return an error to abort processing — the event remains unprocessed for retry.
	Process(ctx context.Context, evt *database.Event) (*database.Event, error)
}

// RunWorker runs the generic event worker loop for a stage handler.
// Loop:
//  1. ClaimNextEvent — SELECT FOR UPDATE SKIP LOCKED
//  2. If nil → sleep(idleBackoff), continue
//  3. handler.Process(evt) → output event (or err)
//  4. On err → log, leave event unprocessed (retry), continue
//  5. InsertEvent(output)   — ON CONFLICT DO NOTHING
//  6. MarkEventProcessed(evt.EventID)
//
// Recovers from handler panics (configured via panic_recovery).
// Stops cleanly when ctx is cancelled.
//
// See docs/db_adapter_spec.md § 9.
func RunWorker(
	ctx context.Context,
	adapter database.Adapter,
	group string,
	eventTypes []string,
	handler StageHandler,
	idleBackoff time.Duration,
	logger *slog.Logger,
) error {
	workerLog := logger.With("worker_group", group)

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

		output, stageErr := safeProcess(ctx, handler, evt, evtLog)
		if stageErr != nil {
			evtLog.Error("stage_handler_failed",
				"error", stageErr,
				"note", "event stays unprocessed for retry",
			)
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

		evtLog.Info("stage_completed", "output_event_id", outputEventID(output))
	}
}

// safeProcess calls handler.Process and recovers from panics.
func safeProcess(ctx context.Context, h StageHandler, evt *database.Event, logger *slog.Logger) (output *database.Event, err error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("stage_handler_panic",
				"panic", r,
				"stack", string(debug.Stack()),
			)
			err = context.DeadlineExceeded // non-nil error to skip mark-processed
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
