package ingestion_solana

import (
	"sync"
)

// mint_pair.go — deterministic tradable-mint resolution for Solana AMM pool events.
//
// PumpFun AMM CreatePool instructions may place wrapped SOL in either the base or
// quote mint account slot depending on pool orientation. Blind use of the baseMint
// account as TokenAddress caused ~92% of emissions to use WSOL as the token (gate run
// 20260612_174145). ResolveTradableMint picks the non-stable side as the tradable mint.

// WrappedSOLMint is the canonical wrapped SOL mint on Solana (also used as the
// native SOL placeholder in many program account layouts).
const WrappedSOLMint = "So11111111111111111111111111111111111111112"

var (
	stableMintsMu sync.RWMutex
	stableMints   = []string{WrappedSOLMint}
)

// ConfigureStableMints sets the package-level stable/system mint list used by
// IsSystemMint and ResolveTradableMint. Called from Module.New with config values.
// When mints is empty, falls back to WSOL only.
func ConfigureStableMints(mints []string) {
	stableMintsMu.Lock()
	defer stableMintsMu.Unlock()
	if len(mints) == 0 {
		stableMints = []string{WrappedSOLMint}
		return
	}
	stableMints = append([]string(nil), mints...)
}

func currentStableMints() []string {
	stableMintsMu.RLock()
	defer stableMintsMu.RUnlock()
	out := make([]string, len(stableMints))
	copy(out, stableMints)
	return out
}

// IsStableMint reports whether addr is in the provided stable mint list.
// When mints is empty, only WrappedSOLMint is checked.
func IsStableMint(addr string, mints []string) bool {
	if len(mints) == 0 {
		mints = []string{WrappedSOLMint}
	}
	for _, m := range mints {
		if addr == m {
			return true
		}
	}
	return false
}

// IsSystemMint reports whether addr is a configured system/stable mint that must
// never be emitted as MarketDataDTO.TokenAddress.
func IsSystemMint(addr string) bool {
	return IsStableMint(addr, currentStableMints())
}

// ResolveTradableMint returns the tradable project mint and quote/stable side for
// a two-asset pool. When exactly one side is a stable mint, the other side is the
// token. When both sides are stable (or both empty), ok is false. When neither is
// stable, baseMint is treated as the graduated token (IDL default orientation).
func ResolveTradableMint(baseMint, quoteMint string) (tokenMint, baseAddr string, ok bool) {
	mints := currentStableMints()
	baseStable := IsStableMint(baseMint, mints)
	quoteStable := IsStableMint(quoteMint, mints)

	switch {
	case baseStable && quoteStable:
		return "", "", false
	case baseStable && quoteMint != "":
		return quoteMint, baseMint, true
	case quoteStable && baseMint != "":
		return baseMint, quoteMint, true
	case baseMint == "":
		return "", "", false
	default:
		return baseMint, quoteMint, true
	}
}

// MintPairWasSwapped reports whether ResolveTradableMint would use the quote
// slot as TokenAddress because the stable mint occupied the base slot.
func MintPairWasSwapped(baseMint, quoteMint string) bool {
	mints := currentStableMints()
	return IsStableMint(baseMint, mints) && quoteMint != "" && !IsStableMint(quoteMint, mints)
}
