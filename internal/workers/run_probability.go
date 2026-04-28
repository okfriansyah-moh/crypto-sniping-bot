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

// ProbabilityWorker implements Layer 4: Probability Model.
// Consumes: feature_event → emits: probability_event.
type ProbabilityWorker struct {
	adapter database.Adapter
	model   *models.ProbabilityModel
	logger  *slog.Logger
}

// NewProbabilityWorker constructs a ProbabilityWorker using the configured logistic coefficients.
func NewProbabilityWorker(adapter database.Adapter, cfg *config.Config, logger *slog.Logger) *ProbabilityWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &ProbabilityWorker{
		adapter: adapter,
		model:   models.NewProbabilityModel(probabilityCfgFromConfig(cfg)),
		logger:  logger,
	}
}

// Process scores a FeatureDTO and emits a probability_event. Pure routing —
// the model itself is in `internal/modules/models`.
func (w *ProbabilityWorker) Process(ctx context.Context, evt *database.Event) (*database.Event, error) {
	var feat contracts.FeatureDTO
	if err := json.Unmarshal(evt.Payload, &feat); err != nil {
		return nil, fmt.Errorf("probability_worker: unmarshal: %w", err)
	}

	prob, err := w.model.Predict(ctx, feat)
	if err != nil {
		w.logger.Warn("probability_worker_invalid",
			"event_id", evt.EventID, "trace_id", evt.TraceID, "error", err)
		return nil, nil // skip (rejected upstream by validator if missing)
	}

	w.logger.Info("probability_scored",
		"event_id", prob.EventID,
		"probability", prob.Probability,
		"trace_id", prob.TraceID,
	)

	if err := w.adapter.InsertProbabilityEstimate(ctx, prob); err != nil {
		w.logger.Warn("probability_worker_persist_failed",
			"event_id", prob.EventID, "error", err)
	}

	return makeOutputEvent(
		prob.EventID, prob, "probability_event",
		evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
	)
}

// probabilityCfgFromConfig builds models.LogisticConfig from the YAML-loaded config.
// Falls back to safe defaults when keys are absent.
func probabilityCfgFromConfig(cfg *config.Config) models.LogisticConfig {
	defaults := models.DefaultLogisticConfig()
	if cfg == nil {
		return defaults
	}
	c := cfg.Models.Probability
	if c.ModelVersionID == "" {
		return defaults
	}
	return models.LogisticConfig{
		Bias:                c.Bias,
		WLiquidityScore:     c.WLiquidityScore,
		WTxVelocityScore:    c.WTxVelocityScore,
		WHolderDistribution: c.WHolderDistribution,
		WWalletEntropy:      c.WWalletEntropy,
		WContractSafety:     c.WContractSafety,
		WTokenAge:           c.WTokenAge,
		WVolumeMomentum:     c.WVolumeMomentum,
		WPriceMomentum:      c.WPriceMomentum,
		ModelVersionID:      c.ModelVersionID,
		BrierCalibration:    c.BrierCalibration,
		MinProbability:      defaults.MinProbability,
		MaxProbability:      defaults.MaxProbability,
	}
}
