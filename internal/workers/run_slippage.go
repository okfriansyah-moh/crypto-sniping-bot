package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/models"
)

// SlippageWorker implements Layer 4: Slippage Model.
// Consumes: feature_event → emits: slippage_event.
type SlippageWorker struct {
	adapter     database.Adapter
	model       *models.SlippageModel
	defaultSize float64
	logger      *slog.Logger
}

// NewSlippageWorker constructs a SlippageWorker using the configured CPMM
// model. The model resolves α via the adapter (no-op default = 1.0 until
// the realized-fill aggregator is wired).
func NewSlippageWorker(adapter database.Adapter, cfg *config.Config, logger *slog.Logger) *SlippageWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &SlippageWorker{
		adapter:     adapter,
		model:       models.NewSlippageModelWithAlpha(slippageCfgFromConfig(cfg), adapter),
		defaultSize: cfg.Capital.FixedEntrySizeUsd,
		logger:      logger,
	}
}

// Process estimates slippage for a feature event using the proposed entry size.
func (w *SlippageWorker) Process(ctx context.Context, evt *database.Event) (*database.Event, error) {
	var feat contracts.FeatureDTO
	if err := json.Unmarshal(evt.Payload, &feat); err != nil {
		return nil, fmt.Errorf("slippage_worker: unmarshal: %w", err)
	}

	size := w.defaultSize
	if size <= 0 {
		size = 50.0
	}

	slip, err := w.model.EstimateForMarket(ctx, feat, size, feat.Market)
	if err != nil {
		w.logger.Warn("slippage_worker_estimate_failed",
			"event_id", evt.EventID, "error", err)
		return nil, nil
	}

	w.logger.Info("slippage_estimated",
		"event_id", slip.EventID,
		"p50_bps", slip.ExpectedP50Bps,
		"p95_bps", slip.ExpectedP95Bps,
		"trace_id", slip.TraceID,
	)

	if err := w.adapter.InsertSlippageEstimate(ctx, slip); err != nil {
		w.logger.Warn("slippage_worker_persist_failed",
			"event_id", slip.EventID, "error", err)
	}

	return makeOutputEvent(
		slip.EventID, slip, "slippage_event",
		evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
	)
}

// slippageCfgFromConfig builds models.SlippageConfig from YAML config.
// Empty / zero values fall back to the model's safe defaults
// (see models.DefaultSlippageConfig).
func slippageCfgFromConfig(cfg *config.Config) models.SlippageConfig {
	defaults := models.DefaultSlippageConfig()
	if cfg == nil {
		return defaults
	}
	src := cfg.Models.Slippage
	out := models.SlippageConfig{
		MaxSlippageBps: src.MaxSlippageBps,
		VolatilityZ:    src.VolatilityZ,
		TailBps:        src.TailBps,
		MinReserveUsd:  src.MinReserveUsd,
		DefaultAlpha:   src.DefaultAlpha,
		MaxAlpha:       src.MaxAlpha,
		ModelVersionID: src.ModelVersionID,
		FallbackP50Bps: src.FallbackP50Bps,
		FallbackP95Bps: src.FallbackP95Bps,
	}
	if out.ModelVersionID == "" {
		out.ModelVersionID = defaults.ModelVersionID
	}
	for _, b := range src.Buckets {
		out.Buckets = append(out.Buckets, models.SlippageBucket{
			LiquidityMaxUsd: b.LiquidityMaxUsd,
			SizeMaxUsd:      b.SizeMaxUsd,
			P50Bps:          b.P50Bps,
			P95Bps:          b.P95Bps,
		})
	}
	return out
}
