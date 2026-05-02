package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/data_quality"
	"crypto-sniping-bot/internal/orchestrator"
)

// DataQualityWorker implements orchestrator.StageHandler for Layer 1.
// Consumes: market_data_event → emits: data_quality_event (PASS/RISKY_PASS only)
type DataQualityWorker struct {
	adapter database.Adapter
	mod     *data_quality.Module
	logger  *slog.Logger
}

// NewDataQualityWorker returns a new DataQualityWorker.
func NewDataQualityWorker(adapter database.Adapter, cfg *config.Config, logger *slog.Logger) *DataQualityWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &DataQualityWorker{
		adapter: adapter,
		mod:     data_quality.New(data_quality.DefaultConfig(cfg), logger).WithRuntimeConfig(&cfg.DataQualityRuntime),
		logger:  logger,
	}
}

// Process decodes a market_data_event, runs data quality checks, persists the result,
// and emits a data_quality_event if the token passes.
func (w *DataQualityWorker) Process(ctx context.Context, evt *database.Event) (*database.Event, error) {
	var dto contracts.MarketDataDTO
	if err := json.Unmarshal(evt.Payload, &dto); err != nil {
		return nil, fmt.Errorf("dq_worker: unmarshal: %w", err)
	}

	// Validate token address before creating lifecycle.
	// An empty address cannot be uniquely keyed in the lifecycle table and would
	// cause unrelated invalid events to share the same lifecycle row.
	if dto.TokenAddress == "" {
		return nil, fmt.Errorf("dq_worker: empty token address in market_data_event %s", evt.EventID)
	}

	// Ensure lifecycle exists.
	lifecycleID, err := w.adapter.StartLifecycle(ctx, dto)
	if err != nil {
		return nil, fmt.Errorf("dq_worker: start_lifecycle: %w", err)
	}

	// Run data quality module (pure function).
	// Read active operational mode from SystemState — STRICT/BALANCED/
	// EXPLORATION select the per-mode threshold profile inside the module.
	// On error or absent state, the module collapses unknown values onto
	// STRICT (conservative).
	sysMode := "BALANCED"
	if state, stateErr := w.adapter.GetSystemState(ctx); stateErr == nil && state != nil && state.Mode != "" {
		sysMode = state.Mode
	}

	dqDTO, err := w.mod.ProcessForMode(ctx, dto, sysMode)
	if err != nil {
		return nil, fmt.Errorf("dq_worker: module: %w", err)
	}

	w.logger.Info("dq_decision",
		"token", dqDTO.TokenAddress,
		"decision", dqDTO.Decision,
		"risk_score", dqDTO.RiskScore,
		"profile", dqDTO.Profile,
		"flags", dqDTO.Flags,
		"trace_id", dqDTO.TraceID,
		"version_id", dqDTO.VersionID,
	)

	// Persist the result regardless of decision.
	if err := w.adapter.InsertDataQuality(ctx, dqDTO); err != nil {
		w.logger.Warn("dq_worker_persist_failed", "event_id", dqDTO.EventID, "error", err)
	}

	// Lifecycle transition: DETECTED → DQ_PASSED or REJECTED (mandatory Phase 3 CAS).
	nextState := "DQ_PASSED"
	if dqDTO.Decision == "REJECT" {
		nextState = "REJECTED"
	}
	if err := doMandatoryTransition(ctx, w.adapter, lifecycleID, "DETECTED", nextState, dqDTO.Decision, "dq_worker"); err != nil {
		return nil, fmt.Errorf("dq_worker: transition: %w", err)
	}

	// Do not emit downstream event for rejections.
	if dqDTO.Decision == "REJECT" {
		orchestrator.RecordDecision(ctx, orchestrator.StageStatusRejected, dqRejectReason(dqDTO))
		return nil, nil
	}

	return makeOutputEvent(
		dqDTO.EventID, dqDTO, "data_quality_event",
		evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
	)
}
