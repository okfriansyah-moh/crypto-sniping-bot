package ingestion_solana

import "context"

// subscribe.go — helpers for building logsSubscribe parameters and optional
// subscription-method extension interfaces.
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

// TransactionSubscriber is an optional extension of SolanaRPCClient.
// Programs configured with subscription_method: "transactionSubscribe" require
// a client that satisfies this interface. The core SolanaRPCClient interface is
// intentionally unchanged to preserve all existing mock implementations.
//
// In production, internal/rpc.SolanaClient implements TransactionSubscriber.
// Test mocks that do not implement this interface trigger the fallback path in
// runSubscribeLoop (falls back to logsSubscribe with a warning log).
type TransactionSubscriber interface {
	// SubscribeTransactions opens a transactionSubscribe WebSocket subscription
	// filtered by accountFilter (passed as accountInclude). The returned channel
	// receives LogsNotification values extracted from matching transactions'
	// meta.logMessages, enabling the same downstream log-decode and tx-fetch
	// paths as logsSubscribe without modification.
	SubscribeTransactions(ctx context.Context, programID string, accountFilter string) (<-chan LogsNotification, error)
}
