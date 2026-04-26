package workers

import (
	"context"
	"log/slog"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/learning"
)

// RunRollbackWatchdog is a periodic worker that compares the currently promoted
// strategy against its baseline and initiates rollback when performance degrades.
func RunRollbackWatchdog(
	ctx context.Context,
	adapter database.Adapter,
	cfg *config.Config,
	logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}

	lcfg := &cfg.Learning
	rollbackThreshold := lcfg.RollbackThresholdPct
	if rollbackThreshold <= 0 {
		rollbackThreshold = 0.10
	}

	activeVersion, err := adapter.GetActiveStrategy(ctx)
	if err != nil {
		// No active version yet — nothing to watch.
		logger.Debug("rollback_watchdog_no_active_version", "error", err)
		return nil
	}

	// Only watch non-root versions (root has no parent to roll back to).
	if activeVersion.ParentVersionID == "" {
		return nil
	}

	windowSeconds := int64(lcfg.EvalWindowMinutes * 60)
	if windowSeconds <= 0 {
		windowSeconds = 3600
	}

	// Gather evaluations for the promoted version and its parent.
	promotedEvals, err := adapter.GetEvaluationsByVersion(ctx, activeVersion.StrategyVersionID)
	if err != nil || len(promotedEvals) == 0 {
		return nil
	}
	baselineEvals, err := adapter.GetEvaluationsByVersion(ctx, activeVersion.ParentVersionID)
	if err != nil || len(baselineEvals) == 0 {
		return nil
	}

	promoted := promotedEvals[len(promotedEvals)-1]
	baseline := baselineEvals[len(baselineEvals)-1]

	minSamples := lcfg.MinSampleSize
	if minSamples <= 0 {
		minSamples = 30
	}
	if int(promoted.SampleSize) < minSamples {
		return nil
	}

	// Check rollback condition: promoted Expectancy degraded by rollbackThreshold.
	shouldRollback := learning.ShouldRollback(promoted, baseline, rollbackThreshold)
	if !shouldRollback {
		return nil
	}

	logger.Warn("rollback_triggered",
		"active_version", activeVersion.StrategyVersionID,
		"parent_version", activeVersion.ParentVersionID,
		"promoted_expectancy", promoted.Expectancy,
		"baseline_expectancy", baseline.Expectancy,
	)

	if err := adapter.SetStrategyVersionStatus(ctx, activeVersion.StrategyVersionID, "rolled_back", "expectancy_degraded"); err != nil {
		logger.Error("rollback_set_status_failed", "error", err)
		return err
	}

	if err := adapter.SetStrategyVersionStatus(ctx, activeVersion.ParentVersionID, "active", "rollback_restore"); err != nil {
		logger.Error("rollback_restore_baseline_failed", "error", err)
		return err
	}

	rollbackPayload := map[string]string{
		"rolled_back_version": activeVersion.StrategyVersionID,
		"restored_version":    activeVersion.ParentVersionID,
		"reason":              "expectancy_degraded",
	}
	traceID := contracts.ContentIDFromString("rollback:" + activeVersion.StrategyVersionID)
	outEvt, err := makeOutputEvent(
		contracts.ContentIDFromString("rollback-evt:"+activeVersion.StrategyVersionID),
		rollbackPayload,
		"rollback_event",
		traceID, traceID, activeVersion.StrategyVersionID, activeVersion.StrategyVersionID,
	)
	if err != nil {
		logger.Error("rollback_event_build_failed", "error", err)
		return err
	}

	if err := adapter.InsertEvent(ctx, *outEvt); err != nil {
		logger.Error("rollback_event_insert_failed", "error", err)
		return err
	}

	return nil
}
