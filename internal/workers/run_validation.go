package workers

import (
"context"
"encoding/json"
"fmt"
"log/slog"

"crypto-sniping-bot/contracts"
"crypto-sniping-bot/database"
"crypto-sniping-bot/internal/app/config"
"crypto-sniping-bot/internal/modules/validation"
)

// ValidationWorker implements Layer 5: Edge Validation (EV gate).
// Consumes: edge_event → emits: validated_edge_event (ACCEPT only)
type ValidationWorker struct {
adapter database.Adapter
mod     *validation.Module
logger  *slog.Logger
}

// NewValidationWorker returns a new ValidationWorker.
func NewValidationWorker(adapter database.Adapter, cfg *config.Config, logger *slog.Logger) *ValidationWorker {
if logger == nil {
logger = slog.Default()
}
return &ValidationWorker{
adapter: adapter,
mod:     validation.New(&cfg.Validation),
logger:  logger,
}
}

func (w *ValidationWorker) Process(ctx context.Context, evt *database.Event) (*database.Event, error) {
var dto contracts.EdgeDTO
if err := json.Unmarshal(evt.Payload, &dto); err != nil {
return nil, fmt.Errorf("validation_worker: unmarshal: %w", err)
}

vedge, err := w.mod.Process(ctx, dto)
if err != nil {
return nil, fmt.Errorf("validation_worker: module: %w", err)
}

if err := w.adapter.InsertValidatedEdge(ctx, vedge); err != nil {
w.logger.Warn("validation_worker_persist_failed", "event_id", vedge.EventID, "error", err)
}

nextState := "VALIDATED"
if vedge.Decision != "ACCEPT" {
nextState = "REJECTED"
}
if lc, ok := fetchLifecycle(ctx, w.adapter, dto.TokenLifecycleID, w.logger); ok {
transitionBestEffort(ctx, w.adapter, database.TransitionRequest{
LifecycleID:       dto.TokenLifecycleID,
ExpectedFromState: "EDGE_DETECTED",
ExpectedVersion:   lc.StateVersion,
NewState:          nextState,
Reason:            vedge.RejectReason,
ActorWorker:       "validation_worker",
}, w.logger)
}

if vedge.Decision != "ACCEPT" {
return nil, nil
}

return makeOutputEvent(
vedge.EventID, vedge, "validated_edge_event",
evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
)
}
