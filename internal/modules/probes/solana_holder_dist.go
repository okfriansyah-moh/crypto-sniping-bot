package probes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"crypto-sniping-bot/contracts"
)

// SolanaHolderDistConfig configures the solana_holder_dist probe.
type SolanaHolderDistConfig struct {
	Enabled    bool   `yaml:"enabled"`
	TimeoutMs  int    `yaml:"timeout_ms"`
	Commitment string `yaml:"commitment"`
	// TopK is the number of largest holders summed for Top5HolderPct.
	// MUST be 5 — the DTO field is named Top5HolderPct and the downstream
	// data-quality layer interprets it as such. Any value other than 5 is
	// clamped back to 5 by NewSolanaHolderDistProbe to avoid semantic
	// corruption. To support a different K, a new DTO field is required.
	TopK int `yaml:"top_k"`
	// CacheTTLSec is how long a cached holder distribution result is served
	// before the next probe call re-fetches from chain. Defaults to 3600 (1h)
	// when 0 or unset. To disable caching entirely use a negative value (e.g. -1),
	// which skips both the cache-hit fast path and the StartEviction loop.
	CacheTTLSec int `yaml:"cache_ttl_sec"`
	// FallbackEnabled controls whether the fallback path (getTokenSupply +
	// getProgramAccounts) is attempted when getTokenLargestAccounts times out
	// or returns an error. Defaults to true. The fallback costs +11 Helius
	// credits per invocation (1 for getTokenSupply + 10 for getProgramAccounts).
	FallbackEnabled bool `yaml:"fallback_enabled"`
	// FallbackTimeoutMs is the context timeout applied to each fallback RPC
	// call. Defaults to 2500 ms. Two sequential calls are issued so the
	// worst-case added latency is 2× this value.
	FallbackTimeoutMs int32 `yaml:"fallback_timeout_ms"`
	// FallbackMaxProgramAccounts caps how many token accounts getProgramAccounts
	// may return for HolderCount computation. Defaults to 200. Higher values
	// improve accuracy but cost more Helius credits; 200 covers virtually all
	// relevant holder concentration cases for fresh pump.fun mints.
	FallbackMaxProgramAccounts int32 `yaml:"fallback_max_program_accounts"`
}

// holderDistResult is the cached outcome of a single probe call.
type holderDistResult struct {
	holderCount int32
	top5Pct     float64
	fetchedAt   time.Time
}

// SolanaHolderDistProbe populates HolderCount, Top5HolderPct and
// HolderDistKnown from getTokenLargestAccounts. The Solana RPC returns
// up to 20 entries — large pump.fun pools typically saturate this.
//
// Results are cached in memory for CacheTTLSec (default 1h). Holder
// distribution changes slowly for new tokens; caching the result avoids
// repeated getAccountInfo + getTokenLargestAccounts calls for every
// rescan band and duplicate ingest event, saving significant Helius credits.
type SolanaHolderDistProbe struct {
	rpc    SolanaProbeRPCClient
	cfg    SolanaHolderDistConfig
	logger *slog.Logger
	// cache maps tokenAddress (string) → holderDistResult.
	cache sync.Map
}

func NewSolanaHolderDistProbe(rpc SolanaProbeRPCClient, cfg SolanaHolderDistConfig, logger *slog.Logger) *SolanaHolderDistProbe {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Commitment == "" {
		cfg.Commitment = "confirmed"
	}
	// Clamp TopK to exactly 5: the DTO field is Top5HolderPct and any
	// other value produces a semantically incorrect result.
	if cfg.TopK != 5 {
		cfg.TopK = 5
	}
	if cfg.CacheTTLSec == 0 {
		cfg.CacheTTLSec = 3600 // default 1h
	}
	if cfg.FallbackTimeoutMs == 0 {
		cfg.FallbackTimeoutMs = 2500 // default 2.5 s
	}
	if cfg.FallbackMaxProgramAccounts == 0 {
		cfg.FallbackMaxProgramAccounts = 200
	}
	return &SolanaHolderDistProbe{rpc: rpc, cfg: cfg, logger: logger}
}

// StartEviction launches a background goroutine that periodically removes
// expired cache entries to prevent unbounded sync.Map growth. Call once
// after construction, passing the application-lifetime context. The
// goroutine stops when ctx is cancelled.
func (p *SolanaHolderDistProbe) StartEviction(ctx context.Context) {
	if p.cfg.CacheTTLSec <= 0 {
		return
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
					if now.Sub(v.(holderDistResult).fetchedAt) >= ttl {
						p.cache.Delete(k)
					}
					return true
				})
			}
		}
	}()
}

func (p *SolanaHolderDistProbe) Name() string { return "solana_holder_dist" }

// Probe sums the top-K holder amounts and divides by total supply when known.
// Skips non-Solana inputs. If total supply is unknown, it falls back to using
// the sum of the returned largest-holder balances as the denominator, so the
// resulting percentage is relative to the returned holder set rather than the
// full mint supply.
//
// Program-ID precheck: getTokenLargestAccounts only works for SPL Token v1
// (TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA). Calling it on a Token-2022
// mint or an LP address returns JSON-RPC error -32602 "Invalid param: not a
// Token mint". We detect this by inspecting the mint account's owner field
// before the main call; if the owner is not SPL Token v1, we return the input
// unchanged with HolderDistKnown=false (not an error — DQ degrades normally).
const splTokenV1Program = "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"

func (p *SolanaHolderDistProbe) Probe(ctx context.Context, in contracts.MarketDataDTO) (contracts.MarketDataDTO, error) {
	if !strings.EqualFold(in.Chain, "solana") {
		return in, nil
	}
	mint := strings.TrimSpace(in.TokenAddress)
	if mint == "" {
		return in, errors.New("probes/solana_holder_dist: empty token address")
	}
	// Reject addresses that are not valid Solana base58 public keys.
	// Fail-open: malformed input should not block the pipeline.
	if !isValidSolanaMint(mint) {
		return in, nil
	}
	if p.rpc == nil {
		return in, errors.New("probes/solana_holder_dist: nil rpc client")
	}

	// Fast path: return cached result if still within TTL.
	if p.cfg.CacheTTLSec > 0 {
		if cached, ok := p.cache.Load(mint); ok {
			r := cached.(holderDistResult)
			ttl := time.Duration(p.cfg.CacheTTLSec) * time.Second
			if time.Since(r.fetchedAt) < ttl {
				out := in
				out.HolderCount = r.holderCount
				out.Top5HolderPct = r.top5Pct
				out.HolderDistKnown = true
				p.logger.Info("solana_holder_dist: cache_hit",
					"mint", mint,
					"age_s", int(time.Since(r.fetchedAt).Seconds()),
				)
				return out, nil
			}
		}
	}

	timeout := time.Duration(p.cfg.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 300 * time.Millisecond
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Program-ID precheck: verify the mint is owned by SPL Token v1 before
	// calling getTokenLargestAccounts. Token-2022 mints and LP addresses are
	// not accepted by that RPC method (error -32602).
	//
	// Fast path: if solana_authorities already ran successfully (SolanaAuthoritiesKnown=true),
	// the mint is confirmed SPL v1 — that probe only succeeds on the SPL v1 mint account
	// layout. Skip the redundant getAccountInfo call (saves 1 Helius credit per event).
	if !in.SolanaAuthoritiesKnown {
		acct, err := p.rpc.GetAccountInfo(cctx, mint, p.cfg.Commitment)
		if err != nil {
			// GetAccountInfo failure is non-fatal here — log and proceed with
			// the holder dist call; if that also fails the error surfaces normally.
			p.logger.Info("solana_holder_dist: account_info_precheck_failed",
				"mint", mint,
				"error", err,
			)
		} else if acct == nil || acct.Owner != splTokenV1Program {
			// Not an SPL v1 mint (Token-2022, LP address, etc.) — skip gracefully.
			p.logger.Info("solana_holder_dist: skipped_non_spl_v1_mint",
				"mint", mint,
				"owner", func() string {
					if acct == nil {
						return "<nil>"
					}
					return acct.Owner
				}(),
			)
			return in, nil
		}
	}

	holders, err := p.rpc.GetTokenLargestAccounts(cctx, mint, p.cfg.Commitment)
	if err != nil {
		errMsg := err.Error()
		if len(errMsg) > 200 {
			errMsg = errMsg[:200]
		}
		p.logger.Info("solana_holder_dist: primary_failed",
			"mint", mint,
			"error", errMsg,
		)
		// Attempt fallback via getTokenSupply + getProgramAccounts if:
		//   1. FallbackEnabled is true (config-driven gate), AND
		//   2. The injected RPC client also satisfies holderDistFallbackClient
		//      (production: *solanaProbeRPCAdapter in cmd/server.go; test stubs
		//      that don't need fallback simply won't satisfy the type assertion).
		if p.cfg.FallbackEnabled {
			if frc, ok := p.rpc.(holderDistFallbackClient); ok {
				return p.fallback(ctx, in, mint, frc)
			}
		}
		return in, fmt.Errorf("probes/solana_holder_dist: get_token_largest_accounts: %s", errMsg)
	}
	if len(holders) == 0 {
		// No holders returned: possible for a freshly minted SPL token.
		// Leave HolderDistKnown=false so DQ profile drives the decision.
		return in, nil
	}

	topK := p.cfg.TopK
	if topK > len(holders) {
		topK = len(holders)
	}
	topSum := new(big.Int)
	for i := 0; i < topK; i++ {
		amt, ok := new(big.Int).SetString(holders[i].Amount, 10)
		if !ok || amt.Sign() < 0 {
			return in, fmt.Errorf("probes/solana_holder_dist: invalid amount %q", holders[i].Amount)
		}
		topSum.Add(topSum, amt)
	}

	out := in
	out.HolderCount = int32(len(holders))

	// Compute pct only when we have a reliable denominator. Prefer the
	// raw token-account decimals (holders[0].Decimals) and the DTO's
	// TotalSupply (already decimal-adjusted for some chains; for SPL it
	// is the raw u64). To stay portable, sum ALL returned holders as
	// the denominator floor when TotalSupply is unknown.
	denom := new(big.Float)
	if in.TotalSupplyKnown && in.TotalSupply > 0 {
		denom.SetFloat64(in.TotalSupply)
	} else {
		// Fall back to "total returned" — biased high (pct biased low),
		// but never produces > 1.0.
		fullSum := new(big.Int)
		for _, h := range holders {
			amt, ok := new(big.Int).SetString(h.Amount, 10)
			if !ok || amt.Sign() < 0 {
				continue
			}
			fullSum.Add(fullSum, amt)
		}
		if fullSum.Sign() == 0 {
			return in, nil
		}
		denom.SetInt(fullSum)
	}

	num := new(big.Float).SetInt(topSum)
	pct := new(big.Float).Quo(num, denom)
	pctF, _ := pct.Float64()
	if pctF < 0 {
		pctF = 0
	}
	if pctF > 1 {
		pctF = 1
	}
	out.Top5HolderPct = pctF
	out.HolderDistKnown = true
	// Store in cache so subsequent rescan events and repeated ingest of the
	// same mint skip the two Helius RPC calls (getAccountInfo + getTokenLargestAccounts).
	if p.cfg.CacheTTLSec > 0 {
		p.cache.Store(mint, holderDistResult{
			holderCount: out.HolderCount,
			top5Pct:     pctF,
			fetchedAt:   time.Now(),
		})
	}
	return out, nil
}

// fallback computes holder distribution via getTokenSupply + getProgramAccounts
// when the primary getTokenLargestAccounts call fails (typically timeout on
// fresh pump.fun mints in their first 60 seconds). Costs +11 Helius credits
// per invocation (1 for getTokenSupply, 10 for getProgramAccounts).
//
// Fail-closed: any RPC error during the fallback leaves HolderDistKnown=false
// so the downstream DQ engine applies the configured unknown-field policy.
func (p *SolanaHolderDistProbe) fallback(ctx context.Context, in contracts.MarketDataDTO, mint string, frc holderDistFallbackClient) (contracts.MarketDataDTO, error) {
	timeout := time.Duration(p.cfg.FallbackTimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 2500 * time.Millisecond
	}
	fctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Step 1: getTokenSupply — 1 Helius credit. Provides the authoritative
	// total-supply denominator for Top5HolderPct. Decimal value is ignored here;
	// the supply field is the raw atomic-unit u64 matching holder amounts.
	supply, _, supplyErr := frc.GetTokenSupply(fctx, mint, p.cfg.Commitment)
	if supplyErr != nil {
		errMsg := supplyErr.Error()
		if len(errMsg) > 200 {
			errMsg = errMsg[:200]
		}
		p.logger.Info("solana_holder_dist: fallback_supply_failed",
			"mint", mint,
			"error", errMsg,
		)
		return in, nil // fail-closed: HolderDistKnown=false
	}

	// Step 2: getProgramAccounts — 10 Helius credits. Filters by:
	//   - dataSize=165: SPL token account layout (initialized, non-multisig)
	//   - memcmp offset=0 bytes=<mint>: first 32 bytes of SPL account = mint field
	maxAccounts := int(p.cfg.FallbackMaxProgramAccounts)
	if maxAccounts <= 0 {
		maxAccounts = 200
	}
	filters := []RPCProgramAccountsFilter{
		{DataSize: 165},
		{Memcmp: &RPCProgramAccountsMemcmp{Offset: 0, Bytes: mint}},
	}
	accounts, err := frc.GetProgramAccounts(fctx, splTokenV1Program, p.cfg.Commitment, filters)
	if err != nil {
		errMsg := err.Error()
		if len(errMsg) > 200 {
			errMsg = errMsg[:200]
		}
		p.logger.Info("solana_holder_dist: fallback_program_accounts_failed",
			"mint", mint,
			"error", errMsg,
		)
		return in, nil // fail-closed: HolderDistKnown=false
	}

	// Collect non-zero balances, capped at maxAccounts to bound credit cost.
	capacity := len(accounts)
	if capacity > maxAccounts {
		capacity = maxAccounts
	}
	balances := make([]uint64, 0, capacity)
	for _, a := range accounts {
		if a.Amount > 0 {
			balances = append(balances, a.Amount)
		}
		if len(balances) >= maxAccounts {
			break
		}
	}
	if len(balances) == 0 {
		return in, nil // no holders — leave HolderDistKnown=false
	}

	// Sort descending for deterministic top-K selection.
	sort.Slice(balances, func(i, j int) bool { return balances[i] > balances[j] })

	topK := p.cfg.TopK
	if topK > len(balances) {
		topK = len(balances)
	}
	var topSum uint64
	for i := 0; i < topK; i++ {
		topSum += balances[i]
	}

	out := in
	out.HolderCount = int32(len(balances))

	// Denominator priority: DTO TotalSupply (exact) → RPC supply → sum of
	// returned balances (biased low, never > 1.0).
	var denom float64
	if in.TotalSupplyKnown && in.TotalSupply > 0 {
		denom = in.TotalSupply
	} else if supply > 0 {
		denom = float64(supply)
	} else {
		var fullSum uint64
		for _, b := range balances {
			fullSum += b
		}
		denom = float64(fullSum)
	}

	var pctF float64
	if denom > 0 {
		pctF = float64(topSum) / denom
		if pctF < 0 {
			pctF = 0
		}
		if pctF > 1 {
			pctF = 1
		}
	}
	out.Top5HolderPct = pctF
	out.HolderDistKnown = true

	p.logger.Info("solana_holder_dist: fallback_success",
		"mint", mint,
		"holder_count", out.HolderCount,
		"top5_pct", pctF,
		"source", "fallback",
	)
	if p.cfg.CacheTTLSec > 0 {
		p.cache.Store(mint, holderDistResult{
			holderCount: out.HolderCount,
			top5Pct:     pctF,
			fetchedAt:   time.Now(),
		})
	}
	return out, nil
}
