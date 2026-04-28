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

	// Kill-switch pre-check (Phase 6): HALTED mode blocks new allocations.
	// DEGRADED mode reduces SizeUsd by the degraded_size_multiplier.
	sysMode := "BALANCED"
	if state, stateErr := w.adapter.GetSystemState(ctx); stateErr == nil && state != nil {
		sysMode = state.Mode
	}
	if sysMode == "HALTED" {
		w.logger.Info("capital_worker_halted",
			"mode", sysMode,
			"event_id", evt.EventID,
			"token_lifecycle_id", dto.TokenLifecycleID,
		)
		// Return an error so the event is NOT marked processed and can be
		// retried once the system exits HALTED mode.  A silent nil,nil would
		// permanently drop the selection_event and leave the lifecycle stuck
		// in SELECTED with no allocation or execution result.
		return nil, fmt.Errorf("capital_worker: system halted, allocation deferred for retry")
	}

	// Derive the chain for per-market isolation (arch §2.4).
	chain := chainFromCorrelation(ctx, w.adapter, evt.CorrelationID, w.logger)

	allocDTO, err := w.mod.Process(ctx, dto, chain)
	if err != nil {
		return nil, fmt.Errorf("capital_worker: module: %w", err)
	}

	// DEGRADED mode: reduce allocation size.
	if sysMode == "DEGRADED" && !allocDTO.Rejected {
		multiplier := w.cfg.Risk.DegradedSizeMultiplier
		if multiplier <= 0 {
			multiplier = 0.5
		}
		allocDTO.SizeUsd *= multiplier
		w.logger.Info("capital_worker_degraded_size_reduction",
			"original_size", allocDTO.SizeUsd/multiplier,
			"reduced_size", allocDTO.SizeUsd,
			"multiplier", multiplier,
		)
	}

	w.logger.Info("capital_allocated",
		"token", allocDTO.TokenAddress,
		"size_usd", allocDTO.SizeUsd,
		"chain", allocDTO.Chain,
		"cohort_id", allocDTO.CohortID,
		"rejected", allocDTO.Rejected,
		"trace_id", allocDTO.TraceID,
	)

	if err := w.adapter.InsertAllocation(ctx, allocDTO); err != nil {
		w.logger.Warn("capital_worker_persist_failed", "event_id", allocDTO.EventID, "error", err)
	}

	// No lifecycle transition here — lifecycle stays SELECTED until execution completes.

	return makeOutputEvent(
		allocDTO.EventID, allocDTO, "allocation_event",
		evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
	)
}
