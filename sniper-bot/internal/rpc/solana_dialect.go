// solana_dialect.go — provider-specific behaviour adapters for Solana RPC.
//
// Both QuickNode and Helius implement the standard Solana JSON-RPC specification
// (getTransaction, logsSubscribe, etc.) with identical method signatures and
// response shapes.  The differences that matter operationally are:
//
//	Provider   | Rate-limit codes  | WS inactivity timer | Recommended ping
//	-----------+-------------------+---------------------+-----------------
//	QuickNode  | -32003            | ~60 s (undocumented)| 20 s
//	Helius     | -32003, -32429    | 10 min (documented) | every ~60 s
//
// Adding a new provider:
//  1. Implement ProviderDialect (three methods).
//  2. Add a URL-pattern match in detectDialect.
//  3. No other files need to change.
package rpc

import (
	"strings"
	"time"
)

// ProviderDialect captures the provider-specific behaviours that vary between
// Solana RPC providers.  The rest of SolanaClient is provider-agnostic.
type ProviderDialect interface {
	// Name returns the provider label used in log fields.
	Name() string

	// IsRateLimited returns true when the JSON-RPC error code indicates quota
	// or rate-limit exhaustion.  The caller should back off and/or rotate to
	// the next endpoint.
	IsRateLimited(code int) bool

	// WSPingInterval returns how frequently to send WebSocket keepalive pings.
	// Each provider has a different idle-connection timeout policy.
	WSPingInterval() time.Duration
}

// ── QuickNode ──────────────────────────────────────────────────────────────────

// quicknodeDialect implements ProviderDialect for QuickNode endpoints.
type quicknodeDialect struct{}

func (quicknodeDialect) Name() string { return "quicknode" }

// IsRateLimited detects QuickNode quota errors:
//
//	-32003 = daily plan limit reached (hard cap)
//	-32007 = per-second rate limit (15/s on free tier)
func (quicknodeDialect) IsRateLimited(code int) bool { return code == -32003 || code == -32007 }

// WSPingInterval returns QuickNode's safe ping cadence (20 s).
func (quicknodeDialect) WSPingInterval() time.Duration { return 20 * time.Second }

// ── Helius ─────────────────────────────────────────────────────────────────────

// heliusDialect implements ProviderDialect for Helius endpoints.
//
// Helius WSS has a documented 10-minute inactivity timer.  They recommend
// pinging "every minute"; 30 s gives comfortable headroom while keeping the
// connection alive through quiet slots.
//
// Helius may return -32429 (Too Many Requests) in addition to the standard
// Solana -32003 quota error.
type heliusDialect struct{}

func (heliusDialect) Name() string { return "helius" }

// IsRateLimited detects both Helius quota codes.
func (heliusDialect) IsRateLimited(code int) bool {
	return code == -32003 || code == -32429
}

// WSPingInterval returns a ping cadence safe for Helius (30 s).
func (heliusDialect) WSPingInterval() time.Duration { return 30 * time.Second }

// ── Generic fallback ───────────────────────────────────────────────────────────

// genericDialect is used when the provider cannot be identified from the URL
// or config hint.  It uses conservative defaults compatible with any standard
// Solana RPC node.
type genericDialect struct{}

func (genericDialect) Name() string                  { return "generic" }
func (genericDialect) IsRateLimited(code int) bool   { return code == -32003 }
func (genericDialect) WSPingInterval() time.Duration { return 30 * time.Second }

// ── detectDialect ──────────────────────────────────────────────────────────────

// detectDialect returns the ProviderDialect for an endpoint.
//
// providerHint is the optional "provider" field from chains.yaml.  When set it
// takes precedence.  Otherwise the endpoint URL is pattern-matched.
func detectDialect(providerHint, endpointURL string) ProviderDialect {
	switch strings.ToLower(strings.TrimSpace(providerHint)) {
	case "quicknode", "qn":
		return quicknodeDialect{}
	case "helius":
		return heliusDialect{}
	}

	// Auto-detect from URL.
	lower := strings.ToLower(endpointURL)
	switch {
	case strings.Contains(lower, "helius-rpc.com") ||
		strings.Contains(lower, "helius.dev") ||
		strings.Contains(lower, "mainnet.helius") ||
		strings.Contains(lower, "devnet.helius"):
		return heliusDialect{}
	case strings.Contains(lower, "quiknode.pro") ||
		strings.Contains(lower, "quicknode.com"):
		return quicknodeDialect{}
	default:
		return genericDialect{}
	}
}
