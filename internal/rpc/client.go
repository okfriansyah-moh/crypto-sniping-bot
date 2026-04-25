// Package rpc provides the RPC client interface for connecting to EVM nodes.
// Implementations are injected into the ingestion module — modules never hold
// concrete RPC drivers.
package rpc

import "context"

// Log is a normalized on-chain event log.
// Fields mirror the JSON-RPC eth_getLogs response with typed fields.
type Log struct {
	BlockNumber    uint64
	BlockHash      string
	TxHash         string
	LogIndex       uint32
	Address        string   // contract address, lowercase 0x-prefixed
	Topics         []string // hex topics[0] = event signature hash
	Data           string   // hex-encoded non-indexed data (no 0x prefix)
	Removed        bool     // true = reorged out
	BlockTimestamp string   // ISO 8601 UTC; populated by normalizer
}

// ClientFactory creates an RPC Client for the given endpoint URL.
// The ingestion module uses a factory to create a fresh client per reconnect
// attempt, enabling true endpoint failover across WSEndpoints/RPCEndpoints.
type ClientFactory func(ctx context.Context, endpoint string) (Client, error)

// Client is the minimal RPC connectivity interface.
// All implementations must be safe for concurrent use.
type Client interface {
	// SubscribeLogs opens a WebSocket eth_subscribe("logs") for the given
	// filter and delivers matching logs on the returned channel until ctx is
	// cancelled or the connection drops.
	SubscribeLogs(ctx context.Context, addresses []string, topics [][]string) (<-chan Log, error)

	// GetLogs fetches historical logs for [fromBlock, toBlock] via eth_getLogs.
	GetLogs(ctx context.Context, fromBlock, toBlock uint64, addresses []string, topics [][]string) ([]Log, error)

	// GetBlockTimestamp returns the UTC timestamp for a block as ISO 8601.
	GetBlockTimestamp(ctx context.Context, blockNumber uint64) (string, error)

	// LatestBlock returns the current chain head block number.
	LatestBlock(ctx context.Context) (uint64, error)

	// Ping checks connectivity to the RPC endpoint.
	Ping(ctx context.Context) error

	// Endpoint returns the URL of this RPC connection.
	Endpoint() string
}
