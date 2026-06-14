---
name: price-feed-integration
type: skill
description: >
  Live price feed integration for position monitoring (Layer 9). Use when implementing
  or reviewing the `PriceClient` interface, per-chain price fetchers (EVM `getAmountsOut`,
  Solana AMM-pool `getAccountInfo`), and the chain-resolving factory wired into
  `RunPositionPoll`. Without this, every TP/SL/Trail decision in `position.go` is dead
  code. Closes GAP-02 from `docs/analysis/profitability-gaps.md`.
---

# Price Feed Integration Skill

## Purpose

Provide the live price signal that drives TP/SL/Trail exit decisions in the Position
Engine (Layer 9). The position module is correctly implemented; its `PriceClient`
dependency is the single missing wire that converts a paper-trading system into a
real-PnL system.

**Core invariant:** Without a live price feed, the Position Engine factor in
`Profit = Edge × Probability × Execution × Capital × DataQuality × AdaptationQuality`
collapses to ~0 — exits only fire on `max_hold_seconds` time expiry, missing every pump
and accumulating every drawdown.

**Phase 9 target chains:** ETH (Uniswap v2), BSC (PancakeSwap v2), Solana (Raydium /
PumpFun curve).

---

## Rules

### 1. PriceClient Interface — Single Contract

There is **one** interface for every chain. Per-chain implementations differ; the
interface does not.

```go
// internal/modules/position/price_client.go (existing)
type PriceClient interface {
    GetPrice(ctx context.Context, chain string, token string) (priceUsd float64, err error)
}
```

- `chain` — `"eth" | "bsc" | "solana"` (matches `MarketDataDTO.Chain`)
- `token` — token contract address (EVM) or token mint pubkey (Solana)
- Returns USD price as `float64`; never returns negative; returns error on RPC failure or stale native price

### 2. Per-Chain Implementation Map

| Chain  | File (`internal/rpc/`)          | RPC method                                                            | Price formula                              |
| ------ | ------------------------------- | --------------------------------------------------------------------- | ------------------------------------------ |
| ETH    | `evm_reserve_price.go`          | `eth_call` → `getAmountsOut(uniRouter, 1e18, [token, WETH])`          | `amounts[1] / 1e18 × ethPriceUsd`          |
| BSC    | `evm_reserve_price.go` (shared) | `eth_call` → `getAmountsOut(pancakeRouter, 1e18, [token, WBNB])`      | `amounts[1] / 1e18 × bnbPriceUsd`          |
| Solana | `solana_reserve_price.go`       | `getAccountInfo(poolAddress)` → decode AMM layout (Raydium / PumpFun) | `reserveQuote / reserveBase × solPriceUsd` |

### 3. Factory Pattern — Chain Resolution at Boot

```go
// internal/rpc/price_oracle_factory.go
func NewPriceClientForChain(
    chain string,
    cfg AppConfig,
    solClient *solana.Client,
    evmClient *ethclient.Client,
) (position.PriceClient, error) {
    switch chain {
    case "eth", "bsc":
        return NewEVMReservePriceFetcher(evmClient, cfg.Chains[chain].Router, ...), nil
    case "solana":
        return NewSolanaReservePriceFetcher(solClient, ...), nil
    default:
        return nil, fmt.Errorf("price client: unsupported chain %q", chain)
    }
}
```

**Wiring rule:** `cmd/server.go` MUST call the factory and assert `priceClient != nil`
before passing it to `workers.RunPositionPoll`. **Never** pass `nil`.

### 4. Native Token Price Source

EVM chains require ETH/USD or BNB/USD to convert reserve ratios to USD. Solana requires
SOL/USD. **Cache aggressively:**

- TTL: `native_price_ttl_sec` (default 60 s)
- Max stale: `native_price_max_stale_sec` (default 300 s)
- Beyond max-stale: halt new TP/SL evaluations until refresh; emit `system_event level=warn`

Source: any reliable on-chain oracle — Chainlink price feed via `eth_call` is the
canonical choice for EVM. Solana: pyth oracle via `getAccountInfo`.

### 5. Failure Handling Contract

| Failure                                          | Behavior                                                                      |
| ------------------------------------------------ | ----------------------------------------------------------------------------- |
| RPC timeout per price fetch                      | Skip this poll cycle for the position; do NOT fire TP/SL                      |
| `consecutive_failures ≥ price_failure_threshold` | Emit `position_event level=warn`, `Reason=price_feed_unavailable`             |
| Native-token price stale                         | Use last-known up to `native_price_max_stale_sec`; beyond → halt evaluations  |
| Pool drained between polls (reserve = 0)         | Emergency `IsRug=true` exit signal → fire SL with `Reason=pool_drained`       |
| Decode error on Solana pool layout               | Increment `position_decode_errors_total`; treat as Indeterminate; do NOT fire |

**Never** return a fabricated price on error. Either return a real price or return an
error — Position Engine handles the error by skipping the cycle.

### 6. Hot-Path Performance

- Per-fetch context timeout: `price_fetch_timeout_ms` (default 500 ms)
- Per-cycle wall budget: `max_open_positions × price_fetch_timeout_ms × 1.2`
- Exceeded budget → emit `system_event level=warn` and reduce poll concurrency; do NOT block

### 7. No DTO Changes

Phase 9 introduces zero DTO changes for price feed. `PositionStateDTO.LastObservedPrice`
already exists — Phase 9 simply populates it with real values rather than leaving it at
zero. Per `copilot-instructions.md` § Architecture Invariants, no new DTO fields,
no schema changes, no adapter modifications.

### 8. Module Boundaries

- `internal/modules/position/` MUST NOT import `internal/rpc/` directly. The `PriceClient`
  is injected into the position module via the worker constructor.
- The factory lives in `internal/rpc/`, **not** in `internal/modules/position/`.
- Native-token USD price fetching is owned by `internal/rpc/` — the position module sees
  only the final USD value.

---

## Inputs

- `chain` (`"eth"` | `"bsc"` | `"solana"`) — from `PositionStateDTO.Chain`
- `token` (contract address / mint pubkey) — from `PositionStateDTO.TokenAddress`
- RPC clients injected at boot (`*ethclient.Client`, `*solana.Client`)
- Native-token USD price oracle (Chainlink / Pyth) — cached
- Config: `shared/config/pipeline.yaml position.*` (timeouts, thresholds, poll cadence)

## Outputs

- `priceUsd float64` — strictly positive, denominated in USD
- Error on: RPC timeout, stale native price beyond max-stale, decode failure, pool drained
- Side effects: cache writes (native price), metric increments, structured logs

---

## Examples

### Wiring at boot (cmd/server.go)

```go
priceClient, err := rpc.NewPriceClientForChain(activeChain, cfg, solanaClient, evmClient)
if err != nil {
    return fmt.Errorf("price feed init failed for chain %s: %w", activeChain, err)
}
if priceClient == nil {
    return errors.New("price feed factory returned nil — refusing to start position poll")
}
workers.RunPositionPoll(ctx, db, cfg, priceClient, logger)
```

### EVM implementation skeleton

```go
func (f *EVMReservePriceFetcher) GetPrice(
    ctx context.Context, chain, token string,
) (float64, error) {
    ctx, cancel := context.WithTimeout(ctx, f.cfg.PriceFetchTimeout)
    defer cancel()

    amounts, err := f.callGetAmountsOut(ctx, oneEther, []common.Address{
        common.HexToAddress(token),
        f.wethAddress,
    })
    if err != nil {
        return 0, fmt.Errorf("getAmountsOut: %w", err)
    }
    if len(amounts) < 2 || amounts[1].Sign() <= 0 {
        return 0, errors.New("getAmountsOut: invalid output amount")
    }
    nativeUsd, err := f.nativePriceCache.Get(ctx, chain)
    if err != nil {
        return 0, fmt.Errorf("native price: %w", err)
    }
    tokenInWeth := new(big.Float).Quo(
        new(big.Float).SetInt(amounts[1]),
        big.NewFloat(1e18),
    )
    priceUsd, _ := new(big.Float).Mul(tokenInWeth, big.NewFloat(nativeUsd)).Float64()
    return priceUsd, nil
}
```

### Solana implementation skeleton

```go
func (f *SolanaReservePriceFetcher) GetPrice(
    ctx context.Context, chain, token string,
) (float64, error) {
    ctx, cancel := context.WithTimeout(ctx, f.cfg.PriceFetchTimeout)
    defer cancel()

    poolPubkey := f.poolForToken(token)
    info, err := f.client.GetAccountInfo(ctx, poolPubkey)
    if err != nil {
        return 0, fmt.Errorf("getAccountInfo: %w", err)
    }
    reserves, err := decodeAMMReserves(info.Value.Data.GetBinary())
    if err != nil {
        return 0, fmt.Errorf("decode reserves: %w", err)
    }
    if reserves.Base == 0 {
        return 0, ErrPoolDrained
    }
    solUsd, err := f.nativePriceCache.Get(ctx, "solana")
    if err != nil {
        return 0, fmt.Errorf("native price: %w", err)
    }
    return float64(reserves.Quote) / float64(reserves.Base) * solUsd, nil
}
```

---

## Checklist

- [ ] `PriceClient` interface lives in `internal/modules/position/`; implementations in `internal/rpc/`
- [ ] Factory function `NewPriceClientForChain` resolves all three target chains (eth/bsc/solana)
- [ ] `cmd/server.go` calls factory and asserts `priceClient != nil` before `RunPositionPoll`
- [ ] No `nil` `PriceClient` reaches `RunPositionPoll` in any code path
- [ ] Per-fetch context timeout enforced (`price_fetch_timeout_ms`)
- [ ] Native-token USD price cached with TTL; max-stale handling implemented
- [ ] Pool drained → emergency SL signal with `Reason=pool_drained`
- [ ] Solana decode errors counted in `position_decode_errors_total`
- [ ] No fabricated prices returned on error — error or real price only
- [ ] `internal/modules/position/` does NOT import `internal/rpc/` directly
- [ ] Tests cover: happy path (eth/bsc/solana), RPC timeout, native price stale, pool drained, decode error

---

## Anti-Patterns

- ❌ Returning `0.0` on RPC error — collapses to false SL trigger
- ❌ Hardcoded native-token price (e.g. `ethPriceUsd := 3000`)
- ❌ Importing `internal/rpc/` directly from `internal/modules/position/`
- ❌ Passing `nil` PriceClient to `RunPositionPoll` "to disable for testing" — use a stub implementation instead
- ❌ Computing USD price in the position module — the module sees only `priceUsd` from the client
- ❌ Skipping the factory and instantiating `EVMReservePriceFetcher` directly in `cmd/server.go`

---

## Cross-References

- `docs/analysis/profitability-gaps.md` § GAP-02 (canonical gap definition)
- `docs/reference/implementation_roadmap.md` § 9.5 (Phase 9 implementation contract)
- `docs/reference/architecture.md` § 3.9 (Position Engine layer spec)
- `.github/skills/position-management/SKILL.md` (consumer of this price feed)
- `.github/skills/rpc-management/SKILL.md` (RPC pool / circuit breaker patterns)
- `.github/skills/monitoring-loop-engine/SKILL.md` (poll loop that consumes prices)
