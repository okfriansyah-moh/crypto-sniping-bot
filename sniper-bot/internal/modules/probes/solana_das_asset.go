package probes

// solana_das_asset.go — Helius Digital Asset Standard (DAS) enrichment probe.
//
// Problem this probe solves:
//
//   The standard probe pipeline issues 3+ separate Helius RPC calls per token:
//     1. solana_authorities: getAccountInfo (supply + authority decode)
//     2. solana_metadata: HTTP fetch of the off-chain metadata URI (social links)
//     3. (optionally) additional authority checks
//
//   Helius DAS getAsset returns supply, decimals, social links, name, and symbol
//   in a single call — folding multiple probes into one and cutting ~50-70% of
//   Helius credits consumed by the enrichment pipeline.
//
// Integration:
//   When enabled, this probe populates:
//     - TotalSupply / TotalSupplyKnown (replaces LP probe supply extraction)
//     - HasSocialLinks / SocialLinksKnown (replaces solana_metadata HTTP fetch)
//   Both probes still run after this one; they are no-ops when the *Known flag
//   is already true, so the DAS probe acts as a fast-path cache with no
//   behavioural change to the DQ layer.
//
// Security constraints:
//   - HTTPS-only: uses the same SolanaProbeRPCClient.GetDASAsset that goes
//     through the existing internal/rpc SolanaClient HTTP path with bounded
//     response body (solanaMaxResponseBytes) and per-request timeout.
//   - Social link validation: applies the same isSocialProfileURL and
//     isTwitterProfileURL gates already enforced in solana_metadata.go.
//   - Fail-open: any error leaves the *Known flags false (DQ degrades per
//     operational-mode profile). This probe MUST NOT block the pipeline.
//
// Design constraints (see probes.go):
//   - Pure-ish: no database calls.
//   - Safe with no configuration: disabled → (in, nil).
//   - Solana-only: non-Solana inputs pass through unchanged.
//   - Caches results in memory for CacheTTLSec (default 1h) to avoid
//     redundant DAS calls on rescan events.

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"crypto-sniping-bot/shared/contracts"
)

// SolanaDASAssetConfig configures the solana_das_asset probe.
type SolanaDASAssetConfig struct {
	Enabled     bool `yaml:"enabled"`
	TimeoutMs   int  `yaml:"timeout_ms"`
	CacheTTLSec int  `yaml:"cache_ttl_sec"` // default 3600
}

// dasAssetResult is the cached outcome of a single DAS getAsset call.
type dasAssetResult struct {
	totalSupply      float64
	totalSupplyKnown bool
	hasSocialLinks   bool
	socialLinksKnown bool
	fetchedAt        time.Time
}

// SolanaDASAssetProbe calls Helius DAS getAsset to populate TotalSupply,
// HasSocialLinks and their *Known flags in a single RPC call. Downstream
// probes (solana_pumpfun_lp, solana_metadata) are still registered; when
// their *Known flag is already true on entry they skip their own RPC call.
type SolanaDASAssetProbe struct {
	rpc    SolanaProbeRPCClient
	cfg    SolanaDASAssetConfig
	logger *slog.Logger
	cache  sync.Map
}

// NewSolanaDASAssetProbe creates a DAS asset probe. Pass nil rpc to run in
// no-op mode (useful in tests when DAS is not available).
func NewSolanaDASAssetProbe(rpc SolanaProbeRPCClient, cfg SolanaDASAssetConfig, logger *slog.Logger) *SolanaDASAssetProbe {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.CacheTTLSec == 0 {
		cfg.CacheTTLSec = 3600
	}
	return &SolanaDASAssetProbe{rpc: rpc, cfg: cfg, logger: logger}
}

// StartEviction launches a background goroutine that periodically removes
// expired cache entries to prevent unbounded sync.Map growth. Call once
// after construction, passing the application-lifetime context. The
// goroutine stops when ctx is cancelled.
func (p *SolanaDASAssetProbe) StartEviction(ctx context.Context) {
	if p.cfg.CacheTTLSec <= 0 {
		return // caching disabled or invalid config — no eviction loop needed
	}
	ttl := time.Duration(p.cfg.CacheTTLSec) * time.Second
	go func() {
		ticker := time.NewTicker(ttl)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := time.Now()
				p.cache.Range(func(k, v any) bool {
					if now.Sub(v.(dasAssetResult).fetchedAt) >= ttl {
						p.cache.Delete(k)
					}
					return true
				})
			}
		}
	}()
}

func (p *SolanaDASAssetProbe) Name() string { return "solana_das_asset" }

// Probe calls getAsset and populates TotalSupply and HasSocialLinks when
// not already set by a prior probe. Non-Solana inputs pass through unchanged.
//
// Credit profile: 1 Helius DAS credit per call (same as getAccountInfo).
// With caching, the effective cost converges to 1 call per token per TTL.
func (p *SolanaDASAssetProbe) Probe(ctx context.Context, in contracts.MarketDataDTO) (contracts.MarketDataDTO, error) {
	if !p.cfg.Enabled {
		return in, nil
	}
	if !strings.EqualFold(in.Chain, "solana") {
		return in, nil
	}
	mint := strings.TrimSpace(in.TokenAddress)
	if mint == "" {
		return in, errors.New("probes/solana_das_asset: empty token address")
	}
	// Reject addresses that are not valid Solana base58 public keys.
	// Fail-open: malformed input should not block the pipeline.
	if !isValidSolanaMint(mint) {
		return in, nil
	}
	if p.rpc == nil {
		return in, errors.New("probes/solana_das_asset: nil rpc client")
	}

	// Skip if downstream probes already populated both known fields.
	if in.TotalSupplyKnown && in.SocialLinksKnown {
		return in, nil
	}

	// Fast path: return cached result if within TTL.
	ttl := time.Duration(p.cfg.CacheTTLSec) * time.Second
	if cached, ok := p.cache.Load(mint); ok {
		r := cached.(dasAssetResult)
		if time.Since(r.fetchedAt) < ttl {
			return p.applyResult(in, r), nil
		}
	}

	timeout := time.Duration(p.cfg.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 3000 * time.Millisecond
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	asset, err := p.rpc.GetDASAsset(cctx, mint)
	if err != nil {
		// Fail-open: DAS unavailable (non-Helius endpoint, rate limit, etc.)
		// Leave *Known flags false; pipeline degrades per operational-mode profile.
		p.logger.Warn("solana_das_asset: get_das_asset_failed",
			"mint", mint,
			"error", err,
		)
		return in, nil
	}
	if asset == nil {
		// Asset not found in DAS index (very new mint, not yet indexed).
		// Fail-open — do not treat as an error.
		return in, nil
	}

	r := p.buildResult(asset)
	r.fetchedAt = time.Now()
	p.cache.Store(mint, r)
	return p.applyResult(in, r), nil
}

// buildResult converts a DASAsset response into a cached result record.
func (p *SolanaDASAssetProbe) buildResult(asset *DASAsset) dasAssetResult {
	r := dasAssetResult{
		socialLinksKnown: true, // DAS always returns a links block (may be empty)
	}

	// Supply: convert raw u64 atomic units to float64 decimal-adjusted value.
	// DAS provides decimals alongside supply so we can do the conversion here.
	if asset.Supply > 0 {
		divisor := math.Pow10(asset.Decimals)
		if divisor > 0 {
			r.totalSupply = float64(asset.Supply) / divisor
		} else {
			r.totalSupply = float64(asset.Supply)
		}
		r.totalSupplyKnown = true
	}

	// Social links: apply the same validation gates as solana_metadata.go to
	// avoid accepting DEX scanner URLs or non-profile Twitter links.
	// Pass asset.Name / asset.Symbol so the profileAssociatedWithToken guard
	// (same logic as solana_metadata.go) can check handle/name association.
	hasTwitter := isSocialProfileURL("twitter", asset.Twitter, asset.Name, asset.Symbol)
	hasTelegram := isSocialProfileURL("telegram", asset.Telegram, asset.Name, asset.Symbol)
	hasWebsite := isSocialProfileURL("website", asset.Website, asset.Name, asset.Symbol)
	r.hasSocialLinks = hasTwitter || hasTelegram || hasWebsite

	return r
}

// applyResult merges a cached result into the output DTO without overwriting
// fields already set by an earlier probe (respect first-writer wins per
// *Known flag semantics).
func (p *SolanaDASAssetProbe) applyResult(in contracts.MarketDataDTO, r dasAssetResult) contracts.MarketDataDTO {
	out := in
	if !out.TotalSupplyKnown && r.totalSupplyKnown {
		out.TotalSupply = r.totalSupply
		out.TotalSupplyKnown = true
	}
	if !out.SocialLinksKnown && r.socialLinksKnown {
		out.HasSocialLinks = r.hasSocialLinks
		out.SocialLinksKnown = true
	}
	return out
}
