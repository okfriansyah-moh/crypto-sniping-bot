package workers

import (
"context"
"encoding/json"
"fmt"
"log/slog"

"crypto-sniping-bot/contracts"
"crypto-sniping-bot/database"
"crypto-sniping-bot/internal/app/config"
"crypto-sniping-bot/internal/modules/capital"
)

// CapitalWorker implements Layer 7: Capital Engine.
// Consumes: selection_event → emits: allocation_event
type CapitalWorker struct {
adapter database.Adapter
mod     *capital.Module
cfg     *config.Config
logger  *slog.Logger
}

// NewCapitalWorker returns a new CapitalWorker.
func NewCapitalWorker(adapter database.Adapter, cfg *config.Config, logger *slog.Logger) *CapitalWorker {
if logger == nil {
logger = slog.Default()
}
return &CapitalWorker{
adapter: adapter,
mod:     capital.New(&cfg.Capital),
cfg:     cfg,
logger:  logger,
}
}

func (w *CapitalWorker) Process(ctx context.Context, evt *database.Event) (*database.Event, error) {
var dto contracts.SelectionOutputDTO
if err := json.Unmarshal(evt.Payload, &dto); err != nil {
return nil, fmt.Errorf("capital_worker: unmarshal: %w", err)
}

chain := chainFromCorrelation(ctx, w.adapter, evt.CorrelationID, w.logger)
if chain == "" {
chain = firstChain(w.cfg)
}

allocDTO, err := w.mod.Process(ctx, dto, chain)
if err != nil {
return nil, fmt.Errorf("capital_worker: module: %w", err)
}

if err := w.adapter.InsertAllocation(ctx, allocDTO); err != nil {
w.logger.Warn("capital_worker_persist_failed", "event_id", allocDTO.EventID, "error", err)
}

// No lifecycle transition here — lifecycle stays SELECTED until execution completes.

return makeOutputEvent(
allocDTO.EventID, allocDTO, "allocation_event",
evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
)
}
