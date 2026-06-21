package workers

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
)

// RunProbePendingWorker drains probe_pending_queue rows whose due_at has passed
// and re-emits market_data_event with transport=probe_pending_retry.
func RunProbePendingWorker(
	ctx context.Context,
	adapter database.Adapter,
	cfg *config.Config,
	logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg == nil || !cfg.Probes.PendingQueue.Enabled {
		<-ctx.Done()
		return ctx.Err()
	}

	pq := cfg.Probes.PendingQueue
	interval := time.Duration(pq.DrainIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}
	batchSize := pq.DrainBatchSize
	if batchSize <= 0 {
		batchSize = 50
	}
	ttlHours := pq.TTLHours
	if ttlHours <= 0 {
		ttlHours = 24
	}
	maxAttempts := pq.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			expiredRows, err := adapter.ExpireStaleProbePendingRows(ctx, ttlHours)
			if err != nil {
				logger.Warn("probe_pending_expire_failed", "error", err)
			} else if len(expiredRows) > 0 {
				logger.Info("probe_pending_expired", "count", len(expiredRows))
				for _, row := range expiredRows {
					if err := emitProbeFairChanceRetry(ctx, adapter, row, "probe_exhausted_retry", logger); err != nil {
						logger.Warn("probe_pending_fair_chance_emit_failed",
							"pending_id", row.PendingID,
							"token", row.TokenAddress,
							"error", err,
						)
					}
				}
			}

			rows, err := adapter.ClaimDueProbePending(ctx, batchSize)
			if err != nil {
				logger.Warn("probe_pending_claim_failed", "error", err)
				continue
			}
			for _, row := range rows {
				if err := emitProbeFairChanceRetry(ctx, adapter, row, "probe_pending_retry", logger); err != nil {
					attempts := row.AttemptCount + 1
					_ = adapter.FailProbePending(ctx, row.PendingID, err.Error(), maxAttempts)
					if attempts >= maxAttempts {
						logger.Warn("probe_pending_max_attempts",
							"pending_id", row.PendingID,
							"token", row.TokenAddress,
							"error", err,
						)
						_ = emitProbeFairChanceRetry(ctx, adapter, row, "probe_exhausted_retry", logger)
					}
				} else {
					_ = adapter.CompleteProbePending(ctx, row.PendingID)
				}
			}
		}
	}
}

func emitProbeFairChanceRetry(
	ctx context.Context,
	adapter database.Adapter,
	row database.ProbePendingRow,
	transport string,
	logger *slog.Logger,
) error {
	md := row.Payload
	if md.TokenAddress == "" {
		latest, err := adapter.GetLatestMarketDataForToken(ctx, row.Chain, row.TokenAddress)
		if err != nil {
			return err
		}
		if latest == nil {
			return nil
		}
		md = *latest
	}

	newEventID := contracts.ContentIDFromString(transport + ":" + row.PendingID)
	traceID := md.TraceID
	if traceID == "" {
		traceID = contracts.ContentIDFromString("probe-pending-trace:" + row.PendingID)
	}

	md.EventID = newEventID
	md.TraceID = traceID
	md.CorrelationID = traceID
	md.CausationID = row.SourceEventID
	md.Transport = transport
	md.IngestedAt = time.Now().UTC().Format(time.RFC3339Nano)

	if err := adapter.InsertMarketData(ctx, md); err != nil {
		return err
	}

	payload, err := json.Marshal(md)
	if err != nil {
		return err
	}

	evt := database.Event{
		EventID:       newEventID,
		EventType:     "market_data_event",
		Payload:       payload,
		TraceID:       traceID,
		CorrelationID: traceID,
		VersionID:     md.VersionID,
		Chain:         row.Chain,
	}
	if err := adapter.InsertEvent(ctx, evt); err != nil {
		return err
	}
	logger.Debug("probe_pending_requeued",
		"pending_id", row.PendingID,
		"token", row.TokenAddress,
		"market", row.Market,
		"transport", transport,
	)
	return nil
}
