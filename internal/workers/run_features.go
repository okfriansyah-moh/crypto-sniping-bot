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
	"crypto-sniping-bot/internal/modules/features"
)

// FeaturesWorker implements Layer 2: Feature Extraction.
// Consumes: data_quality_event → emits: feature_event
//
// The worker hydrates the upstream MarketDataDTO via the event causation
// chain, builds a MarketSnapshot, and feeds the pure features module a
// snapshot of the rolling per-market baseline. After every successful
// extraction the raw signals are appended to the baseline so subsequent
// events compute z-scores against an evolving distribution.
type FeaturesWorker struct {
	adapter  database.Adapter
	mod      *features.Module
	baseline *features.BaselineStore
	logger   *slog.Logger
	now      func() time.Time

	// Baseline persistence (residual-risk #1) — populated from cfg.Feature.
	flushIntervalSec int
	flushMaxWrites   int
}

// baselineModuleFeatures is the Adapter `module` discriminator for the
// features worker's persisted baselines.
const baselineModuleFeatures = "features"

// NewFeaturesWorker returns a new FeaturesWorker.
func NewFeaturesWorker(adapter database.Adapter, cfg *config.Config, logger *slog.Logger) *FeaturesWorker {
	if logger == nil {
		logger = slog.Default()
	}
	maxLen := 256
	flushInterval := 0
	flushMax := 0
	if cfg != nil {
		if cfg.Feature.SyncCache.SizePerPool > 0 {
			maxLen = cfg.Feature.SyncCache.SizePerPool
		}
		flushInterval = cfg.Feature.BaselineFlushIntervalSec
		flushMax = cfg.Feature.BaselineFlushMaxWrites
	}
	return &FeaturesWorker{
		adapter:          adapter,
		mod:              features.New(cfg),
		baseline:         features.NewBaselineStore(maxLen),
		logger:           logger,
		now:              time.Now,
		flushIntervalSec: flushInterval,
		flushMaxWrites:   flushMax,
	}
}

func (w *FeaturesWorker) Process(ctx context.Context, evt *database.Event) (*database.Event, error) {
	var dq contracts.DataQualityDTO
	if err := json.Unmarshal(evt.Payload, &dq); err != nil {
		return nil, fmt.Errorf("features_worker: unmarshal: %w", err)
	}

	// Hydrate the upstream MarketDataDTO via the data-quality causation
	// chain. data_quality.causation_id == market_data.event_id by contract.
	snap, snapKnown := w.fetchMarketSnapshot(ctx, dq, evt)
	baseSnap := w.baseline.Snapshot(snap.Market)

	// Inject a deterministic timestamp — prefer the upstream block time
	// so replay produces identical FeatureDTO bytes; fall back to the
	// upstream evaluation time, then the worker clock.
	extractedAt := dq.EvaluatedAt
	if !snap.BlockTimestamp.IsZero() {
		extractedAt = snap.BlockTimestamp.UTC().Format(time.RFC3339Nano)
	}
	if extractedAt == "" {
		extractedAt = w.now().UTC().Format(time.RFC3339Nano)
	}

	featDTO, err := w.mod.ProcessWithContext(ctx, dq, snap, baseSnap, extractedAt)
	if err != nil {
		return nil, fmt.Errorf("features_worker: module: %w", err)
	}

	// Append the raw signals to the baseline AFTER processing so the
	// current event's z-score is computed against history alone.
	if snap.Market != "" {
		w.baseline.AppendBatch(snap.Market, w.mod.RawSignalsForBaseline(dq, snap))
	}

	w.logger.Info("features_extracted",
		"token", featDTO.TokenAddress,
		"liquidity_score", featDTO.LiquidityScore,
		"volume_momentum", featDTO.VolumeMomentum,
		"contract_safety", featDTO.ContractSafety,
		"liquidity_conf", featDTO.Confidence.LiquidityScore,
		"volume_conf", featDTO.Confidence.VolumeMomentum,
		"safety_conf", featDTO.Confidence.ContractSafety,
		"market_snap_known", snapKnown,
		"trace_id", featDTO.TraceID,
	)

	if err := w.adapter.InsertFeature(ctx, featDTO); err != nil {
		w.logger.Warn("features_worker_persist_failed", "event_id", featDTO.EventID, "error", err)
	}

	if err := doMandatoryTransition(ctx, w.adapter, dq.TokenLifecycleID, "DQ_PASSED", "FEATURE_READY", "", "features_worker"); err != nil {
		return nil, fmt.Errorf("features_worker: transition: %w", err)
	}

	return makeOutputEvent(
		featDTO.EventID, featDTO, "feature_event",
		evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
	)
}

// fetchMarketSnapshot resolves the MarketDataDTO that originated this
// trace. Returns (snapshot, true) on success; (zero snapshot, false) when
// the upstream record is unavailable so the module degrades to cold-start
// rather than emitting constants.
func (w *FeaturesWorker) fetchMarketSnapshot(
	ctx context.Context,
	dq contracts.DataQualityDTO,
	evt *database.Event,
) (features.MarketSnapshot, bool) {
	mdID := dq.CausationID
	if mdID == "" && evt.CausationID != nil {
		mdID = *evt.CausationID
	}
	if mdID == "" {
		w.logger.Warn("features_worker_missing_causation",
			"event_id", evt.EventID, "trace_id", evt.TraceID)
		return features.MarketSnapshot{}, false
	}
	md, err := w.adapter.GetMarketData(ctx, mdID)
	if err != nil || md == nil {
		w.logger.Warn("features_worker_market_data_unavailable",
			"event_id", evt.EventID,
			"market_data_id", mdID,
			"error", err,
		)
		return features.MarketSnapshot{}, false
	}
	return features.MarketSnapshotFromDTO(md), true
}

// HydrateBaselines rehydrates the in-memory ring buffers from the
// adapter's persisted snapshot. Best-effort: a failed load logs a warn
// and the worker proceeds with empty baselines (cold-start behaviour,
// identical to a fresh deploy). Safe to call before the event loop.
//
// Residual-risk #1 fix.
func (w *FeaturesWorker) HydrateBaselines(ctx context.Context) {
	snap, err := w.adapter.LoadBaselines(ctx, baselineModuleFeatures)
	if err != nil {
		w.logger.Warn("baselines_hydrate_failed",
			"module", baselineModuleFeatures, "error", err)
		return
	}
	w.baseline.Hydrate(snap)
	keys := 0
	for _, bySig := range snap {
		keys += len(bySig)
	}
	w.logger.Info("baselines_hydrated",
		"module", baselineModuleFeatures,
		"markets", len(snap),
		"keys", keys,
	)
}

// RunBaselinePersistence runs the debounced flush loop. Blocks until
// ctx.Done. Intended to be launched as a goroutine after the worker is
// constructed and before — or in parallel with — the orchestrator
// starts dispatching events.
func (w *FeaturesWorker) RunBaselinePersistence(ctx context.Context) {
	runBaselineFlushLoop(
		ctx, w.logger, baselineModuleFeatures,
		w.flushIntervalSec, w.flushMaxWrites,
		&featuresBaselineFlusher{adapter: w.adapter, store: w.baseline},
	)
}

// featuresBaselineFlusher adapts the features BaselineStore + database
// adapter to the package-private baselineFlusher interface.
type featuresBaselineFlusher struct {
	adapter database.Adapter
	store   *features.BaselineStore
}

func (f *featuresBaselineFlusher) dirtyKeys() []baselineFlushKey {
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

func (f *featuresBaselineFlusher) flushKey(ctx context.Context, market, signal string) error {
	values := f.store.Values(market, signal)
	if err := f.adapter.SaveBaseline(ctx, baselineModuleFeatures, market, signal, values); err != nil {
		return err
	}
	f.store.MarkClean(market, signal)
	return nil
}
