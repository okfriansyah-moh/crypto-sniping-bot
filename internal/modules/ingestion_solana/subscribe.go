package ingestion_solana

// subscribe.go — helpers for building logsSubscribe parameters.
// The actual WebSocket connection is managed by the SolanaRPCClient implementation
// (injected from the worker). This file contains only pure helper types.

// SubscribeFilter describes the filter passed to logsSubscribe.
type SubscribeFilter struct {
	// Mentions holds the program ID to filter logs by.
	// Corresponds to {"mentions": [programID]} in the JSON RPC filter.
	Mentions []string
}

// Commitment is the Solana commitment level for subscriptions.
type Commitment string

const (
	CommitmentProcessed Commitment = "processed"
	CommitmentConfirmed Commitment = "confirmed"
	CommitmentFinalized Commitment = "finalized"
)
