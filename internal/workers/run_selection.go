package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/selection"
)

// SelectionWorker implements Layer 6: Selection Engine.
// Consumes: validated_edge_event → emits: selection_event (Selected only)
type SelectionWorker struct {
	adapter database.Adapter
	mod     *selection.Module
	logger  *slog.Logger
}

// NewSelectionWorker returns a new SelectionWorker.
func NewSelectionWorker(adapter database.Adapter, cfg *config.Config, logger *slog.Logger) *SelectionWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &SelectionWorker{
		adapter: adapter,
		mod:     selection.New(&cfg.Selection),
		logger:  logger,
	}
}

func (w *SelectionWorker) Process(ctx context.Context, evt *database.Event) (*database.Event, error) {
	var dto contracts.ValidatedEdgeDTO
	if err := json.Unmarshal(evt.Payload, &dto); err != nil {
		return nil, fmt.Errorf("selection_worker: unmarshal: %w", err)
	}

	openPositions, err := w.adapter.GetOpenPositions(ctx)
	if err != nil {
		w.logger.Warn("selection_worker_open_positions_failed", "error", err)
		openPositions = nil
	}

	// Derive the chain for per-market isolation (arch §2.4).
	// ValidatedEdgeDTO doesn't carry Chain, so we look it up from the event log.
	chain := chainFromCorrelation(ctx, w.adapter, evt.CorrelationID, w.logger)

	// Count only positions on the same chain/market so that a position on one
	// market does not block selection on another.
	chainOpenCount := 0
	for _, p := range openPositions {
		if p.Chain == chain {
			chainOpenCount++
		}
	}

	selDTO, err := w.mod.Process(ctx, dto, chainOpenCount)
	if err != nil {
		return nil, fmt.Errorf("selection_worker: module: %w", err)
	}

	if err := w.adapter.InsertSelection(ctx, selDTO); err != nil {
		w.logger.Warn("selection_worker_persist_failed", "event_id", selDTO.EventID, "error", err)
	}

	nextState := "SELECTED"
	if !selDTO.Selected {
		nextState = "REJECTED"
	}
	if lc, ok := fetchLifecycle(ctx, w.adapter, dto.TokenLifecycleID, w.logger); ok {
		transitionBestEffort(ctx, w.adapter, database.TransitionRequest{
			LifecycleID:       dto.TokenLifecycleID,
			ExpectedFromState: "VALIDATED",
			ExpectedVersion:   lc.StateVersion,
			NewState:          nextState,
			Reason:            selDTO.RejectReason,
			ActorWorker:       "selection_worker",
		}, w.logger)
	}

	if !selDTO.Selected {
		return nil, nil
	}

	return makeOutputEvent(
		selDTO.EventID, selDTO, "selection_event",
		evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
	)
}
