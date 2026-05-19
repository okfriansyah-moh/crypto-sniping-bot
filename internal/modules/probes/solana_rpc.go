package probes

import (
	"context"
	"regexp"
)

// solanaAddressRE is a compile-time allowlist for Solana base58-encoded
// public keys. Solana addresses are 32–44 base58 characters (Bitcoin
// base58 alphabet: no 0, O, I, l). Invalid addresses are rejected before
// any RPC call to prevent log injection and wasted Helius credits.
var solanaAddressRE = regexp.MustCompile(`^[1-9A-HJ-NP-Za-km-z]{32,44}$`)

// isValidSolanaMint returns true iff s looks like a well-formed Solana
// base58 public key. This is a format check only — it does not verify
// that the address exists on-chain.
func isValidSolanaMint(s string) bool {
	return solanaAddressRE.MatchString(s)
}

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

// DASAsset is the minimal view of a Helius Digital Asset Standard (DAS)
// getAsset response. One DAS call replaces up to 3 separate RPC probes
// (metadata URI fetch, authority account decode, supply decode).
//
// Fields populated by Helius DAS getAsset:
//   - Supply: token_info.supply (u64 raw atomic units)
//   - Decimals: token_info.decimals
//   - Twitter, Telegram, Website: content.links fields
//   - Name, Symbol: content.metadata fields
//
// Returns nil when the asset does not exist or is not a token.
type DASAsset struct {
	// Supply is the raw u64 total supply (atomic units, before decimal adjustment).
	Supply uint64
	// Decimals is the SPL mint decimal precision.
	Decimals int
	// Twitter is the Twitter/X profile URL from DAS content.links.twitter.
	Twitter string
	// Telegram is the Telegram link from DAS content.links.telegram.
	Telegram string
	// Website is the project website from DAS content.links.website.
	Website string
	// Name is the token name from DAS content.metadata.name.
	Name string
	// Symbol is the token symbol from DAS content.metadata.symbol.
	Symbol string
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

	// GetDASAsset fetches token metadata, supply, and social links from
	// Helius Digital Asset Standard (DAS) in a single RPC call. Returns
	// (nil, nil) when the asset does not exist. Only available on Helius
	// endpoints; other providers return an "unsupported method" error.
	GetDASAsset(ctx context.Context, mint string) (*DASAsset, error)
}
