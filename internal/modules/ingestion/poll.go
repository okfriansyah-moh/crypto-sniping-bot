package ingestion

import (
	"context"
	"log/slog"
	"time"

	"crypto-sniping-bot/internal/rpc"
)

// PollConfig holds parameters for the HTTP polling fallback loop.
type PollConfig struct {
	Chain              string
	Market             string
	FactoryAddresses   []string
	BaseTokens         []string
	ConfirmationDepth  uint32
	VersionID          string
	Endpoint           string
	PollIntervalMs     int
	LastProcessedBlock uint64
	PairTokens         map[string][2]string
}

// PollLoop runs the HTTP eth_getLogs polling fallback loop.
// It polls for new blocks every PollIntervalMs milliseconds.
// Used when WebSocket subscription is unavailable or degraded.
// Stops when ctx is cancelled.
func PollLoop(
	ctx context.Context,
	cfg PollConfig,
	client rpc.Client,
	emit EventEmitter,
	logger *slog.Logger,
) error {
	if cfg.PollIntervalMs <= 0 {
		cfg.PollIntervalMs = 2000 // 2s default
	}

	lastBlock := cfg.LastProcessedBlock
	ticker := time.NewTicker(time.Duration(cfg.PollIntervalMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		latest, err := client.LatestBlock(ctx)
		if err != nil {
			logger.Warn("poll_latest_block_failed", "chain", cfg.Chain, "error", err)
			continue
		}

		if latest <= lastBlock {
			continue
		}

		fromBlock := lastBlock + 1
		toBlock := latest

		logs, err := RecoverGap(ctx, client, cfg.FactoryAddresses, [][]string{
			{TopicPairCreated, TopicMint, TopicSwap, TopicBurn},
		}, fromBlock, toBlock)
		if err != nil {
			logger.Warn("poll_get_logs_failed",
				"chain", cfg.Chain, "from", fromBlock, "to", toBlock, "error", err)
			continue
		}

		for _, l := range logs {
			if l.BlockTimestamp == "" {
				if ts, tsErr := client.GetBlockTimestamp(ctx, l.BlockNumber); tsErr == nil {
					l.BlockTimestamp = ts
				}
			}

			// Derive ingestedAt from block timestamp for deterministic replay;
			// fall back to wall clock only when the node provides no timestamp.
			ingestedAt := l.BlockTimestamp
			if ingestedAt == "" {
				ingestedAt = time.Now().UTC().Format(time.RFC3339Nano)
			}
			subCfg := SubscribeConfig{
				Chain:             cfg.Chain,
				Market:            cfg.Market,
				FactoryAddresses:  cfg.FactoryAddresses,
				BaseTokens:        cfg.BaseTokens,
				ConfirmationDepth: cfg.ConfirmationDepth,
				VersionID:         cfg.VersionID,
				Endpoint:          cfg.Endpoint,
				PairTokens:        cfg.PairTokens,
			}
			dto, normErr := dispatchNormalize(l, subCfg, ingestedAt, "polling")
			if normErr != nil {
				logger.Error("poll_normalize_failed",
					"chain", cfg.Chain, "tx_hash", l.TxHash, "log_index", l.LogIndex, "error", normErr)
				continue
			}

			if emitErr := emit(ctx, dto); emitErr != nil {
				logger.Error("poll_emit_failed",
					"event_id", dto.EventID, "error", emitErr)
			}
		}

		lastBlock = toBlock
		logger.Info("poll_processed",
			"chain", cfg.Chain, "from", fromBlock, "to", toBlock,
			"logs_count", len(logs))
	}
}
