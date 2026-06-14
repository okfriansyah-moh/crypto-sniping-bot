// Package workers contains dispatcher functions that wire database adapters
// to ingestion module callbacks. Workers are the ONLY components allowed to
// call adapter methods — modules never import database/.
package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/sniper-bot/internal/modules/ingestion"
	"crypto-sniping-bot/sniper-bot/internal/rpc"
)

// RunIngestion starts the ingestion module for every chain defined in cfg.Chains.
// It is the ONLY component allowed to call adapter.InsertEvent and adapter.InsertMarketData.
//
// factory is a ClientFactory used to create a fresh RPC client per reconnect attempt.
// When factory is nil, modules run in no-op mode (useful in integration tests).
//
// Flow:
//  1. Get active strategy version (pins VersionID for the run).
//  2. For each configured chain (sorted for determinism): get ingestion watermark.
//  3. Create EventEmitter wrapping adapter.InsertEvent + adapter.InsertMarketData.
//  4. Create ingestion.Module with chain config + emit callback + client factory.
//  5. Start module goroutine; block until ctx cancelled.
func RunIngestion(ctx context.Context, adapter database.Adapter, cfg *config.Config, factory rpc.ClientFactory, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	// Step 1: Pin active strategy version.
	sv, err := adapter.GetActiveStrategyVersion(ctx)
	if err != nil {
		return fmt.Errorf("run_ingestion: get active strategy version: %w", err)
	}
	versionID := sv.StrategyVersionID
	logger.Info("ingestion_version_pinned", "version_id", versionID)

	if len(cfg.Chains) == 0 {
		logger.Info("ingestion_no_chains_configured")
		<-ctx.Done()
		return ctx.Err()
	}

	// Sort chain keys for deterministic startup order.
	chainKeys := make([]string, 0, len(cfg.Chains))
	for k := range cfg.Chains {
		chainKeys = append(chainKeys, k)
	}
	sort.Strings(chainKeys)

	// Collect modules.
	modules := make([]*ingestion.Module, 0, len(chainKeys))

	for _, chainKey := range chainKeys {
		chainCfg := cfg.Chains[chainKey]

		// Step 2: Get watermark (last processed block) for gap recovery on restart.
		lastBlock, wErr := adapter.GetIngestionWatermark(ctx, chainKey)
		if wErr != nil {
			logger.Warn("ingestion_watermark_failed",
				"chain", chainKey, "error", wErr)
			lastBlock = 0
		}
		logger.Info("ingestion_watermark",
			"chain", chainKey, "last_block", lastBlock)

		// Collect factory addresses and base token addresses.
		factories := make([]string, 0, len(chainCfg.Factories))
		for _, f := range chainCfg.Factories {
			factories = append(factories, f.Address)
		}
		baseTokens := make([]string, 0, len(chainCfg.BaseTokens))
		for _, bt := range chainCfg.BaseTokens {
			baseTokens = append(baseTokens, bt.Address)
		}

		// Pick market label from first factory (if any).
		market := chainKey
		if len(chainCfg.Factories) > 0 {
			market = chainCfg.Factories[0].Market
		}

		// Step 3: Build EventEmitter closure.
		chainKey := chainKey // capture loop variable
		capturedAdapter := adapter
		capturedVersionID := versionID
		capturedLogger := logger

		emit := func(emitCtx context.Context, dto contracts.MarketDataDTO) error {
			payload, marshalErr := json.Marshal(dto)
			if marshalErr != nil {
				return fmt.Errorf("emit: marshal dto: %w", marshalErr)
			}

			// CausationID is nil for Layer 0 root events.
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
				VersionID:     capturedVersionID,
			}

			if insertErr := capturedAdapter.InsertEvent(emitCtx, evt); insertErr != nil {
				capturedLogger.Error("emit_insert_event_failed",
					"event_id", dto.EventID, "chain", chainKey, "error", insertErr)
				return fmt.Errorf("emit: insert event: %w", insertErr)
			}

			if insertErr := capturedAdapter.InsertMarketData(emitCtx, dto); insertErr != nil {
				capturedLogger.Error("emit_insert_market_data_failed",
					"event_id", dto.EventID, "chain", chainKey, "error", insertErr)
				return fmt.Errorf("emit: insert market data: %w", insertErr)
			}

			// Update watermark after successful emit.
			if wmErr := capturedAdapter.UpsertIngestionWatermark(emitCtx, chainKey, dto.BlockNumber); wmErr != nil {
				capturedLogger.Warn("ingestion_watermark_update_failed",
					"chain", chainKey, "block", dto.BlockNumber, "error", wmErr)
				// Non-fatal: watermark is best-effort.
			}

			return nil
		}

		// Step 4: Build ingestion.Config.
		ingCfg := ingestion.Config{
			Chain:             chainKey,
			Market:            market,
			FactoryAddresses:  factories,
			BaseTokens:        baseTokens,
			WSEndpoints:       chainCfg.WSEndpoints,
			RPCEndpoints:      chainCfg.RPCEndpoints,
			ConfirmationDepth: chainCfg.ConfirmationDepth,
			Backoff: ingestion.BackoffConfig{
				InitialMs:  chainCfg.Backoff.InitialMs,
				MaxMs:      chainCfg.Backoff.MaxMs,
				Multiplier: chainCfg.Backoff.Multiplier,
			},
			PollIntervalMs:     chainCfg.PollIntervalMs,
			HeartbeatInterval:  chainCfg.HeartbeatIntervalMs,
			HeartbeatTimeout:   chainCfg.HeartbeatTimeoutMs,
			LastProcessedBlock: lastBlock, // seeds gap recovery on restart
		}

		mod := ingestion.New(ingCfg, versionID, emit, logger)
		if factory != nil {
			mod.WithClientFactory(factory)
		}
		modules = append(modules, mod)
	}

	// Step 5: Start all modules concurrently.
	errCh := make(chan error, len(modules))
	for _, mod := range modules {
		mod := mod
		go func() {
			if err := mod.Start(ctx); err != nil && err != ctx.Err() {
				errCh <- err
			}
		}()
	}

	// Block until context is cancelled or a module fails.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return fmt.Errorf("run_ingestion: module error: %w", err)
	}
}
