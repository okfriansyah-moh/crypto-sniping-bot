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

// RunLearningRecord is triggered by "position_state_event" with Status=exited.
// It emits one learning_record_event (shadow=false) per exited position.
func RunLearningRecord(
	ctx context.Context,
	adapter database.Adapter,
	cfg *config.Config,
	logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}

	recorder := learning.NewRecorder()

	evt, err := adapter.ClaimNextEvent(ctx, "learning_recorder_worker", []string{"position_state_event"})
	if err != nil {
		return err
	}
	if evt == nil {
		return nil
	}

	var pos contracts.PositionStateDTO
	if err := json.Unmarshal(evt.Payload, &pos); err != nil {
		logger.Warn("learning_recorder_decode_failed", "event_id", evt.EventID, "error", err)
		_ = adapter.MarkEventProcessed(ctx, evt.EventID)
		return nil
	}

	// Only process exited positions.
	if pos.Status != "exited" {
		return adapter.MarkEventProcessed(ctx, evt.EventID)
	}

	activeVersion, err := adapter.GetActiveStrategy(ctx)
	if err != nil {
		logger.Warn("learning_recorder_no_active_version", "error", err)
		_ = adapter.ReleaseEventClaim(ctx, evt.EventID)
		return nil
	}

	strategyStatus := "active"
	lrDTO, err := recorder.RecordExecuted(ctx, pos, evt.EventID, activeVersion.StrategyVersionID, strategyStatus)
	if err != nil {
		logger.Error("learning_recorder_record_failed", "event_id", evt.EventID, "error", err)
		_ = adapter.ReleaseEventClaim(ctx, evt.EventID)
		return nil
	}

	logger.Info("learning_record_created",
		"record_id", lrDTO.RecordID,
		"outcome", lrDTO.Outcome,
		"classification", lrDTO.Classification,
		"pnl_pct", lrDTO.PnlPct,
		"shadow", lrDTO.Shadow,
		"trace_id", lrDTO.TraceID,
	)

	if err := adapter.InsertLearningRecord(ctx, lrDTO); err != nil {
		logger.Error("learning_recorder_persist_failed", "event_id", evt.EventID, "error", err)
		_ = adapter.ReleaseEventClaim(ctx, evt.EventID)
		return err
	}

	outEvt, err := makeOutputEvent(
		lrDTO.EventID, lrDTO, "learning_record_event",
		lrDTO.TraceID, lrDTO.CorrelationID, evt.EventID, lrDTO.VersionID,
	)
	if err != nil {
		logger.Error("learning_recorder_event_build_failed", "error", err)
		_ = adapter.ReleaseEventClaim(ctx, evt.EventID)
		return err
	}

	if err := adapter.InsertEvent(ctx, *outEvt); err != nil {
		logger.Error("learning_recorder_event_insert_failed", "error", err)
		_ = adapter.ReleaseEventClaim(ctx, evt.EventID)
		return err
	}

	logger.Info("learning_record_emitted",
		"record_id", lrDTO.RecordID,
		"classification", lrDTO.Classification,
		"pnl_pct", lrDTO.PnlPct,
	)

	return adapter.MarkEventProcessed(ctx, evt.EventID)
}
