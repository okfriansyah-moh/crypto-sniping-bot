package workers

// run_rescan.go — periodic worker that re-emits market_data_event for
// tokens in configured age bands. Pure DB reader + event emitter; no
// RPC, no keys, no on-chain calls.
//
// Architecture: Layer 0.5 (between raw ingestion and DQ). Re-uses the
// existing market_data_event type so the downstream pipeline is unchanged.
// See docs/PLAN.md § Task 5 for full design rationale.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
)

// RunRescan starts the time-banded rescan worker.
// Blocks until ctx is cancelled.
//
// When cfg.Rescan.Enabled is false the worker logs a single diagnostic line
// and parks on ctx.Done() — it never aborts the caller goroutine.
func RunRescan(
	ctx context.Context,
	adapter database.Adapter,
	cfg *config.Config,
	logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}

	if !cfg.Rescan.Enabled {
		logger.Info("rescan_worker_disabled")
		<-ctx.Done()
		return ctx.Err()
	}

	sv, err := adapter.GetActiveStrategyVersion(ctx)
	if err != nil {
		return fmt.Errorf("run_rescan: pin version: %w", err)
	}
	versionID := sv.StrategyVersionID

	interval := time.Duration(cfg.Rescan.IntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Info("rescan_worker_started",
		"version_id", versionID,
		"interval_seconds", cfg.Rescan.IntervalSeconds,
		"bands", len(cfg.Rescan.Bands),
	)

	var heartbeatTick int
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case t := <-ticker.C:
			if err := runRescanTick(ctx, adapter, cfg, versionID, t, logger); err != nil {
				logger.Warn("rescan_tick_error", "error", err)
				// Never abort the worker — bounded failure per failure skill.
			}
			heartbeatTick++
			if heartbeatTick%10 == 0 {
				logger.Info("rescan_worker_heartbeat",
					"ticks", heartbeatTick,
					"interval_seconds", cfg.Rescan.IntervalSeconds,
				)
			}
		}
	}
}

// runRescanTick executes one rescan cycle across all configured bands.
// Per-band errors are wrapped and returned to the caller; per-token errors
// within a band are logged and skipped so one failure cannot abort the band.
func runRescanTick(
	ctx context.Context,
	adapter database.Adapter,
	cfg *config.Config,
	versionID string,
	tickTime time.Time,
	logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}
	// 1. Read active operational mode for eligibility override.
	mode := "BALANCED"
	if state, stateErr := adapter.GetSystemState(ctx); stateErr == nil && state != nil && state.Mode != "" {
		mode = state.Mode
	}
	eligibility := resolveEligibility(cfg.Rescan, mode)

	// 2. Compute bucket timestamp (idempotency anchor).
	// Two ticker cycles within the same bucket compute the same EventID.
	bucketTs := tickTime.Truncate(time.Duration(cfg.Rescan.IntervalSeconds) * time.Second).Unix()

	// 3. Process bands in ascending min_age order (already enforced by Validate).
	for _, band := range cfg.Rescan.Bands {
		rows, err := adapter.GetTokensForRescan(ctx, database.RescanQuery{
			MinAgeSeconds:     band.MinAgeSeconds,
			MaxAgeSeconds:     band.MaxAgeSeconds,
			MaxHoneypotScore:  *eligibility.MaxHoneypotScore,
			MaxRugScore:       *eligibility.MaxRugScore,
			MaxBuyTaxBps:      *eligibility.MaxBuyTaxBps,
			IncludePassed:     eligibility.IncludePassed,
			SkipOpenPositions: cfg.Rescan.SkipOpenPositions,
			Limit:             cfg.Rescan.MaxPerBandPerTick,
		})
		if err != nil {
			return fmt.Errorf("band %s: query: %w", band.Name, err)
		}

		var emitted, skipped int
		for _, dto := range rows {
			if err := emitRescanEvent(ctx, adapter, dto, band, bucketTs, versionID); err != nil {
				// Per-token failure must not abort the band — monitoring-loop-engine skill.
				logger.Warn("rescan_emit_failed",
					"token", dto.TokenAddress,
					"band", band.Name,
					"error", err,
				)
				skipped++
				continue
			}
			emitted++
		}

		logger.Info("rescan_band_completed",
			"band", band.Name,
			"candidates", len(rows),
			"emitted", emitted,
			"skipped", skipped,
			"mode", mode,
		)
	}
	return nil
}

// emitRescanEvent constructs a content-addressable re-emission of a
// MarketDataDTO and writes it to both market_data and the event bus.
//
// EventID = SHA256(chain|token_address|band_name|bucket_ts)[:16]
// This guarantees two ticker cycles in the same bucket produce the same
// EventID — InsertEvent ON CONFLICT DO NOTHING is a no-op on the second call.
func emitRescanEvent(
	ctx context.Context,
	adapter database.Adapter,
	dto contracts.MarketDataDTO,
	band config.RescanBand,
	bucketTs int64,
	versionID string,
) error {
	// Content-addressable EventID — idempotent per (chain, token, band, bucket).
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%d",
		dto.Chain, dto.TokenAddress, band.Name, bucketTs)))
	newEventID := hex.EncodeToString(h[:])[:16]

	// Fresh trace — each rescan is a new pipeline run (traceability skill R2).
	// Derived deterministically from the event ID so replay is bit-for-bit stable.
	traceID := contracts.ContentIDFromString("rescan-trace:" + newEventID)

	// Build rescanned DTO with updated routing fields.
	rescanned := dto
	rescanned.EventID = newEventID
	rescanned.TraceID = traceID
	rescanned.CorrelationID = traceID
	rescanned.CausationID = "" // Layer 0 root convention
	rescanned.VersionID = versionID
	rescanned.IngestedAt = time.Unix(bucketTs, 0).UTC().Format(time.RFC3339Nano)
	rescanned.Transport = "rescan_" + band.Name // diagnostic tag for log-reviewer
	rescanned.Priority = band.Priority

	// Persist the re-emitted market_data row (idempotent on event_id).
	if err := adapter.InsertMarketData(ctx, rescanned); err != nil {
		return fmt.Errorf("insert_market_data: %w", err)
	}

	// Emit event onto the bus (idempotent via ON CONFLICT DO NOTHING).
	payload, err := json.Marshal(rescanned)
	if err != nil {
		return fmt.Errorf("marshal_dto: %w", err)
	}

	evt := database.Event{
		EventID:       newEventID,
		EventType:     "market_data_event",
		Payload:       payload,
		TraceID:       traceID,
		CorrelationID: traceID,
		CausationID:   nil, // Layer 0 root — nil per adapter contract
		VersionID:     versionID,
		Priority:      int(band.Priority),
		Chain:         rescanned.Chain,
	}
	if err := adapter.InsertEvent(ctx, evt); err != nil {
		return fmt.Errorf("insert_event: %w", err)
	}
	return nil
}

// resolveEligibility returns the eligibility thresholds for the given
// operational mode, falling back to the base eligibility when no override
// is configured for that mode.
func resolveEligibility(cfg config.RescanConfig, mode string) config.RescanEligibility {
	if override, ok := cfg.ModeOverrides[mode]; ok {
		return override
	}
	return cfg.Eligibility
}
