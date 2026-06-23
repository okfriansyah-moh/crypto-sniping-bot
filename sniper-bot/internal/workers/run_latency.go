package workers

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/sniper-bot/internal/modules/models"
)

// LatencyWorker implements Layer 4: periodic Latency Profile emission per chain.
// Unlike the other Phase 4 workers, this worker is timer-driven and has no input event.
//
// Each tick:
//  1. For every configured chain, compute a LatencyProfileDTO snapshot.
//  2. Persist via adapter.InsertLatencyProfile.
//  3. Emit latency_event onto the bus (CausationID="" — periodic root event).
//
// The TraceID is content-addressed per (chain, window epoch) so duplicate emissions
// in the same window are dropped by ON CONFLICT (event_id) DO NOTHING.
type LatencyWorker struct {
	adapter      database.Adapter
	model        *models.LatencyModel
	cfg          *config.Config
	logger       *slog.Logger
	tickInterval time.Duration
	versionID    string
}

// NewLatencyWorker constructs a LatencyWorker with the given config.
func NewLatencyWorker(adapter database.Adapter, cfg *config.Config, versionID string, logger *slog.Logger) *LatencyWorker {
	if logger == nil {
		logger = slog.Default()
	}
	tick := time.Duration(cfg.Models.LatencyProfileIntervalSecs) * time.Second
	if tick <= 0 {
		tick = 60 * time.Second
	}
	return &LatencyWorker{
		adapter:      adapter,
		model:        models.NewLatencyModel(latencyCfgFromConfig(cfg)),
		cfg:          cfg,
		logger:       logger,
		tickInterval: tick,
		versionID:    versionID,
	}
}

// Model exposes the underlying LatencyModel so RPC clients can record samples.
func (w *LatencyWorker) Model() *models.LatencyModel { return w.model }

// Run emits a latency profile per configured chain at every tick.
// Returns when ctx is cancelled.
func (w *LatencyWorker) Run(ctx context.Context) error {
	timer := time.NewTicker(w.tickInterval)
	defer timer.Stop()

	// Emit immediately on startup to seed the table.
	w.emitAll(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			w.emitAll(ctx)
		}
	}
}

// emitAll computes and emits a latency profile for every configured chain.
// Chains are processed in deterministic (sorted) order.
func (w *LatencyWorker) emitAll(ctx context.Context) {
	chains := make([]string, 0, len(w.cfg.Chains))
	for k := range w.cfg.Chains {
		chains = append(chains, k)
	}
	sort.Strings(chains)

	for _, chain := range chains {
		dto, err := w.model.Profile(ctx, chain)
		if err != nil {
			w.logger.Warn("latency_worker_profile_failed", "chain", chain, "error", err)
			continue
		}
		dto.VersionID = w.versionID
		if err := w.adapter.InsertLatencyProfile(ctx, dto); err != nil {
			w.logger.Warn("latency_worker_persist_failed", "chain", chain, "error", err)
		}
		evt, err := makeOutputEvent(
			dto.EventID, dto, "latency_event",
			dto.TraceID, dto.CorrelationID, "", w.versionID,
		)
		if err != nil {
			w.logger.Warn("latency_worker_event_marshal_failed", "chain", chain, "error", err)
			continue
		}
		if err := w.adapter.InsertEvent(ctx, *evt); err != nil {
			w.logger.Debug("latency_worker_event_insert_skipped", "chain", chain, "error", err)
		}
	}
}

// latencyCfgFromConfig converts YAML-loaded config to a models.LatencyConfig.
func latencyCfgFromConfig(cfg *config.Config) models.LatencyConfig {
	defaults := models.DefaultLatencyConfig()
	if cfg == nil {
		return defaults
	}
	src := cfg.Models.Latency
	out := models.LatencyConfig{
		WindowSeconds: src.WindowSeconds,
		MinSamples:    src.MinSamples,
		FallbackP50Ms: src.FallbackP50Ms,
		FallbackP95Ms: src.FallbackP95Ms,
	}
	if out.WindowSeconds == 0 {
		out.WindowSeconds = defaults.WindowSeconds
	}
	if out.MinSamples == 0 {
		out.MinSamples = defaults.MinSamples
	}
	if out.FallbackP50Ms == 0 {
		out.FallbackP50Ms = defaults.FallbackP50Ms
	}
	if out.FallbackP95Ms == 0 {
		out.FallbackP95Ms = defaults.FallbackP95Ms
	}
	return out
}
