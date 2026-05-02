package probes

import "context"

// SolanaAccountData is the minimal account view the Solana enrichment
// probes consume. The DataB64 field carries the base64-encoded account
// data exactly as Solana JSON-RPC returns it under encoding="base64".
type SolanaAccountData struct {
	DataB64 string
	Owner   string
	Slot    uint64
}

// SolanaTokenHolder is one entry in a getTokenLargestAccounts response.
// Amount is the raw token-account amount (uint64 serialized as string by
// the JSON-RPC layer); Decimals is the SPL mint's decimal count.
type SolanaTokenHolder struct {
	Address  string
	Amount   string
	Decimals int
}

// SolanaProbeRPCClient is the narrow surface the Solana enrichment
// probes need. Defined here (not imported from internal/rpc) so the
// probes package stays a leaf — the concrete *rpc.SolanaClient is
// adapted to this interface in cmd/server.go.
type SolanaProbeRPCClient interface {
	// GetAccountInfo returns the on-chain account at pubkey, encoded as
	// base64. Returns (nil, nil) when the account does not exist.
	GetAccountInfo(ctx context.Context, pubkey, commitment string) (*SolanaAccountData, error)

	// GetTokenLargestAccounts returns up to 20 largest token-account
	// holders for an SPL mint, ordered by amount descending.
	GetTokenLargestAccounts(ctx context.Context, mint, commitment string) ([]SolanaTokenHolder, error)
}
