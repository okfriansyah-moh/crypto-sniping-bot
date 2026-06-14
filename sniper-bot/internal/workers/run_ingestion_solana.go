package workers

// run_ingestion_solana.go — worker that wires the Solana ingestion module to the database adapter.
// Follows the same pattern as run_ingestion.go.
// Only this worker is allowed to call adapter.InsertMarketData and adapter.UpsertSolanaIngestionWatermark.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/sniper-bot/internal/modules/ingestion_solana"
)

// RunIngestionSolana starts the Solana ingestion module.
// client is the SolanaRPCClient implementation (nil = noop mode for tests).
// solUsdSource (Phase 3) is the optional live SOL/USD price provider; pass
// nil to fall back to cfg.Solana.SolEstimatedPriceUsd.
//
// Flow:
//  1. Pin the active strategy version.
//  2. Get Solana ingestion watermark per program (gap recovery on restart).
//  3. Build EventEmitter wrapping adapter.InsertEvent + adapter.InsertMarketData.
//  4. Start ingestion_solana.Module.
func RunIngestionSolana(
	ctx context.Context,
	adapter database.Adapter,
	cfg *config.Config,
	client ingestion_solana.SolanaRPCClient,
	solUsdSource ingestion_solana.SolUsdSource,
	logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}

	sv, err := adapter.GetActiveStrategyVersion(ctx)
	if err != nil {
		return fmt.Errorf("run_ingestion_solana: get active strategy version: %w", err)
	}
	versionID := sv.StrategyVersionID
	logger.Info("solana_ingestion_version_pinned", "version_id", versionID)

	solanaCfg := cfg.Solana
	if len(solanaCfg.Programs) == 0 {
		logger.Info("solana_ingestion_no_programs_configured")
		<-ctx.Done()
		return ctx.Err()
	}

	// Get the first program's watermark for startup logging.
	for _, prog := range solanaCfg.Programs {
		slot, wErr := adapter.GetSolanaIngestionWatermark(ctx, "solana-"+prog.Family)
		if wErr != nil {
			logger.Warn("solana_ingestion_watermark_failed",
				"program", prog.ProgramID, "error", wErr)
		} else {
			logger.Info("solana_ingestion_watermark",
				"program", prog.ProgramID,
				"last_slot", slot,
			)
		}
	}

	emit := buildSolanaEmitter(ctx, adapter, versionID, logger)

	mod := ingestion_solana.New(solanaCfg, versionID, emit, logger)
	if client != nil {
		mod.WithClient(client)
	}
	if solUsdSource != nil {
		mod.WithSolUsdSource(solUsdSource)
	}
	if solanaCfg.PreFilter.Enabled && solanaCfg.PreFilter.MaxCreatorPrevTokenCount > 0 {
		mod.WithCreatorProfileReader(newAdapterCreatorProfileReader(adapter))
		logger.Info("solana_ingestion_pre_filter_enabled",
			"max_creator_prev_token_count", solanaCfg.PreFilter.MaxCreatorPrevTokenCount,
		)
	}

	if err := mod.Start(ctx); err != nil && err != ctx.Err() {
		return fmt.Errorf("run_ingestion_solana: module error: %w", err)
	}
	return ctx.Err()
}

// buildSolanaEmitter returns an EventEmitter that persists MarketDataDTOs to the adapter.
func buildSolanaEmitter(
	_ context.Context,
	adapter database.Adapter,
	versionID string,
	logger *slog.Logger,
) ingestion_solana.EventEmitter {
	return func(emitCtx context.Context, dto contracts.MarketDataDTO) error {
		payload, err := json.Marshal(dto)
		if err != nil {
			return fmt.Errorf("solana_emit: marshal: %w", err)
		}

		var causationID *string
		if dto.CausationID != "" {
			causationID = &dto.CausationID
		}

		evt := database.Event{
			EventID:       dto.EventID,
			EventType:     "market_data_event",
			Payload:       payload,
			TraceID:       dto.TraceID,
			CorrelationID: dto.CorrelationID,
			CausationID:   causationID,
			VersionID:     versionID,
		}

		if err := adapter.InsertEvent(emitCtx, evt); err != nil {
			logger.Error("solana_emit_insert_event_failed",
				"event_id", dto.EventID, "error", err)
			return fmt.Errorf("solana_emit: insert event: %w", err)
		}

		if err := adapter.InsertMarketData(emitCtx, dto); err != nil {
			logger.Error("solana_emit_insert_market_data_failed",
				"event_id", dto.EventID, "error", err)
			return fmt.Errorf("solana_emit: insert market data: %w", err)
		}

		// Update Solana slot watermark (best-effort; non-fatal).
		market := dto.Market
		if market == "" {
			market = "solana-" + dto.Chain
		}
		if wmErr := adapter.UpsertSolanaIngestionWatermark(emitCtx, market, dto.BlockNumber); wmErr != nil {
			logger.Warn("solana_ingestion_watermark_update_failed",
				"market", market, "slot", dto.BlockNumber, "error", wmErr)
		}

		return nil
	}
}
