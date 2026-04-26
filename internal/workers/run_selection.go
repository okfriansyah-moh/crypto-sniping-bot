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

	// Kill-switch pre-check (Phase 6): drop entry events when HALTED.
	state, stateErr := w.adapter.GetSystemState(ctx)
	if stateErr == nil && state != nil && state.Mode == "HALTED" {
		w.logger.Info("selection_worker_halted",
			"mode", state.Mode,
			"event_id", evt.EventID,
			"token_lifecycle_id", dto.TokenLifecycleID,
		)
		_ = doMandatoryTransition(ctx, w.adapter, dto.TokenLifecycleID, "VALIDATED", "REJECTED", "system_halted", "selection_worker")
		return nil, nil
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
	if err := doMandatoryTransition(ctx, w.adapter, dto.TokenLifecycleID, "VALIDATED", nextState, selDTO.RejectReason, "selection_worker"); err != nil {
		return nil, fmt.Errorf("selection_worker: transition: %w", err)
	}

	if !selDTO.Selected {
		return nil, nil
	}

	return makeOutputEvent(
		selDTO.EventID, selDTO, "selection_event",
		evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
	)
}
