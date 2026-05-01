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
	"crypto-sniping-bot/internal/modules/edge"
)

// EdgeWorker implements Layer 3: Signal & Edge Discovery.
// Consumes: feature_event → emits: edge_event (only when an actionable
// edge is detected; NONE outputs are persisted but no downstream event
// is emitted, and the lifecycle transitions to REJECTED).
//
// The worker owns a per-process BaselineStore that records observed
// PriceMomentum / VolumeMomentum values so the pure module can derive
// an adaptive PriceMomentum threshold (rolling-window quantile) on the
// next event. The store is in-memory only; cold-start (samples < min)
// falls back to the configured MinPriceMomentum.
type EdgeWorker struct {
	adapter  database.Adapter
	mod      *edge.Module
	baseline *edge.BaselineStore
	logger   *slog.Logger
	now      func() time.Time

	// Baseline persistence (residual-risk #1) — populated from cfg.Edge.
	flushIntervalSec int
	flushMaxWrites   int
}

// baselineMarketKey is the per-process baseline partition. FeatureDTO
// does not (yet) carry a market identifier, so the worker uses a single
// global key. Documented residual risk: per-market percentile drift is
// not isolated until FeatureDTO.Market is added (cross-module change,
// out of scope for the F-4 fix).
const baselineMarketKey = "global"

// baselineModuleEdge is the Adapter `module` discriminator for the edge
// worker's persisted baselines.
const baselineModuleEdge = "edge"

// NewEdgeWorker returns a new EdgeWorker.
func NewEdgeWorker(adapter database.Adapter, cfg *config.Config, logger *slog.Logger) *EdgeWorker {
	if logger == nil {
		logger = slog.Default()
	}
	maxLen := 256
	flushInterval := 0
	flushMax := 0
	if cfg != nil {
		if cfg.Edge.BaselineMaxLen > 0 {
			maxLen = cfg.Edge.BaselineMaxLen
		}
		flushInterval = cfg.Edge.BaselineFlushIntervalSec
		flushMax = cfg.Edge.BaselineFlushMaxWrites
	}
	var edgeCfg *config.EdgeConfig
	if cfg != nil {
		edgeCfg = &cfg.Edge
	}
	return &EdgeWorker{
		adapter:          adapter,
		mod:              edge.New(edgeCfg),
		baseline:         edge.NewBaselineStore(maxLen),
		logger:           logger,
		now:              time.Now,
		flushIntervalSec: flushInterval,
		flushMaxWrites:   flushMax,
	}
}

func (w *EdgeWorker) Process(ctx context.Context, evt *database.Event) (*database.Event, error) {
	var dto contracts.FeatureDTO
	if err := json.Unmarshal(evt.Payload, &dto); err != nil {
		return nil, fmt.Errorf("edge_worker: unmarshal: %w", err)
	}

	snapshot := w.baseline.Snapshot(baselineMarketKey)
	edgeDTO, err := w.mod.ProcessWithContext(ctx, dto, snapshot, w.now().UTC())
	if err != nil {
		return nil, fmt.Errorf("edge_worker: module: %w", err)
	}

	// Append the observed momentum signals AFTER processing so this
	// event's adaptive threshold is computed against history alone
	// (deterministic — first-event behaviour is independent of itself).
	w.baseline.AppendBatch(baselineMarketKey, map[string]float64{
		edge.SignalPriceMomentum:  dto.PriceMomentum,
		edge.SignalVolumeMomentum: dto.VolumeMomentum,
	})

	w.logger.Info("edge_decision",
		"token", edgeDTO.TokenAddress,
		"edge_type", edgeDTO.EdgeType,
		"edge_strength", edgeDTO.EdgeStrength,
		"edge_confidence", edgeDTO.EdgeConfidence,
		"momentum_score", edgeDTO.MomentumScore,
		"threshold_applied", edgeDTO.ThresholdApplied,
		"edge_model_version", edgeDTO.EdgeModelVersionID,
		"reject_reason", edgeDTO.RejectReason,
		"trace_id", edgeDTO.TraceID,
	)

	if err := w.adapter.InsertEdge(ctx, edgeDTO); err != nil {
		w.logger.Warn("edge_worker_persist_failed", "event_id", edgeDTO.EventID, "error", err)
	}

	nextState := "EDGE_DETECTED"
	if !edgeDTO.IsEdgeDetected() {
		nextState = "REJECTED"
	}
	if err := doMandatoryTransition(ctx, w.adapter, dto.TokenLifecycleID, "FEATURE_READY", nextState, "", "edge_worker"); err != nil {
		return nil, fmt.Errorf("edge_worker: transition: %w", err)
	}

	if !edgeDTO.IsEdgeDetected() {
		return nil, nil
	}

	return makeOutputEvent(
		edgeDTO.EventID, edgeDTO, "edge_event",
		evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
	)
}

// HydrateBaselines rehydrates the in-memory ring buffers from the
// adapter's persisted snapshot. Best-effort: a failed load logs a warn
// and the worker proceeds with empty baselines (cold-start).
//
// Residual-risk #1 fix.
func (w *EdgeWorker) HydrateBaselines(ctx context.Context) {
	snap, err := w.adapter.LoadBaselines(ctx, baselineModuleEdge)
	if err != nil {
		w.logger.Warn("baselines_hydrate_failed",
			"module", baselineModuleEdge, "error", err)
		return
	}
	w.baseline.Hydrate(snap)
	keys := 0
	for _, bySig := range snap {
		keys += len(bySig)
	}
	w.logger.Info("baselines_hydrated",
		"module", baselineModuleEdge,
		"markets", len(snap),
		"keys", keys,
	)
}

// RunBaselinePersistence runs the debounced flush loop. Blocks until
// ctx.Done.
func (w *EdgeWorker) RunBaselinePersistence(ctx context.Context) {
	runBaselineFlushLoop(
		ctx, w.logger, baselineModuleEdge,
		w.flushIntervalSec, w.flushMaxWrites,
		&edgeBaselineFlusher{adapter: w.adapter, store: w.baseline},
	)
}

// edgeBaselineFlusher adapts the edge BaselineStore + database adapter
// to the package-private baselineFlusher interface.
type edgeBaselineFlusher struct {
	adapter database.Adapter
	store   *edge.BaselineStore
}

func (f *edgeBaselineFlusher) dirtyKeys() []baselineFlushKey {
	src := f.store.DirtyKeys()
	if len(src) == 0 {
		return nil
	}
	out := make([]baselineFlushKey, len(src))
	for i, k := range src {
		out[i] = baselineFlushKey{Market: k.Market, Signal: k.Signal}
	}
	return out
}

func (f *edgeBaselineFlusher) flushKey(ctx context.Context, market, signal string) error {
	values := f.store.Values(market, signal)
	if err := f.adapter.SaveBaseline(ctx, baselineModuleEdge, market, signal, values); err != nil {
		return err
	}
	f.store.MarkClean(market, signal)
	return nil
}
