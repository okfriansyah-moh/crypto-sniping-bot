package workers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/validation"
	"crypto-sniping-bot/internal/orchestrator"
)

// ValidationWorker implements Layer 5: Edge Validation (EV gate).
// Consumes: edge_event → emits: validated_edge_event (ACCEPT only)
type ValidationWorker struct {
	adapter database.Adapter
	mod     *validation.Module
	cfg     *config.Config
	logger  *slog.Logger
}

// NewValidationWorker returns a new ValidationWorker.
func NewValidationWorker(adapter database.Adapter, cfg *config.Config, logger *slog.Logger) *ValidationWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &ValidationWorker{
		adapter: adapter,
		mod:     validation.New(&cfg.Validation).WithProbabilityRuntime(&cfg.ProbabilityRuntime),
		cfg:     cfg,
		logger:  logger,
	}
}

func (w *ValidationWorker) Process(ctx context.Context, evt *database.Event) (*database.Event, error) {
	var dto contracts.EdgeDTO
	if err := json.Unmarshal(evt.Payload, &dto); err != nil {
		return nil, fmt.Errorf("validation_worker: unmarshal: %w", err)
	}

	prob, slip, lat := w.fetchEstimates(ctx, evt.TraceID, evt.CorrelationID)
	// F-SEC-04: pass the bus-recorded creation time as the deterministic
	// `now` so replay produces identical ExpiresAt/ValidatedAt timestamps.
	// Falls back to wall-clock UTC when CreatedAt is missing/unparseable
	// (legacy events / unit-test paths).
	now := time.Now().UTC()
	if evt.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, evt.CreatedAt); err == nil && !t.IsZero() {
			now = t.UTC()
		}
	}
	evThreshold := w.resolveEVThresholdBps(ctx)
	vedge, err := w.mod.ProcessWithEstimatesAt(ctx, dto, prob, slip, lat, evThreshold, now)
	if err != nil {
		return nil, fmt.Errorf("validation_worker: module: %w", err)
	}

	w.logger.Info("validation_decision",
		"token", vedge.TokenAddress,
		"decision", vedge.Decision,
		"ev_bps", vedge.ExpectedValueBps,
		"ev_threshold_bps", vedge.EvThresholdApplied,
		"probability_used", vedge.ProbabilityUsed,
		"reject_reason", vedge.RejectReason,
		"trace_id", vedge.TraceID,
		"version_id", vedge.VersionID,
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
		orchestrator.RecordDecision(ctx, orchestrator.StageStatusRejected, validationRejectReason(vedge.RejectReason))
		return nil, nil
	}

	return makeOutputEvent(
		vedge.EventID, vedge, "validated_edge_event",
		evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
	)
}

// fetchEstimates returns the latest model estimates for this trace.
//
// Bounded join-wait (per docs/architecture.md § 2 + § 3.5): probability
// and slippage producers consume the same upstream feature_event and
// commit to the bus-state tables in parallel with edge production.
// Without a bounded wait the validation worker can race ahead of the
// probability worker and observe a missing row — historically this
// caused the regression where every trace fell back to PriorProbability
// and rejected with ev_bps≈-1900. We now poll the DB-backed join state
// for at most ValidationConfig.JoinTimeoutMs, then return whatever is
// committed. Missing-prob handling (REJECT vs prior fallback) is
// enforced inside the validation module so it stays deterministic and
// pure.
//
// All errors are logged at debug level and treated as nil — the bus
// is the source of truth, transient driver errors are retried on the
// next poll iteration.
func (w *ValidationWorker) fetchEstimates(
	ctx context.Context,
	traceID, correlationID string,
) (*contracts.ProbabilityEstimateDTO, *contracts.SlippageEstimateDTO, *contracts.LatencyProfileDTO) {
	var (
		prob *contracts.ProbabilityEstimateDTO
		slip *contracts.SlippageEstimateDTO
		lat  *contracts.LatencyProfileDTO
	)

	timeout := time.Duration(w.cfg.Validation.JoinTimeoutMs) * time.Millisecond
	pollInterval := time.Duration(w.cfg.Validation.JoinPollIntervalMs) * time.Millisecond
	deadline := time.Now().Add(timeout)

joinLoop:
	for {
		if prob == nil || slip == nil {
			// F-SEC-05: single combined join lookup halves DB round-trips
			// vs the previous two-call (prob + slip) per-iteration pattern.
			p, s, err := w.adapter.GetEstimatesByTrace(ctx, traceID)
			if err != nil && !errors.Is(err, database.ErrNotFound) {
				w.logger.Debug("validation_estimates_lookup_failed", "trace_id", traceID, "error", err)
			}
			if prob == nil && p != nil {
				prob = p
			}
			if slip == nil && s != nil {
				slip = s
			}
		}

		// Both joins satisfied or join-wait disabled (timeout <= 0): exit.
		if (prob != nil && slip != nil) || timeout <= 0 {
			break
		}
		if !time.Now().Before(deadline) || ctx.Err() != nil {
			break
		}
		// Bounded sleep — deterministic upper bound per JoinPollIntervalMs.
		// pollInterval <= 0 means "tight loop"; clamp to 1ms to avoid burn.
		sleep := pollInterval
		if sleep <= 0 {
			sleep = time.Millisecond
		}
		select {
		case <-ctx.Done():
			break joinLoop
		case <-time.After(sleep):
		}
	}

	if prob == nil {
		w.logger.Warn("validation_prob_join_timeout",
			"trace_id", traceID,
			"timeout_ms", w.cfg.Validation.JoinTimeoutMs,
		)
	}
	if slip == nil {
		w.logger.Debug("validation_slip_unavailable", "trace_id", traceID)
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

// resolveEVThresholdBps maps the active operational mode to ev_threshold_bps
// from config/priority.yaml via Config.ResolveModeThresholds (docs/PLAN.md Task 2).
func (w *ValidationWorker) resolveEVThresholdBps(ctx context.Context) int32 {
	sysMode := w.cfg.Priority.ActiveMode
	if sysMode == "" {
		sysMode = "balanced"
	}
	if state, err := w.adapter.GetSystemState(ctx); err == nil && state != nil && state.Mode != "" {
		sysMode = state.Mode
	}
	return w.cfg.ResolveModeThresholds(sysMode).EvThresholdBps
}
