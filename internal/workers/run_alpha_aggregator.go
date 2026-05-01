package workers

import (
	"context"
	"log/slog"
	"time"

	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/execution_quality"
)

// RunAlphaAggregator periodically derives per-market slippage α from
// realized fills and upserts to slippage_alpha_calibrations. Closes
// residual risk #3 (the GetSlippageAlpha stub).
//
// Cadence: every cfg.ExecutionQuality.Alpha.UpdateIntervalSec.
// Window:  4 × EwmaHalflifeSec of recent execution_results.
// Gating:  markets with sample_count < MinSampleCount are skipped (the
//
//	existing row, if any, is preserved as last-good calibration).
func RunAlphaAggregator(
	ctx context.Context,
	adapter database.Adapter,
	cfg *config.Config,
	logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}

	aggCfg := buildAlphaCfg(cfg)
	interval := time.Duration(aggCfg.UpdateIntervalSec) * time.Second
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	windowSec := aggCfg.EwmaHalflifeSec * 4
	if windowSec <= 0 {
		windowSec = 4 * 3600
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := runAlphaAggregatorOnce(ctx, adapter, aggCfg, windowSec, logger); err != nil {
				logger.Error("alpha_aggregator_cycle_failed", "error", err)
			}
		}
	}
}

// runAlphaAggregatorOnce executes a single aggregation cycle.
// Exposed for tests.
func runAlphaAggregatorOnce(
	ctx context.Context,
	adapter database.Adapter,
	cfg execution_quality.AlphaAggregatorConfig,
	windowSec int,
	logger *slog.Logger,
) error {
	rawByMarket, err := adapter.GetRealizedFillSamples(ctx, windowSec)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	for market, raw := range rawByMarket {
		samples := toModuleSamples(raw)
		alpha, ewmaPred, ewmaReal, n := execution_quality.ComputeMarketAlpha(samples, cfg, now)

		if n < cfg.MinSampleCount {
			logger.Debug("alpha_aggregator_below_min_samples",
				"market", market, "samples", n, "min", cfg.MinSampleCount,
			)
			continue
		}

		if err := adapter.UpsertSlippageAlpha(ctx, market, alpha, ewmaPred, ewmaReal, n); err != nil {
			logger.Warn("alpha_aggregator_upsert_failed",
				"market", market, "error", err,
			)
			continue
		}
		logger.Info("alpha_aggregator_updated",
			"market", market,
			"alpha", alpha,
			"samples", n,
			"ewma_predicted_bps", ewmaPred,
			"ewma_realized_bps", ewmaReal,
		)
	}
	return nil
}

func toModuleSamples(in []database.FillSample) []execution_quality.FillSample {
	out := make([]execution_quality.FillSample, len(in))
	for i, s := range in {
		out[i] = execution_quality.FillSample{
			PredictedBps: s.PredictedBps,
			RealizedBps:  s.RealizedBps,
			At:           s.At,
		}
	}
	return out
}

func buildAlphaCfg(cfg *config.Config) execution_quality.AlphaAggregatorConfig {
	y := cfg.ExecutionQuality.Alpha
	return execution_quality.AlphaAggregatorConfig{
		MinSampleCount:    y.MinSampleCount,
		AlphaMin:          y.AlphaMin,
		AlphaMax:          y.AlphaMax,
		EwmaHalflifeSec:   y.EwmaHalflifeSec,
		UpdateIntervalSec: y.UpdateIntervalSec,
	}
}
