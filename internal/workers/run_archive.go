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
)

// RunArchive runs the periodic data archival worker.
// It moves processed events older than cfg.Retention.WarmDays to events_archive,
// and runs every cfg.Retention.IntervalHours.
func RunArchive(ctx context.Context, adapter database.Adapter, cfg *config.Config, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	intervalHours := cfg.Retention.IntervalHours
	if intervalHours <= 0 {
		intervalHours = 24
	}
	warmDays := cfg.Retention.WarmDays
	if warmDays <= 0 {
		warmDays = 30
	}
	batchSize := cfg.Retention.BatchSize
	if batchSize <= 0 {
		batchSize = 10000
	}

	interval := time.Duration(intervalHours) * time.Hour
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := runArchiveBatch(ctx, adapter, cfg, warmDays, batchSize, logger); err != nil {
				logger.Error("archive_worker_batch_failed", "error", err)
				// Non-fatal: log and continue.
			}
		}
	}
}

// runArchiveBatch moves one batch of processed events to the archive partition.
func runArchiveBatch(
	ctx context.Context,
	adapter database.Adapter,
	cfg *config.Config,
	warmDays int,
	batchSize int,
	logger *slog.Logger,
) error {
	sv, svErr := adapter.GetActiveStrategyVersion(ctx)
	versionID := ""
	if svErr == nil && sv != nil {
		versionID = sv.StrategyVersionID
	}

	cutoff := time.Now().UTC().Add(-time.Duration(warmDays) * 24 * time.Hour)
	n, err := adapter.ArchiveEvents(ctx, cutoff, batchSize)
	if err != nil {
		return fmt.Errorf("archive_worker: archive events: %w", err)
	}

	if n == 0 {
		return nil
	}

	// Emit archive_event for observability.
	payload, marshalErr := json.Marshal(map[string]interface{}{
		"archived_count": n,
		"cutoff":         cutoff.Format(time.RFC3339),
		"batch_size":     batchSize,
	})
	if marshalErr != nil {
		return fmt.Errorf("archive_worker: marshal event: %w", marshalErr)
	}

	eventID := contracts.ContentIDFromString(fmt.Sprintf("archive:%s:%d", cutoff.Format(time.RFC3339), n))
	evt := database.Event{
		EventID:       eventID,
		EventType:     "archive_event",
		Payload:       payload,
		TraceID:       systemTraceID,
		CorrelationID: systemTraceID,
		VersionID:     versionID,
	}

	if insertErr := adapter.InsertEvent(ctx, evt); insertErr != nil {
		logger.Warn("archive_worker_event_insert_failed", "error", insertErr)
	}

	logger.Info("archive_worker_batch_complete",
		"archived_count", n,
		"cutoff", cutoff.Format(time.RFC3339),
	)
	return nil
}
