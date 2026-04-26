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
		mod:     data_quality.New(data_quality.DefaultConfig(cfg), logger),
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
	dqDTO, err := w.mod.Process(ctx, dto)
	if err != nil {
		return nil, fmt.Errorf("dq_worker: module: %w", err)
	}

	// Persist the result regardless of decision.
	if err := w.adapter.InsertDataQuality(ctx, dqDTO); err != nil {
		w.logger.Warn("dq_worker_persist_failed", "event_id", dqDTO.EventID, "error", err)
	}

	// Lifecycle transition: DETECTED → DQ_PASSED or REJECTED.
	nextState := "DQ_PASSED"
	if dqDTO.Decision == "REJECT" {
		nextState = "REJECTED"
	}
	if lc, ok := fetchLifecycle(ctx, w.adapter, lifecycleID, w.logger); ok {
		transitionBestEffort(ctx, w.adapter, database.TransitionRequest{
			LifecycleID:       lifecycleID,
			ExpectedFromState: "DETECTED",
			ExpectedVersion:   lc.StateVersion,
			NewState:          nextState,
			Reason:            dqDTO.Decision,
			ActorWorker:       "dq_worker",
		}, w.logger)
	}

	// Do not emit downstream event for rejections.
	if dqDTO.Decision == "REJECT" {
		return nil, nil
	}

	return makeOutputEvent(
		dqDTO.EventID, dqDTO, "data_quality_event",
		evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
	)
}
