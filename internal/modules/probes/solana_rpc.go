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

// SolanaTokenAccount is one SPL token account entry from getProgramAccounts.
// Amount is the raw uint64 (atomic units, not decimal-adjusted).
type SolanaTokenAccount struct {
	Pubkey string
	Amount uint64
}

// RPCProgramAccountsFilter is one filter clause for getProgramAccounts.
// Exactly one of DataSize or Memcmp must be set per filter object.
type RPCProgramAccountsFilter struct {
	// DataSize filters by exact account data size in bytes.
	DataSize int
	// Memcmp filters by a byte pattern at a given offset within account data.
	Memcmp *RPCProgramAccountsMemcmp
}

// RPCProgramAccountsMemcmp is the memcmp sub-filter for getProgramAccounts.
type RPCProgramAccountsMemcmp struct {
	// Offset is the byte offset into the account data to compare.
	Offset int
	// Bytes is the base58-encoded expected bytes to match.
	Bytes string
}

// holderDistFallbackClient is the supplemental interface the fallback path in
// solana_holder_dist.go requires. It is satisfied at runtime by the
// *solanaProbeRPCAdapter in cmd/server.go, which wraps *rpc.SolanaClient.
//
// Using a separate local interface (rather than extending SolanaProbeRPCClient)
// keeps existing test stubs compilable without modification — the primary
// interface is unchanged. The fallback is activated via a type assertion
// on the injected SolanaProbeRPCClient: if it also satisfies this interface
// the fallback runs; otherwise the probe fails closed (HolderDistKnown=false).
type holderDistFallbackClient interface {
	// GetTokenSupply returns the raw total supply (u64 atomic units) and
	// decimal count for mint. Used as the denominator for Top5HolderPct.
	GetTokenSupply(ctx context.Context, mint, commitment string) (supply uint64, decimals int, err error)
	// GetProgramAccounts returns SPL token accounts owned by programID
	// that match all provided filters. The adapter applies jsonParsed
	// encoding so amounts are available without binary decoding.
	GetProgramAccounts(ctx context.Context, programID, commitment string, filters []RPCProgramAccountsFilter) ([]SolanaTokenAccount, error)
}
