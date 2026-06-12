package workers

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/learning"
)

// RunABPromoter evaluates the shadow strategy candidate against the active
// baseline and promotes when ABPromoter.ShouldPromote passes (docs/PLAN.md Task 5).
func RunABPromoter(
	ctx context.Context,
	adapter database.Adapter,
	cfg *config.Config,
	logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}

	shadow, err := adapter.GetShadowStrategy(ctx)
	if err != nil {
		return err
	}
	if shadow == nil {
		return nil
	}

	if !shadowVersionReady(shadow, cfg.Learning.ShadowWindowMinutes) {
		logger.Debug("ab_promoter_shadow_window_open",
			"candidate_version", shadow.StrategyVersionID,
		)
		return nil
	}

	active, err := adapter.GetActiveStrategy(ctx)
	if err != nil {
		logger.Debug("ab_promoter_no_active_version", "error", err)
		return nil
	}
	if active.StrategyVersionID == shadow.StrategyVersionID {
		return nil
	}

	end := time.Now().UTC()
	lookbackSec := cfg.Learning.EvalWindowSeconds
	if lookbackSec <= 0 {
		lookbackSec = 86400
	}
	start := end.Add(-time.Duration(lookbackSec) * time.Second)

	evaluator := learning.NewEvaluator()

	shadowRecords, err := adapter.GetLearningRecordsByWindow(ctx, shadow.StrategyVersionID, start, end)
	if err != nil {
		return err
	}
	candidateEval, err := evaluator.EvaluateWindow(ctx, shadow.StrategyVersionID, start, end, shadowRecords)
	if err != nil {
		logger.Warn("ab_promoter_candidate_eval_failed", "error", err)
		return nil
	}

	baselineRecords, err := adapter.GetLearningRecordsByWindow(ctx, active.StrategyVersionID, start, end)
	if err != nil {
		return err
	}
	baselineEval, err := evaluator.EvaluateWindow(ctx, active.StrategyVersionID, start, end, baselineRecords)
	if err != nil {
		logger.Warn("ab_promoter_baseline_eval_failed", "error", err)
		return nil
	}

	promoter := learning.NewABPromoter(&cfg.Learning)
	promote, reason, err := promoter.ShouldPromote(ctx, candidateEval, baselineEval)
	if err != nil {
		return err
	}
	if !promote {
		logger.Info("ab_promoter_skip",
			"candidate_version", shadow.StrategyVersionID,
			"baseline_version", active.StrategyVersionID,
			"reason", reason,
		)
		return nil
	}

	drainTimeout := cfg.Hardening.DrainTimeoutSec
	if drainTimeout <= 0 {
		drainTimeout = 60
	}

	if err := adapter.PromoteStrategyVersion(ctx, shadow.StrategyVersionID, drainTimeout); err != nil {
		if errors.Is(err, database.ErrDrainTimeout) {
			logger.Warn("ab_promoter_drain_timeout",
				"candidate_version", shadow.StrategyVersionID,
				"drain_timeout_sec", drainTimeout,
			)
			return nil
		}
		return err
	}

	logger.Info("strategy_version_promoted",
		"from_version", active.StrategyVersionID,
		"to_version", shadow.StrategyVersionID,
		"candidate_expectancy", candidateEval.Expectancy,
		"baseline_expectancy", baselineEval.Expectancy,
		"reason", reason,
	)

	payload := map[string]string{
		"action":          "promote",
		"from_version_id": active.StrategyVersionID,
		"to_version_id":   shadow.StrategyVersionID,
		"reason":          reason,
	}
	traceID := contracts.ContentIDFromString("promote:" + shadow.StrategyVersionID)
	outEvt, err := makeOutputEvent(
		contracts.ContentIDFromString("promotion-evt:"+shadow.StrategyVersionID),
		payload,
		"strategy_promotion_event",
		traceID, traceID, active.StrategyVersionID, shadow.StrategyVersionID,
	)
	if err != nil {
		logger.Error("ab_promoter_event_build_failed", "error", err)
		return err
	}

	if err := adapter.InsertEvent(ctx, *outEvt); err != nil {
		logger.Error("ab_promoter_event_insert_failed", "error", err)
		return err
	}

	return nil
}

func shadowVersionReady(shadow *database.StrategyVersion, windowMinutes int) bool {
	if shadow == nil {
		return false
	}
	if windowMinutes <= 0 {
		windowMinutes = 60
	}
	ts := shadow.CreatedAt
	if shadow.ShadowStartedAt != nil && *shadow.ShadowStartedAt != "" {
		ts = *shadow.ShadowStartedAt
	}
	started, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		started, err = time.Parse(time.RFC3339, ts)
	}
	if err != nil {
		return true
	}
	return time.Since(started) >= time.Duration(windowMinutes)*time.Minute
}
