package ingestion

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/sniper-bot/internal/rpc"
)

// SubscribeConfig holds parameters for a WebSocket subscription loop.
type SubscribeConfig struct {
	Chain             string
	Market            string
	FactoryAddresses  []string
	BaseTokens        []string
	ConfirmationDepth uint32
	VersionID         string
	Endpoint          string
	// PairTokens maps pool address → [token0, token1] for Mint/Swap/Burn events.
	PairTokens map[string][2]string
}

// SubscribeLoop runs the WebSocket eth_subscribe("logs") loop for a single chain.
// Normalizes each received log and calls emit(ctx, dto).
// Stops cleanly when ctx is cancelled or the subscription channel is closed.
func SubscribeLoop(
	ctx context.Context,
	cfg SubscribeConfig,
	client rpc.Client,
	emit EventEmitter,
	logger *slog.Logger,
) error {
	topicFilter := [][]string{
		{TopicPairCreated, TopicMint, TopicSwap, TopicBurn},
	}

	logCh, err := client.SubscribeLogs(ctx, cfg.FactoryAddresses, topicFilter)
	if err != nil {
		return fmt.Errorf("subscribe_logs: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case l, ok := <-logCh:
			if !ok {
				return nil // channel closed — caller handles reconnect
			}
			emitLog(ctx, cfg, client, l, emit, logger, "websocket")
		}
	}
}

// emitLog normalizes one log and calls emit.
// Single-log errors are logged and skipped — MUST NOT halt ingestion.
func emitLog(
	ctx context.Context,
	cfg SubscribeConfig,
	client rpc.Client,
	l rpc.Log,
	emit EventEmitter,
	logger *slog.Logger,
	transport string,
) {
	if len(l.Topics) == 0 || !IsKnownTopic(l.Topics[0]) {
		return
	}

	if l.BlockTimestamp == "" && client != nil {
		if ts, tsErr := client.GetBlockTimestamp(ctx, l.BlockNumber); tsErr == nil {
			l.BlockTimestamp = ts
		}
	}

	// Derive ingestedAt from the block timestamp for deterministic replay:
	// same log always produces the same DTO regardless of wall-clock time.
	// Fall back to wall clock only when the node does not provide a timestamp.
	ingestedAt := l.BlockTimestamp
	if ingestedAt == "" {
		ingestedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}

	dto, err := dispatchNormalize(l, cfg, ingestedAt, transport)
	if err != nil {
		logger.Error("normalize_log_failed",
			"chain", cfg.Chain, "tx_hash", l.TxHash,
			"log_index", l.LogIndex, "error", err)
		return
	}

	// Apply full confirmation-depth reorg check (overrides the l.Removed-only
	// default set by normalize functions). Best-effort: if LatestBlock fails,
	// dto.Reorged retains the l.Removed-based value.
	if client != nil && cfg.ConfirmationDepth > 0 {
		if lb, lbErr := client.LatestBlock(ctx); lbErr == nil {
			dto.Reorged = IsReorged(l, lb, cfg.ConfirmationDepth)
		}
	}

	if emitErr := emit(ctx, dto); emitErr != nil {
		logger.Error("emit_dto_failed",
			"event_id", dto.EventID, "chain", cfg.Chain, "error", emitErr)
	}
}

// dispatchNormalize routes a log to the correct normalize function.
func dispatchNormalize(l rpc.Log, cfg SubscribeConfig, ingestedAt, transport string) (contracts.MarketDataDTO, error) {
	topic := l.Topics[0]

	switch topic {
	case TopicPairCreated:
		dto, err := NormalizePairCreated(l, cfg.Chain, cfg.Market, cfg.Endpoint, cfg.VersionID,
			cfg.BaseTokens, cfg.ConfirmationDepth, ingestedAt)
		if err != nil {
			return contracts.MarketDataDTO{}, err
		}
		dto.Transport = transport
		if cfg.PairTokens != nil {
			cfg.PairTokens[dto.PoolAddress] = [2]string{dto.Token0Address, dto.Token1Address}
		}
		return dto, nil

	case TopicMint, TopicSwap, TopicBurn:
		// Guard against uninitialized PairTokens — will panic on nil map access.
		if cfg.PairTokens == nil {
			return contracts.MarketDataDTO{}, fmt.Errorf("pair_tokens not initialized: cannot handle %s for pool %s", TopicToEventName(topic), l.Address)
		}
		poolAddr := l.Address
		pair, ok := cfg.PairTokens[poolAddr]
		if !ok {
			return contracts.MarketDataDTO{}, fmt.Errorf("unknown pair %s for %s", poolAddr, TopicToEventName(topic))
		}
		token0, token1 := pair[0], pair[1]
		var dto contracts.MarketDataDTO
		var err error
		switch topic {
		case TopicMint:
			dto, err = NormalizeMint(l, cfg.Chain, cfg.Market, cfg.Endpoint, cfg.VersionID,
				token0, token1, cfg.BaseTokens, cfg.ConfirmationDepth, ingestedAt)
		case TopicSwap:
			dto, err = NormalizeSwap(l, cfg.Chain, cfg.Market, cfg.Endpoint, cfg.VersionID,
				token0, token1, cfg.BaseTokens, cfg.ConfirmationDepth, ingestedAt)
		default: // TopicBurn
			dto, err = NormalizeBurn(l, cfg.Chain, cfg.Market, cfg.Endpoint, cfg.VersionID,
				token0, token1, cfg.BaseTokens, cfg.ConfirmationDepth, ingestedAt)
		}
		if err != nil {
			return contracts.MarketDataDTO{}, err
		}
		dto.Transport = transport
		return dto, nil

	default:
		return contracts.MarketDataDTO{}, fmt.Errorf("unknown topic: %s", topic)
	}
}
