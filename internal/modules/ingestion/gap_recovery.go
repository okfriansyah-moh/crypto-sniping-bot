package ingestion

import (
	"context"
	"fmt"
	"sort"

	"crypto-sniping-bot/internal/rpc"
)

// RecoverGap fetches logs for [fromBlock, toBlock] inclusive via HTTP eth_getLogs.
// Results are sorted deterministically: block_number ASC, log_index ASC.
// This ensures replay produces identical output for the same gap.
func RecoverGap(
	ctx context.Context,
	client rpc.Client,
	addresses []string,
	topics [][]string,
	fromBlock, toBlock uint64,
) ([]rpc.Log, error) {
	if fromBlock > toBlock {
		return nil, fmt.Errorf("gap_recovery: fromBlock %d > toBlock %d", fromBlock, toBlock)
	}

	logs, err := client.GetLogs(ctx, fromBlock, toBlock, addresses, topics)
	if err != nil {
		return nil, fmt.Errorf("gap_recovery: get_logs [%d, %d]: %w", fromBlock, toBlock, err)
	}

	// Sort for deterministic replay: block ASC, then log_index ASC.
	sort.Slice(logs, func(i, j int) bool {
		if logs[i].BlockNumber != logs[j].BlockNumber {
			return logs[i].BlockNumber < logs[j].BlockNumber
		}
		return logs[i].LogIndex < logs[j].LogIndex
	})

	return logs, nil
}
