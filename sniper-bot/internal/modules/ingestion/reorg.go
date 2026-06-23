package ingestion

import "crypto-sniping-bot/sniper-bot/internal/rpc"

// IsReorged returns true when a log should be treated as a chain reorganisation.
//
// A log is a reorg if:
//   - log.Removed = true (explicitly removed by the node during a reorg), OR
//   - the log's block depth from the chain head is shallower than the configured
//     confirmation depth (i.e., the block is not yet final).
//
// latestBlock is the current chain head block number fetched from the RPC node.
func IsReorged(l rpc.Log, latestBlock uint64, confirmationDepth uint32) bool {
	if l.Removed {
		return true
	}
	if confirmationDepth == 0 {
		return false
	}
	// Block depth = latestBlock - l.BlockNumber.
	// If latestBlock < l.BlockNumber (e.g. during tests), treat as unconfirmed.
	if latestBlock < l.BlockNumber {
		return true
	}
	depth := latestBlock - l.BlockNumber
	return depth < uint64(confirmationDepth)
}
