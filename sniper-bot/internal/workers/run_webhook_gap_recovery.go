package workers

// run_webhook_gap_recovery.go — periodic gap backfill for webhook-delivery programs.
// Webhook programs do not maintain a persistent WS subscription; this ticker
// invokes ingestion_solana.RecoverGap on a bounded slot window so missed
// graduation events are re-emitted idempotently.

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/sniper-bot/internal/modules/ingestion_solana"
)

const webhookGapRecoveryInterval = 5 * time.Minute

type slotReader interface {
	GetSlot(ctx context.Context, commitment string) (uint64, error)
}

// RunWebhookGapRecovery periodically backfills webhook-delivery programs.
func RunWebhookGapRecovery(
	ctx context.Context,
	adapter database.Adapter,
	cfg *config.Config,
	client ingestion_solana.SolanaRPCClient,
	logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg == nil || !ingestion_solana.WebhookIngressActive(cfg.Solana) {
		logger.Info("webhook_gap_recovery_disabled")
		<-ctx.Done()
		return ctx.Err()
	}

	sv, err := adapter.GetActiveStrategyVersion(ctx)
	if err != nil {
		return fmt.Errorf("webhook_gap_recovery: pin version: %w", err)
	}
	versionID := sv.StrategyVersionID

	emit := BuildSolanaEmitter(ctx, adapter, versionID, logger)
	ticker := time.NewTicker(webhookGapRecoveryInterval)
	defer ticker.Stop()

	maxSlots := cfg.Solana.GapRecoveryMaxSlots
	if maxSlots == 0 {
		maxSlots = 500
	}

	logger.Info("webhook_gap_recovery_started", "interval", webhookGapRecoveryInterval.String())

	runOnce := func() {
		var currentSlot uint64
		if sr, ok := client.(slotReader); ok {
			if s, sErr := sr.GetSlot(ctx, "confirmed"); sErr == nil {
				currentSlot = s
			}
		}
		for _, prog := range cfg.Solana.Programs {
			if prog.Disabled {
				continue
			}
			if ingestion_solana.EffectiveProgramDelivery(cfg.Solana, prog) != ingestion_solana.DeliveryWebhook {
				continue
			}
			fromSlot, wmErr := adapter.GetSolanaIngestionWatermark(ctx, "solana-"+prog.Family)
			if wmErr != nil {
				logger.Warn("webhook_gap_recovery_watermark_failed",
					"family", prog.Family,
					"error", wmErr,
				)
				continue
			}
			toSlot := currentSlot
			if toSlot == 0 || toSlot <= fromSlot {
				continue
			}
			_, recErr := ingestion_solana.RecoverGap(
				ctx, client, prog, fromSlot, toSlot, versionID, emit,
				maxSlots, logger,
			)
			if recErr != nil {
				logger.Warn("webhook_gap_recovery_tick_failed",
					"family", prog.Family,
					"error", recErr,
				)
			}
		}
	}

	runOnce()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			runOnce()
		}
	}
}
