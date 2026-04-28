package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/validation"
)

// ValidationWorker implements Layer 5: Edge Validation (EV gate).
// Consumes: edge_event → emits: validated_edge_event (ACCEPT only)
type ValidationWorker struct {
	adapter database.Adapter
	mod     *validation.Module
	logger  *slog.Logger
}

// NewValidationWorker returns a new ValidationWorker.
func NewValidationWorker(adapter database.Adapter, cfg *config.Config, logger *slog.Logger) *ValidationWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &ValidationWorker{
		adapter: adapter,
		mod:     validation.New(&cfg.Validation),
		logger:  logger,
	}
}

func (w *ValidationWorker) Process(ctx context.Context, evt *database.Event) (*database.Event, error) {
	var dto contracts.EdgeDTO
	if err := json.Unmarshal(evt.Payload, &dto); err != nil {
		return nil, fmt.Errorf("validation_worker: unmarshal: %w", err)
	}

	prob, slip, lat := w.fetchEstimates(ctx, evt.TraceID, evt.CorrelationID)
	vedge, err := w.mod.ProcessWithEstimates(ctx, dto, prob, slip, lat)
	if err != nil {
		return nil, fmt.Errorf("validation_worker: module: %w", err)
	}

	w.logger.Info("validation_decision",
		"token", vedge.TokenAddress,
		"decision", vedge.Decision,
		"ev_bps", vedge.ExpectedValueBps,
		"probability_used", vedge.ProbabilityUsed,
		"reject_reason", vedge.RejectReason,
		"trace_id", vedge.TraceID,
	)

	if err := w.adapter.InsertValidatedEdge(ctx, vedge); err != nil {
		w.logger.Warn("validation_worker_persist_failed", "event_id", vedge.EventID, "error", err)
	}

	nextState := "VALIDATED"
	if vedge.Decision != "ACCEPT" {
		nextState = "REJECTED"
	}
	if err := doMandatoryTransition(ctx, w.adapter, dto.TokenLifecycleID, "EDGE_DETECTED", nextState, vedge.RejectReason, "validation_worker"); err != nil {
		return nil, fmt.Errorf("validation_worker: transition: %w", err)
	}

	if vedge.Decision != "ACCEPT" {
		return nil, nil
	}

	return makeOutputEvent(
		vedge.EventID, vedge, "validated_edge_event",
		evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
	)
}

// fetchEstimates returns the latest model estimates for this trace.  Any of the
// returned values may be nil — in that case the validation module will fall
// back to its configured priors.  All errors are logged and treated as nil.
func (w *ValidationWorker) fetchEstimates(
	ctx context.Context,
	traceID, correlationID string,
) (*contracts.ProbabilityEstimateDTO, *contracts.SlippageEstimateDTO, *contracts.LatencyProfileDTO) {
	var (
		prob *contracts.ProbabilityEstimateDTO
		slip *contracts.SlippageEstimateDTO
		lat  *contracts.LatencyProfileDTO
	)
	if p, err := w.adapter.GetProbabilityEstimateByTrace(ctx, traceID); err == nil {
		prob = p
	} else {
		w.logger.Debug("validation_prob_lookup_failed", "trace_id", traceID, "error", err)
	}
	if s, err := w.adapter.GetSlippageEstimateByTrace(ctx, traceID); err == nil {
		slip = s
	} else {
		w.logger.Debug("validation_slip_lookup_failed", "trace_id", traceID, "error", err)
	}
	chain := chainFromCorrelation(ctx, w.adapter, correlationID, w.logger)
	if chain != "" {
		if l, err := w.adapter.GetLatestLatencyProfile(ctx, chain); err == nil {
			lat = l
		} else {
			w.logger.Debug("validation_lat_lookup_failed", "chain", chain, "error", err)
		}
	}
	return prob, slip, lat
}
