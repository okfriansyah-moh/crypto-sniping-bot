package workers

import (
	"context"
	"log/slog"
	"time"

	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/sniper-bot/internal/modules/learning"
)

// RunEvaluator is a periodic worker that aggregates LearningRecords into
// an EvaluationDTO and publishes an evaluation_event.
func RunEvaluator(
	ctx context.Context,
	adapter database.Adapter,
	cfg *config.Config,
	logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}

	lcfg := &cfg.Learning
	evaluator := learning.NewEvaluator()

	windowMinutes := lcfg.EvalWindowMinutes
	if windowMinutes <= 0 {
		windowMinutes = 60
	}

	activeVersion, err := adapter.GetActiveStrategy(ctx)
	if err != nil {
		logger.Warn("evaluator_no_active_version", "error", err)
		return nil
	}

	end := time.Now().UTC()
	start := end.Add(-time.Duration(windowMinutes) * time.Minute)

	records, err := adapter.GetLearningRecordsByWindow(ctx, activeVersion.StrategyVersionID, start, end)
	if err != nil {
		return err
	}

	evalDTO, err := evaluator.EvaluateWindow(ctx, activeVersion.StrategyVersionID, start, end, records)
	if err != nil {
		logger.Error("evaluator_compute_failed", "error", err)
		return err
	}

	if err := adapter.InsertEvaluation(ctx, evalDTO); err != nil {
		logger.Error("evaluator_persist_failed", "eval_id", evalDTO.EvaluationID, "error", err)
		return err
	}

	outEvt, err := makeOutputEvent(
		evalDTO.EvaluationID, evalDTO, "evaluation_event",
		evalDTO.TraceID, evalDTO.CorrelationID, "", evalDTO.VersionID,
	)
	if err != nil {
		logger.Error("evaluator_event_build_failed", "error", err)
		return err
	}

	if err := adapter.InsertEvent(ctx, *outEvt); err != nil {
		logger.Error("evaluator_event_insert_failed", "error", err)
		return err
	}

	logger.Info("evaluation_emitted",
		"eval_id", evalDTO.EvaluationID,
		"sample_size", evalDTO.SampleSize,
		"expectancy", evalDTO.Expectancy,
	)
	return nil
}
