package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/evaluation"
	"crypto-sniping-bot/internal/resource_control"
)

// EvaluationWorker consumes position_event (Status=exited) and emits evaluation_event.
// This is the mandatory pre-learning gate: Learning Engine (Phase 5) MUST NOT run
// without evaluation_event as input.
type EvaluationWorker struct {
	adapter database.Adapter
	mod     *evaluation.Module
	weights config.EventPriorityWeights
	logger  *slog.Logger
}

// NewEvaluationWorker returns a new EvaluationWorker.
func NewEvaluationWorker(
	adapter database.Adapter,
	cfg *config.Config,
	logger *slog.Logger,
) *EvaluationWorker {
	if logger == nil {
		logger = slog.Default()
	}
	weights := resource_control.DefaultWeights()
	if cfg != nil {
		weights = cfg.EventWeights
	}
	evalCfg := config.EvaluationConfig{}
	if cfg != nil {
		evalCfg = cfg.Evaluation
	}
	return &EvaluationWorker{
		adapter: adapter,
		mod:     evaluation.New(evalCfg),
		weights: weights,
		logger:  logger,
	}
}

// Process handles a position_event. Skips non-exited positions.
func (w *EvaluationWorker) Process(ctx context.Context, evt *database.Event) (*database.Event, error) {
	var posDTO contracts.PositionStateDTO
	if err := json.Unmarshal(evt.Payload, &posDTO); err != nil {
		return nil, fmt.Errorf("evaluation_worker: unmarshal position: %w", err)
	}

	// Skip positions that have not exited yet.
	if posDTO.Status != "exited" {
		if err := w.adapter.MarkEventProcessed(ctx, evt.EventID); err != nil {
			w.logger.Warn("evaluation_worker_mark_processed_failed",
				"event_id", evt.EventID,
				"error", err,
			)
		}
		return nil, nil
	}

	// Fetch the ExecutionResultDTO for this lifecycle (best-effort — may not exist in tests).
	var execDTO *contracts.ExecutionResultDTO
	if posDTO.TokenLifecycleID != "" {
		var execErr error
		execDTO, execErr = w.adapter.GetExecutionByLifecycle(ctx, posDTO.TokenLifecycleID)
		if execErr != nil && execErr != database.ErrNotFound {
			w.logger.Warn("evaluation_worker_get_execution_failed",
				"lifecycle_id", posDTO.TokenLifecycleID,
				"error", execErr,
			)
		}
	}

	// Fetch shadow trades for the evaluation window.
	now := time.Now().UTC()
	windowEnd := now.Format(time.RFC3339Nano)
	windowStart := now.Add(-time.Hour).Format(time.RFC3339Nano) // 1h rolling window
	rawShadows, shadowErr := w.adapter.GetShadowTradesByWindow(ctx, windowStart, windowEnd)
	if shadowErr != nil {
		w.logger.Warn("evaluation_worker_get_shadows_failed",
			"error", shadowErr,
		)
		rawShadows = nil
	}

	// Convert to module-local type — keeps modules free of database imports.
	shadowInputs := make([]evaluation.ShadowTradeInput, len(rawShadows))
	for i, st := range rawShadows {
		shadowInputs[i] = evaluation.ShadowTradeInput{PeakGainPct: st.PeakGainPct}
	}

	// Compute evaluation (pure function).
	evalDTO, err := w.mod.Process(ctx, evaluation.EvaluationInput{
		Position:     posDTO,
		Execution:    execDTO,
		ShadowTrades: shadowInputs,
	})
	if err != nil {
		return nil, fmt.Errorf("evaluation_worker: module: %w", err)
	}

	// Mandatory CAS transition: POSITION_CLOSED → EVALUATED.
	if posDTO.TokenLifecycleID != "" {
		lc, lcErr := w.adapter.GetLifecycleByToken(ctx, posDTO.TokenAddress)
		if lcErr != nil {
			return nil, fmt.Errorf("evaluation_worker: get_lifecycle: %w", lcErr)
		}
		if err := w.adapter.TransitionState(ctx, database.TransitionRequest{
			LifecycleID:       lc.TokenLifecycleID,
			ExpectedFromState: "POSITION_CLOSED",
			ExpectedVersion:   lc.StateVersion,
			NewState:          "EVALUATED",
			Reason:            "evaluation_complete",
			ActorWorker:       "evaluation_worker",
		}); err != nil {
			return nil, fmt.Errorf("evaluation_worker: transition POSITION_CLOSED→EVALUATED: %w", err)
		}
	}

	// Persist evaluation.
	if err := w.adapter.InsertEvaluation(ctx, evalDTO); err != nil {
		w.logger.Warn("evaluation_worker_persist_failed",
			"evaluation_id", evalDTO.EvaluationID,
			"error", err,
		)
	}

	// Compute priority for the output event.
	evalDTO.Priority = resource_control.ComputePriority(
		"position_event", true, time.Time{}, now, w.weights,
	)

	outEvt, err := makeOutputEvent(
		evalDTO.EventID, evalDTO, "evaluation_event",
		evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
	)
	if err != nil {
		return nil, fmt.Errorf("evaluation_worker: make_output: %w", err)
	}
	outEvt.Priority = int(evalDTO.Priority)
	return outEvt, nil
}
