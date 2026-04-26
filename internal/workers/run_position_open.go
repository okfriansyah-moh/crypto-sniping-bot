package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/position"
)

// PositionOpenWorker implements Layer 9 (open): Position Open.
// Consumes: execution_result_event → emits: position_state_event
type PositionOpenWorker struct {
	adapter database.Adapter
	mod     *position.Module
	cfg     *config.Config
	logger  *slog.Logger
}

// NewPositionOpenWorker returns a new PositionOpenWorker.
func NewPositionOpenWorker(adapter database.Adapter, cfg *config.Config, logger *slog.Logger) *PositionOpenWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &PositionOpenWorker{
		adapter: adapter,
		mod:     position.New(&cfg.Position),
		cfg:     cfg,
		logger:  logger,
	}
}

func (w *PositionOpenWorker) Process(ctx context.Context, evt *database.Event) (*database.Event, error) {
	var dto contracts.ExecutionResultDTO
	if err := json.Unmarshal(evt.Payload, &dto); err != nil {
		return nil, fmt.Errorf("position_open_worker: unmarshal: %w", err)
	}

	chain := chainFromCorrelation(ctx, w.adapter, evt.CorrelationID, w.logger)
	if chain == "" {
		chain = firstChain(w.cfg)
	}

	tokenAddress := ""
	if lc, err := w.adapter.GetLifecycle(ctx, dto.TokenLifecycleID); err == nil && lc != nil {
		tokenAddress = lc.TokenAddress
	}

	pos, err := w.mod.OpenPosition(ctx, dto, chain, tokenAddress)
	if err != nil {
		return nil, fmt.Errorf("position_open_worker: module: %w", err)
	}

	// Populate EntrySizeUsd from the AllocationDTO persisted earlier in the pipeline.
	// Without this, PnlUsd calculations and exposure risk controls are always zero.
	pos.EntrySizeUsd = allocationSizeFromCorrelation(ctx, w.adapter, evt.CorrelationID, w.logger)

	if err := w.adapter.InsertPositionState(ctx, pos); err != nil {
		w.logger.Warn("position_open_worker_persist_failed", "event_id", pos.EventID, "error", err)
	}

	nextState := "POSITION_OPEN"
	if pos.Status == "failed" {
		nextState = "FAILED"
	}
	if lc, ok := fetchLifecycle(ctx, w.adapter, dto.TokenLifecycleID, w.logger); ok {
		transitionBestEffort(ctx, w.adapter, database.TransitionRequest{
			LifecycleID:       dto.TokenLifecycleID,
			ExpectedFromState: "EXECUTED",
			ExpectedVersion:   lc.StateVersion,
			NewState:          nextState,
			ActorWorker:       "position_open_worker",
		}, w.logger)
	}

	return makeOutputEvent(
		pos.EventID, pos, "position_state_event",
		evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
	)
}
