package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/features"
)

// FeaturesWorker implements Layer 2: Feature Extraction.
// Consumes: data_quality_event → emits: feature_event
type FeaturesWorker struct {
	adapter database.Adapter
	mod     *features.Module
	logger  *slog.Logger
}

// NewFeaturesWorker returns a new FeaturesWorker.
func NewFeaturesWorker(adapter database.Adapter, cfg *config.Config, logger *slog.Logger) *FeaturesWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &FeaturesWorker{
		adapter: adapter,
		mod:     features.New(cfg),
		logger:  logger,
	}
}

func (w *FeaturesWorker) Process(ctx context.Context, evt *database.Event) (*database.Event, error) {
	var dto contracts.DataQualityDTO
	if err := json.Unmarshal(evt.Payload, &dto); err != nil {
		return nil, fmt.Errorf("features_worker: unmarshal: %w", err)
	}

	featDTO, err := w.mod.Process(ctx, dto)
	if err != nil {
		return nil, fmt.Errorf("features_worker: module: %w", err)
	}

	w.logger.Info("features_extracted",
		"token", featDTO.TokenAddress,
		"liquidity_score", featDTO.LiquidityScore,
		"volume_momentum", featDTO.VolumeMomentum,
		"contract_safety", featDTO.ContractSafety,
		"trace_id", featDTO.TraceID,
	)

	if err := w.adapter.InsertFeature(ctx, featDTO); err != nil {
		w.logger.Warn("features_worker_persist_failed", "event_id", featDTO.EventID, "error", err)
	}

	if err := doMandatoryTransition(ctx, w.adapter, dto.TokenLifecycleID, "DQ_PASSED", "FEATURE_READY", "", "features_worker"); err != nil {
		return nil, fmt.Errorf("features_worker: transition: %w", err)
	}

	return makeOutputEvent(
		featDTO.EventID, featDTO, "feature_event",
		evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
	)
}
