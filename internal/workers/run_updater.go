package workers

import (
	"context"
	"encoding/json"
	"log/slog"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/learning"
)

// RunUpdater consumes evaluation_events, checks the sample gate, and proposes
// new strategy version candidates when conditions are met.
func RunUpdater(
	ctx context.Context,
	adapter database.Adapter,
	cfg *config.Config,
	logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}

	lcfg := &cfg.Learning
	updater := learning.NewUpdater(lcfg)

	evt, err := adapter.ClaimNextEvent(ctx, "updater_worker", []string{"evaluation_event"})
	if err != nil {
		return err
	}
	if evt == nil {
		return nil
	}

	var evalDTO contracts.EvaluationDTO
	if err := json.Unmarshal(evt.Payload, &evalDTO); err != nil {
		logger.Warn("updater_decode_failed", "event_id", evt.EventID, "error", err)
		_ = adapter.MarkEventProcessed(ctx, evt.EventID)
		return nil
	}

	activeVersion, err := adapter.GetActiveStrategy(ctx)
	if err != nil {
		logger.Warn("updater_no_active_version", "error", err)
		_ = adapter.ReleaseEventClaim(ctx, evt.EventID)
		return nil
	}

	newVersion, err := updater.ProposeVersion(ctx, activeVersion.ConfigSnapshot, activeVersion.StrategyVersionID, evalDTO, evt.TraceID)
	if err != nil {
		logger.Warn("updater_propose_failed", "error", err)
		_ = adapter.MarkEventProcessed(ctx, evt.EventID)
		return nil // non-fatal: insufficient samples or no update needed
	}

	dbVersion := database.StrategyVersion{
		StrategyVersionID: newVersion.StrategyVersionID,
		ConfigSnapshot:    newVersion.ConfigSnapshot,
		CreatedAt:         newVersion.CreatedAt,
		Status:            newVersion.Status,
		ParentVersionID:   newVersion.ParentVersionID,
	}

	if err := adapter.CreateStrategyVersion(ctx, dbVersion); err != nil {
		logger.Error("updater_create_version_failed", "version_id", dbVersion.StrategyVersionID, "error", err)
		_ = adapter.ReleaseEventClaim(ctx, evt.EventID)
		return err
	}

	logger.Info("strategy_version_proposed",
		"version_id", dbVersion.StrategyVersionID,
		"parent_version_id", dbVersion.ParentVersionID,
		"trace_id", evt.TraceID,
	)

	if err := adapter.SetStrategyVersionStatus(ctx, dbVersion.StrategyVersionID, "shadow", "updater_proposed"); err != nil {
		logger.Error("updater_set_shadow_failed", "version_id", dbVersion.StrategyVersionID, "error", err)
		_ = adapter.ReleaseEventClaim(ctx, evt.EventID)
		return err
	}

	outEvt, err := makeOutputEvent(
		dbVersion.StrategyVersionID, dbVersion, "strategy_version_event",
		evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
	)
	if err != nil {
		logger.Error("updater_event_build_failed", "error", err)
		_ = adapter.ReleaseEventClaim(ctx, evt.EventID)
		return err
	}

	if err := adapter.InsertEvent(ctx, *outEvt); err != nil {
		logger.Error("updater_event_insert_failed", "error", err)
		_ = adapter.ReleaseEventClaim(ctx, evt.EventID)
		return err
	}

	logger.Info("strategy_candidate_created",
		"version_id", dbVersion.StrategyVersionID,
		"parent_version_id", dbVersion.ParentVersionID,
	)
	return adapter.MarkEventProcessed(ctx, evt.EventID)
}
