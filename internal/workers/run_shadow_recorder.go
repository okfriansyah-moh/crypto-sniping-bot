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

// rejectionDecision extracts the decision field from known rejection event payloads.
// Returns ("", false) when the event is not a rejection.
func rejectionDecision(eventType string, payload []byte) (tokenAddress, lifecycleID, stage string, isRejection bool) {
	switch eventType {
	case "data_quality_event":
		var dto contracts.DataQualityDTO
		if err := json.Unmarshal(payload, &dto); err != nil {
			return "", "", "", false
		}
		if dto.Decision != "REJECT" {
			return "", "", "", false
		}
		return dto.TokenAddress, dto.TokenLifecycleID, "data_quality", true

	case "edge_event":
		var dto contracts.EdgeDTO
		if err := json.Unmarshal(payload, &dto); err != nil {
			return "", "", "", false
		}
		// EdgeDTO has no explicit Decision field — low edge_strength signals rejection.
		if dto.EdgeStrength >= 0.3 {
			return "", "", "", false
		}
		return dto.TokenAddress, dto.TokenLifecycleID, "edge", true

	case "validated_edge_event":
		var dto contracts.ValidatedEdgeDTO
		if err := json.Unmarshal(payload, &dto); err != nil {
			return "", "", "", false
		}
		if dto.Decision != "REJECT" {
			return "", "", "", false
		}
		return dto.TokenAddress, dto.TokenLifecycleID, "validated_edge", true

	case "selection_event":
		var dto contracts.SelectionOutputDTO
		if err := json.Unmarshal(payload, &dto); err != nil {
			return "", "", "", false
		}
		if dto.Selected {
			return "", "", "", false
		}
		return dto.TokenAddress, dto.TokenLifecycleID, "selection", true
	}
	return "", "", "", false
}

// RunShadowRecorder is triggered by rejection events and emits shadow LearningRecordDTOs.
func RunShadowRecorder(
	ctx context.Context,
	adapter database.Adapter,
	cfg *config.Config,
	logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}

	recorder := learning.NewShadowRecorder()

	rejectionEventTypes := []string{
		"data_quality_event",
		"edge_event",
		"validated_edge_event",
		"selection_event",
	}

	evt, err := adapter.ClaimNextEvent(ctx, "shadow_recorder_worker", rejectionEventTypes)
	if err != nil {
		return err
	}
	if evt == nil {
		return nil
	}

	tokenAddress, lifecycleID, stage, isRejection := rejectionDecision(evt.EventType, evt.Payload)
	if !isRejection {
		// Not our event — release the claim so the stage worker (e.g. features_worker
		// for PASS data_quality_event) can process it. Marking processed here would
		// permanently skip downstream layers (single shared processed flag).
		return adapter.ReleaseEventClaim(ctx, evt.EventID)
	}

	activeVersion, err := adapter.GetActiveStrategy(ctx)
	if err != nil {
		logger.Warn("shadow_recorder_no_active_version", "error", err)
		_ = adapter.ReleaseEventClaim(ctx, evt.EventID)
		return nil
	}

	lrDTO, sp, err := recorder.RecordRejection(ctx, stage, tokenAddress, lifecycleID,
		evt.EventID, activeVersion.StrategyVersionID, "active")
	if err != nil {
		logger.Error("shadow_recorder_record_failed", "event_id", evt.EventID, "error", err)
		_ = adapter.ReleaseEventClaim(ctx, evt.EventID)
		return nil
	}

	if err := adapter.InsertLearningRecord(ctx, lrDTO); err != nil {
		logger.Error("shadow_recorder_persist_lr_failed", "error", err)
		_ = adapter.ReleaseEventClaim(ctx, evt.EventID)
		return err
	}

	st := database.ShadowTrade{
		ShadowID:            sp.ShadowID,
		TokenAddress:        sp.TokenAddress,
		Stage:               sp.Stage,
		RejectedAt:          sp.RejectedAt,
		ObservationComplete: sp.ObservationComplete,
		ObservedReturnPct:   sp.ObservedReturnPct,
		Classification:      sp.Classification,
		LearningRecordID:    sp.LearningRecordID,
		VersionID:           sp.VersionID,
	}
	if err := adapter.InsertShadowTrade(ctx, st); err != nil {
		logger.Warn("shadow_recorder_persist_st_failed", "shadow_id", st.ShadowID, "error", err)
		// Non-fatal: learning record is persisted; shadow trade is best-effort.
	}

	outEvt, err := makeOutputEvent(
		lrDTO.EventID, lrDTO, "learning_record_event",
		lrDTO.TraceID, lrDTO.CorrelationID, evt.EventID, lrDTO.VersionID,
	)
	if err != nil {
		logger.Error("shadow_recorder_event_build_failed", "error", err)
		_ = adapter.ReleaseEventClaim(ctx, evt.EventID)
		return err
	}

	if err := adapter.InsertEvent(ctx, *outEvt); err != nil {
		logger.Error("shadow_recorder_event_insert_failed", "error", err)
		_ = adapter.ReleaseEventClaim(ctx, evt.EventID)
		return err
	}

	logger.Info("shadow_record_emitted",
		"shadow_id", st.ShadowID,
		"stage", stage,
		"token", tokenAddress,
	)

	return adapter.MarkEventProcessed(ctx, evt.EventID)
}
