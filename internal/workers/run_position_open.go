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

	// Exactly-once position creation (architecture.md § 4.10.E, certification § E).
	// UpsertPositionFromExecution uses ON CONFLICT (source_execution_id) DO NOTHING
	// keyed on pos.ExecutionID, guaranteeing 1:1 binding between execution_id and
	// open position. Re-delivery of the same execution_result_event yields
	// created=false; we then suppress the lifecycle transition, but still re-emit
	// the downstream position_state_event idempotently so the original handler's
	// outputs remain recoverable across the crash window.
	created, persistErr := w.adapter.UpsertPositionFromExecution(ctx, pos)
	if persistErr != nil {
		return nil, fmt.Errorf("position_open_worker: persist: %w", persistErr)
	}
	if !created {
		// Duplicate redelivery: the prior delivery already persisted the position
		// row and transitioned the lifecycle. We must NOT call doMandatoryTransition
		// again (CAS would fail since lifecycle is no longer EXECUTED). However,
		// we MUST re-emit the deterministically-derived position_state_event so a
		// crash between UpsertPositionFromExecution and the outer InsertEvent
		// (worker.go) cannot leave position monitoring permanently un-started.
		// pos.EventID is content-addressable, InsertEvent is idempotent via
		// ON CONFLICT (event_id), so re-emission is safe.
		w.logger.Info("position_open_worker_duplicate_reemit",
			"execution_id", pos.ExecutionID,
			"event_id", pos.EventID,
			"trace_id", evt.TraceID,
			"correlation_id", evt.CorrelationID,
		)
		return makeOutputEvent(
			pos.EventID, pos, "position_state_event",
			evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
		)
	}

	nextState := "POSITION_OPEN"
	if pos.Status == "failed" {
		nextState = "FAILED"
	}
	if err := doMandatoryTransition(ctx, w.adapter, dto.TokenLifecycleID, "EXECUTED", nextState, "", "position_open_worker"); err != nil {
		return nil, fmt.Errorf("position_open_worker: transition: %w", err)
	}

	w.logger.Info("position_opened",
		"token", pos.TokenAddress,
		"chain", pos.Chain,
		"entry_price", pos.EntryPrice,
		"tp1_bps", pos.Tp1Bps,
		"tp2_bps", pos.Tp2Bps,
		"sl_bps", pos.SlBps,
		"status", pos.Status,
		"trace_id", pos.TraceID,
	)

	return makeOutputEvent(
		pos.EventID, pos, "position_state_event",
		evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
	)
}
