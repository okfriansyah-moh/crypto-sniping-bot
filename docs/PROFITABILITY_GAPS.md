# Profitability Gap Analysis — Cross-Chain Audit

> **Status:** Verified against codebase on 2026-04-29.
> **Canonical invariant:** `Profit = Edge × Probability × Execution × Capital × DataQuality × AdaptationQuality`
> Any factor approaching zero kills profit regardless of how well the other factors perform.

---

## Current State Estimate

| Factor       | Current State                                    | Estimated Multiplier |
| ------------ | ------------------------------------------------ | -------------------- |
| DataQuality  | 4 static checks; all scam detectors `false`      | ~0.10                |
| Features     | 5/8 signals hardcoded `0.5`                      | ~0.30                |
| Edge         | Correct logic, depends on feature quality        | ~0.60                |
| Probability  | Fixed prior `P=0.35`, model output ignored       | ~0.40                |
| Execution    | Real on EVM; Solana real but single market       | ~0.75                |
| Capital      | Fixed `$50` regardless of edge strength          | ~0.40                |
| Adaptation   | Learning code correct; inputs are stub features  | ~0.20                |
| **Combined** | `0.10 × 0.30 × 0.60 × 0.40 × 0.75 × 0.40 × 0.20` | **~0.1%**            |

---

## Pipeline Reference

```
[INGEST]→[DQ]→[FEATURES]→[EDGE]→[P/S/L MODELS]→[VALIDATE]→[SELECT]→[CAPITAL]→[EXECUTE]→[POSITION/EXIT]→[EVALUATE]→[LEARN]
Layer 0    L1    L2         L3       L4              L5         L6        L7        L8          L9              L10
```

---

## TIER 0 — Safety Multiplier: Zero Without These

These gaps make the bot unconditionally unprofitable. Fix before anything else.

---

### GAP-01 · DataQuality: All Scam Detectors Hardcoded Off

**Layer:** 1 · **Chains:** ALL (ETH, BSC, Solana) · **Priority:** P0

**Code location:** `internal/modules/data_quality/data_quality.go` lines 61–70

```go
// Phase 2: static heuristic checks — no RPC calls (deferred to Phase 3 with retry).
isHoneypot       := false  // bot WILL buy honeypots
isFakeLiquidity  := false  // only set by reserve threshold check below
isWashTrading    := false  // never detected
isRugRisk        := false  // never detected
isTaxAnomaly     := false  // never detected
lpLocked         := false  // never checked
contractVerified := false  // never checked
```

Only 4 real checks run: missing reserves, reserve below minimum, reorged event, missing token address. Everything else passes with `RiskScore = 0.0`.

**Impact:** The `ContractSafety` feature in Layer 2 is derived directly from DQ flags. With all flags false, `contractSafety = 1.0` for every token including rugs. The probability model's highest weight (`WContractSafety=1.4`) feeds on a constant lie.

**Required implementation per chain:**

| Detector          | ETH / BSC                                                                                   | Solana                                                                        |
| ----------------- | ------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| Honeypot          | `eth_call` simulate buy→sell round trip; compare `received/sent` ratio                      | `simulateTransaction` with buy+sell bundle; check log output                  |
| Tax anomaly       | Decode `Transfer` logs in simulated tx; compute `amountReceived / amountSent`               | Same via simulated transaction log analysis                                   |
| LP locked         | `balanceOf(lockContract, lpToken)` — check Unicrypt / Team Finance / PinkLock addresses     | Check LP NFT ownership (Raydium) or PumpFun bonding curve `complete` field    |
| Wash trading      | Count unique `from` addresses across last 20 `Swap` events via `eth_getLogs`                | Count unique signers via `GetSignaturesForAddress`                            |
| Rug risk          | Decode ABI selectors; flag if `mint()`/`pause()`/`blacklist()` exist in unverified contract | Check mint authority not renounced via `getAccountInfo` on token mint account |
| Contract verified | Query Etherscan / BscScan v2 API `/api?module=contract&action=getsourcecode`                | n/a — Solana programs are bytecode; check program upgrade authority instead   |

**Config addition needed in `config/chains.yaml`:**

```yaml
data_quality:
  honeypot_threshold: 0.30 # max acceptable received/sent ratio deviation
  tax_anomaly_max_bps: 1000 # 10% total tax ceiling
  wash_unique_ratio_min: 0.30 # min unique-senders ratio
  lp_lock_required: true
  min_lp_lock_days: 30
```

---

### GAP-02 · Position: `priceClient` is `nil` in Production

**Layer:** 9 · **Chains:** ALL · **Priority:** P0

**Code location:** `cmd/server.go` line 121

```go
workers.RunPositionPoll(ctx, db, cfg, nil, logger)  // nil = no price polling
```

`RunPositionPoll` skips price fetching when `priceClient == nil` (guarded in `run_position_poll.go` line 85). All TP1/TP2/SL logic in `position.go` is mathematically correct but never executes. Every position exits only when `max_hold_seconds` expires, regardless of price. The bot:

- Cannot capture take-profit on 5× pumps
- Cannot stop-loss on rugs before they go to zero
- Holds every position to the time limit regardless of outcome

**Interface already defined** — `position.PriceClient` exists in `internal/modules/position/position.go`. No contract changes needed.

**Required implementations:**

| Chain  | Implementation                                         | RPC Method                                                                                       |
| ------ | ------------------------------------------------------ | ------------------------------------------------------------------------------------------------ |
| ETH    | `EVMReservePriceFetcher`                               | `getAmountsOut(router, 1e18, [token→WETH])` → price in WETH                                      |
| BSC    | `EVMReservePriceFetcher` (same code, different router) | same pattern, PancakeSwap router                                                                 |
| Solana | `SolanaReservePriceFetcher`                            | `getAccountInfo(poolAddress)` → decode AMM reserve layout → `price = reserveBase / reserveToken` |

**File to create:** `internal/rpc/price_fetcher.go` implementing `position.PriceClient`

**Wire in `cmd/server.go`:**

```go
priceClient := rpc.NewPriceClientForChain(activeChain, cfg, solanaRPCClient, evmClient)
workers.RunPositionPoll(ctx, db, cfg, priceClient, logger)
```

---

## TIER 1 — Signal Quality: ~30% of Maximum

These gaps ensure every decision is based on fabricated data. Fix after Tier 0.

---

### GAP-03 · Features: 5 of 8 Signals Hardcoded at `0.5`

**Layer:** 2 · **Chains:** ALL · **Priority:** P1

**Code location:** `internal/modules/features/features.go`

```go
txVelocityScore := 0.5   // Phase 2 stub — "moderate positive signal"
walletEntropy   := 0.5   // Phase 2 stub
tokenAge        := 0.0   // stub — all new tokens score 0
volumeMomentum  := 0.5   // Phase 2 stub
priceMomentum   := 0.5   // Phase 2 stub
```

The probability model in `internal/modules/models/probability.go` applies these weights:

```go
WTxVelocityScore:    0.9   // highest weight — feeds a stub
WVolumeMomentum:     0.8   // second highest — feeds a stub
WContractSafety:     1.4   // only real input after GAP-01 fixed
WLiquidityScore:     1.6   // highest — computed from DQ risk (itself flawed)
```

`LiquidityUsdRaw` is also always `0` — the slippage model (Layer 4) always falls into the worst bucket (`P95Bps=800`) because it never receives real liquidity depth.

**Required real signals per chain:**

| Feature           | ETH / BSC source                                                        | Solana source                                                      |
| ----------------- | ----------------------------------------------------------------------- | ------------------------------------------------------------------ |
| `TxVelocityScore` | Count `Swap` log topics for pool address in last 30s via `eth_getLogs`  | `GetSignaturesForAddress(poolAddress, limit=20)` count in last 30s |
| `WalletEntropy`   | Count unique `from` addresses in last 50 `Swap` events                  | Count unique signers from `GetSignaturesForAddress`                |
| `TokenAge`        | `block.timestamp` at pool creation − `in.BlockTimestamp`                | PumpFun create timestamp already in `MarketDataDTO.IngestedAt`     |
| `VolumeMomentum`  | Compare 30s swap volume vs 5min rolling avg from Sync events            | Reserve delta from two consecutive `getAccountInfo` calls          |
| `PriceMomentum`   | Reserve ratio delta between consecutive Sync events on the pool         | Same via AMM reserve account polling                               |
| `LiquidityUsdRaw` | `reserveBase_decimal × ethPriceUsd` from `MarketDataDTO.ReserveBaseRaw` | `reserveBase × solPriceUsd` from AMM pool account                  |

**Confidence values must be updated** from `0.3` stubs to `0.7–0.9` once real data flows in.

---

### GAP-04 · Probability Layer: Model Output Not Wired Into EV Gate

**Layer:** 5 (Validation) · **Chains:** ALL · **Priority:** P1

**Code location:** `internal/modules/validation/validation.go`

```go
// Phase 2: fixed probability priors, no real model (deferred to Phase 4).
p := m.cfg.PriorProbability  // = 0.35, always — ignores ProbabilityEstimateDTO entirely
```

`ProbabilityModel.Predict()` in `internal/modules/models/probability.go` is fully implemented and mathematically correct (logistic regression with calibration). Its output is never used. The EV gate always evaluates:

```
EV = 0.35 × 3000 − 0.65 × 4000 − 150 − 200 = 100 bps (fixed)
```

**Required change:**

- `validation.Process()` must accept `contracts.ProbabilityEstimateDTO` as an additional parameter
- Replace `p := m.cfg.PriorProbability` with `p := probDTO.Probability`
- Orchestrator/worker must call `models.ProbabilityModel.Predict(featureDTO)` and thread the result through to validation
- This immediately makes the EV gate dynamic — high-signal tokens pass, marginal tokens reject

---

### GAP-05 · Capital: Fixed `$50` Entry Size, No Edge-Proportional Sizing

**Layer:** 7 · **Chains:** ALL · **Priority:** P1

**Code location:** `internal/modules/capital/capital.go`

```go
// Phase 2: fixed base allocation; Phase 7 adds Kelly-adjacent sizing.
sizeUsd := m.cfg.FixedEntrySizeUsd  // always $50, ignores edge score, probability, confidence
```

A 10× edge signal gets identical sizing to a 1.1× noise signal. This eliminates the `Capital` multiplier from the profit formula entirely.

**Required implementation (Kelly-adjacent):**

```
f_kelly = (P × R − (1−P)) / R
  where R = PriorGainBps / PriorLossBps

sizeUsd = base_size × f_kelly × cohort_multiplier × mode_multiplier
  clamp(sizeUsd, min_size_usd, max_size_usd)
```

**Mode multipliers** (per `config/pipeline.yaml` operational_modes section):

- `STRICT` → 0.5×
- `BALANCED` → 1.0×
- `EXPLORATION` → 1.3×

**Inputs already available:** `SelectionOutputDTO.CombinedScore`, `AllocationDTO.VersionID`, `LearningRecord.CohortID`.

---

## TIER 2 — Execution Quality: Profit Leak on Every Trade

---

### GAP-06 · EVM Execution: Legacy Gas Only, No EIP-1559

**Layer:** 8 · **Chains:** ETH (critical), BSC (not applicable) · **Priority:** P2

**Code location:** `internal/modules/execution/execution.go`

```go
// Phase 2: single wallet, no sharding, no replacement loop, public mempool only.
gasPrice, err := m.client.GetGasPrice(ctx)  // Type-0 legacy transaction only
```

ETH mainnet validators deprioritize legacy Type-0 transactions vs EIP-1559 Type-2. Every competing bot using `maxFeePerGas + maxPriorityFeePerGas` gets preferential block inclusion. Legacy txs pay higher effective gas cost and get included later — by which time the opportunity window is closed.

**Required change:**

- Add `GetMaxFeePerGas(ctx) (baseFee *big.Int, tip *big.Int, err error)` to `EVMClient` interface
- Build `geth_core.DynamicFeeTx` (Type 2) instead of `LegacyTx` for ETH
- Config additions in `config/gas.yaml`:
  ```yaml
  eth:
    max_priority_fee_gwei: 2.0 # tip cap for validators
    max_fee_multiplier: 1.5 # baseFee × multiplier = maxFeePerGas ceiling
  ```
- BSC: keep legacy `LegacyTx` — BSC does not support EIP-1559

---

### GAP-07 · EVM Execution: Hardcoded ETH Price `$3500`

**Layer:** 8 · **Chains:** ETH, BSC · **Priority:** P2

**Code location:** `internal/modules/execution/execution.go`

```go
ethPriceUsd := float64(3500)  // hardcoded; actual ETH price ignored
```

This is used to convert `AllocationDTO.SizeUsd` to wei. At ETH=$2000 the bot over-allocates by 75%. At ETH=$5000 it under-allocates by 30%. Position sizes are systematically wrong.

`ExecutionConfig` struct already has an `EthPriceUsd` field — it just needs to be populated.

**Required implementation:**

- At startup and with 60s TTL: call `getAmountsOut(uniV2Router, 1e18, [WETH→USDC])` to get live ETH price
- Update `execCfg.EthPriceUsd` from oracle before allocation conversion
- Fall back to last known price on RPC error, never to the hardcoded `3500`
- BSC equivalent: `getAmountsOut(pancakeRouter, 1e18, [WBNB→BUSD])`

---

### GAP-08 · EVM Execution: Private Mempool Stub (Flashbots/Beaverbuild)

**Layer:** 8 · **Chains:** ETH · **Priority:** P2

**Code location:** `internal/modules/execution/private_rpc.go`

```go
// Phase 4 stub — actual relay wiring implemented once private endpoints are live.
func (r *PrivateRPCRouter) Route(sizeUsd float64) bool {
    if len(r.endpoints) == 0 { return false }  // always false — no endpoints configured
```

`PickRoute()` in `mev.go` correctly determines "flashbots" vs "public" based on size threshold and latency. The routing decision is correct — the actual relay submission is not wired. New ETH launches are front-run by sandwich bots watching the public mempool.

**Required implementation:**

- Add Flashbots relay endpoint to `config/execution.yaml` `private_endpoints`
- Implement `eth_sendBundle` POST format for `https://relay.flashbots.net`
- `sendBundle` requires signing with `X-Flashbots-Signature` header (separate flashbots signing key)
- Route decision already handled by `PickRoute()` — only need the HTTP relay call in the execution worker

---

### GAP-09 · EVM Execution: Wallet Sharding Requires Env Var Setup

**Layer:** 8 · **Chains:** ETH, BSC · **Priority:** P2 (operational)

**Code location:** `cmd/server.go` lines 307–340

Wallet sharding IS implemented correctly via `buildWalletShards()`. However it silently falls back to a single wallet when `SNIPER_WALLET_N_ADDRESS` / `SNIPER_WALLET_N_KEY` env vars are not set:

```go
// Fall back to single wallet from config when no env vars are provided.
if len(shards) == 0 && cfg.Capital.WalletAddress != "" {
    shards = []execution.WalletConfig{{Address: ..., PrivateKey: ...}}
}
```

Single wallet = one in-flight tx at a time = throughput cap of 1 tx per confirmation cycle.

**Required setup:**

```bash
export SNIPER_WALLET_0_ADDRESS="0x..."
export SNIPER_WALLET_0_KEY="abc..."
export SNIPER_WALLET_1_ADDRESS="0x..."
export SNIPER_WALLET_1_KEY="def..."
```

Recommended 3–5 wallets for ETH. Code requires no changes — only operational configuration.

---

### GAP-10 · Slippage Model: Always Uses Worst Liquidity Bucket

**Layer:** 4 · **Chains:** ALL · **Priority:** P2 (blocked by GAP-03)

**Code location:** `internal/modules/models/slippage.go`

`SlippageModel.Estimate()` uses `feature.LiquidityUsdRaw` to select a bucket. `LiquidityUsdRaw` is always `0` in `FeatureDTO` because the feature module never computes it from real reserves.

**Effect on EV gate:** At `LiquidityUsdRaw=0` + `ProposedSize=$50`, the slippage model selects bucket `{LiquidityMaxUsd: 25_000, SizeMaxUsd: 50, P50Bps: 250, P95Bps: 800}`. The EV gate uses P95=800 bps slippage → rejects many valid trades, OR underestimates slippage for illiquid pools → allows bad trades.

**Fix:** Blocked by GAP-03. Once `LiquidityUsdRaw = reserveBase × chainTokenPriceUsd` flows through the feature vector, slippage bucketing self-corrects with no code change.

---

## TIER 3 — Coverage Gaps: Volume, Not Quality

Fix these after Tier 0–2 to expand opportunity set.

---

### GAP-11 · Solana Coverage: Only 2 of 6 Active Programs

**Layer:** 0 · **Chain:** Solana · **Priority:** P3

**Code location:** `config/chains.yaml` solana.programs

Currently configured:

```yaml
programs:
  - program_id: "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8" # Raydium AMM V4
    family: raydium-v4
  - program_id: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P" # PumpFun bonding curve
    family: pumpfun
```

Missing:

| Program                      | Program ID                                     | Gap Impact                                                                                                                                           |
| ---------------------------- | ---------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| **PumpFun AMM** (graduation) | `pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA`  | **Critical** — tokens that survive the bonding curve are the highest-quality targets (proven demand), but the bot is blind to their graduation event |
| Raydium CLMM                 | `CAMMCzo5YL8w4VFF8KVHrK22GGUsp5VTaW7grrKgrWqK` | High TVL concentrated liquidity pools; tighter spreads                                                                                               |
| Orca Whirlpool               | `whirLbMiicVdio4qvUfM5KAg6Ct8VwpYzGff3uctyCc`  | Second-largest Solana DEX by volume                                                                                                                  |
| Meteora DLMM                 | `LBUZKhRxPF3XUpBCjp4YzTKgLLjLeNox4HgSehp9ZSe`  | Dynamic bin-based AMM; popular for new launches                                                                                                      |

**Implementation pattern** — ingestion module is config-driven and program-agnostic. Adding coverage = add decoder + add program entry to `config/chains.yaml`:

1. Create `internal/modules/ingestion_solana/{program_name}.go` with discriminator + decoder
2. Register in `ingestion_solana.go` `normalizeNotification()` dispatch switch
3. Add to `config/chains.yaml` programs list

---

### GAP-12 · ETH Coverage: Missing DEX Factories

**Layer:** 0 · **Chain:** ETH · **Priority:** P3

**Code location:** `config/chains.yaml` eth.factories

Currently configured: UniswapV2 + UniswapV3.

Missing:

| Protocol     | Factory Address                              | Notes                               |
| ------------ | -------------------------------------------- | ----------------------------------- |
| SushiSwap V2 | `0xC0AEe478e3658e2610c5F7A4A2E1777cE9e4f2Ac` | Significant new token launch volume |
| SushiSwap V3 | `0xbACEB8eC6b9355Dfc0269C18bac9d6E2Bdc29C4F` | Concentrated liquidity              |

**Implementation:** EVM ingestion is factory-agnostic (subscribes to `PairCreated`/`PoolCreated` events). Add factory addresses to `config/chains.yaml` — no code changes required for V2. V3 requires the `PoolCreated` log signature decoder already present for UniV3.

---

### GAP-13 · BSC Coverage: PancakeSwap V3 Missing

**Layer:** 0 · **Chain:** BSC · **Priority:** P3

**Code location:** `config/chains.yaml` bsc.factories

Only PancakeSwap V2 (`0xcA143Ce32Fe78f1f7019d7d551a6402fC5350c73`) configured.

PancakeSwap V3 factory: `0x0BFbCF9fa4f9C56B0F40a671Ad40E0805A091865`

V3 emits `PoolCreated(address indexed token0, address indexed token1, uint24 indexed fee, int24 tickSpacing, address pool)` — same signature as UniV3. Decoder is already present in the EVM ingestion normalize pipeline. Add factory address to config.

---

## TIER 4 — Adaptation Quality: Compounding Over Time

These gaps prevent the system from improving. Address after Tier 1–2 are complete.

---

### GAP-14 · Learning Engine: Training on Stub Feature Signals

**Layer:** 10 · **Chains:** ALL · **Priority:** P4 (blocked by GAP-03)

**Code location:** `internal/modules/learning/`

The learning stack (`updater.go`, `ab_promoter.go`, `shadow_recorder.go`, `evaluator.go`) is correctly implemented. The A/B promotion gate (`ShouldPromote`) and rollback logic are sound.

The problem: every `LearningRecord` is labeled with a `FeatureDTO` where 5/8 signals are `0.5`. Cohort analysis groups by `CohortID` which is derived from these signals. The system learns to optimize for `[0.5, 0.5, 0.5, 0.5, 0.5]` — a meaningless centroid.

**Actions once GAP-03 is resolved:**

- Raise `MinSamplesForUpdate` to `N ≥ 50` (from whatever default is in `config/pipeline.yaml`)
- Enable per-cohort multiplier updates in `LearningConfig`
- Verify `ShadowRecorder.RecordRejection()` is wired in the worker for false-negative tracking (shadow trade path)

---

### GAP-15 · Selection: No Ranking, Exploration Band Always Disabled

**Layer:** 6 · **Chains:** ALL · **Priority:** P4

**Code location:** `internal/modules/selection/selection.go` lines 70–71

```go
DiversityBucket: "default",   // single bucket — no diversity
IsExploration:   false,        // hardcoded — EXPLORATION mode never fires
```

`max_open_positions = 1` (configured in `config/pipeline.yaml`) means ranking is moot at single-position scale. But exploration band = 0 means the system never deliberately tests lower-confidence candidates to measure false-negative rate. Without exploration data, the learning engine cannot tune thresholds beyond the training distribution.

**Required implementation:**

- Increase `max_open_positions` to 3–5 for BALANCED mode
- Implement `IsExploration = true` for 1–3% of budget when `OperationalMode == EXPLORATION`
- Top-K ranking: sort candidates by `CombinedScore desc`, take top K respecting diversity buckets

---

### GAP-16 · No Per-Chain DQ Thresholds

**Layer:** 1 · **Chains:** ALL · **Priority:** P4

**Code location:** `internal/modules/data_quality/data_quality.go` `DefaultConfig()`

Single universal threshold set for all chains. Chain-specific rug/honeypot dynamics differ materially:

| Chain  | Characteristic                                                        | Default Is Wrong Because                                   |
| ------ | --------------------------------------------------------------------- | ---------------------------------------------------------- |
| ETH    | Tax anomalies 2–10%, rug timeline hours                               | Single threshold too loose for fast BSC dynamics           |
| BSC    | Tax anomalies 10–30% common, rug timeline minutes                     | Current `MaxBuyTaxBps=1000` (10%) rejects valid BSC tokens |
| Solana | No concept of transfer tax; rug pattern is LP drain or mint authority | Tax checks are irrelevant, different signals needed        |

**Required addition to `config/chains.yaml`:**

```yaml
eth:
  data_quality:
    max_buy_tax_bps: 500
    max_sell_tax_bps: 800
    wash_unique_ratio_min: 0.35
    min_lp_lock_days: 7

bsc:
  data_quality:
    max_buy_tax_bps: 1500
    max_sell_tax_bps: 2000
    wash_unique_ratio_min: 0.25
    min_lp_lock_days: 3

solana:
  data_quality:
    check_mint_authority: true
    check_freeze_authority: true
    min_bonding_curve_progress: 0.05
```

---

## TIER 5 — Multi-Chain Architecture Gaps

These gaps block future chain expansion.

---

### GAP-17 · No Chain-Agnostic Price Oracle Factory

**Layer:** infra · **Chains:** Future (Arbitrum, Base, Avalanche) · **Priority:** P5

`position.PriceClient` interface is correctly defined. There is no factory to construct the right implementation based on chain ID. Adding a new chain requires manual wiring in `cmd/server.go`.

**Required implementation:**

```go
// internal/rpc/price_oracle.go
func NewPriceClientForChain(chain string, ...) position.PriceClient {
    switch chain {
    case "eth", "bsc":
        return NewEVMReservePriceFetcher(...)
    case "solana":
        return NewSolanaReservePriceFetcher(...)
    default:
        return nil
    }
}
```

---

## Gap Summary Table

| ID     | Description                         | Layer | Chains   | Factor Affected     | Priority |
| ------ | ----------------------------------- | ----- | -------- | ------------------- | -------- |
| GAP-01 | All DQ scam detectors `false`       | L1    | ALL      | DataQuality ×0.1    | **P0**   |
| GAP-02 | `priceClient = nil`, all TP/SL dead | L9    | ALL      | Execution ×0        | **P0**   |
| GAP-03 | 5/8 features hardcoded `0.5`        | L2    | ALL      | Features ×0.3       | **P1**   |
| GAP-04 | Fixed prior P=0.35, model ignored   | L5    | ALL      | Probability static  | **P1**   |
| GAP-05 | Fixed $50 entry, no Kelly sizing    | L7    | ALL      | Capital not scaling | **P1**   |
| GAP-06 | Legacy gas only, no EIP-1559        | L8    | ETH      | Execution slower    | P2       |
| GAP-07 | Hardcoded ETH price `$3500`         | L8    | ETH, BSC | Capital wrong size  | P2       |
| GAP-08 | Private mempool stub (Flashbots)    | L8    | ETH      | Front-run exposure  | P2       |
| GAP-09 | EVM wallet sharding needs env vars  | L8    | ETH, BSC | Throughput cap      | P2 (ops) |
| GAP-10 | Slippage bucket uses liquidity=0    | L4    | ALL      | Wrong EV gate       | P2       |
| GAP-11 | Solana: only 2/6 programs covered   | L0    | Solana   | Volume coverage     | P3       |
| GAP-12 | ETH: missing SushiSwap factories    | L0    | ETH      | Volume coverage     | P3       |
| GAP-13 | BSC: PancakeSwap V3 missing         | L0    | BSC      | Volume coverage     | P3       |
| GAP-14 | Learning trains on stub features    | L10   | ALL      | Adaptation blocked  | P4       |
| GAP-15 | No ranking, exploration always off  | L6    | ALL      | Signal discovery    | P4       |
| GAP-16 | No per-chain DQ thresholds          | L1    | ALL      | Miscalibrated       | P4       |
| GAP-17 | No price oracle factory             | infra | Future   | Scaling blocked     | P5       |

---

## Recommended Implementation Sequence

```
Phase 1 — Safety (P0)
  GAP-02: Implement PriceClient for ETH + BSC + Solana; wire into cmd/server.go
  GAP-01: Implement real DQ detectors (honeypot sim, tax, LP lock, wash, rug)
  Expected DataQuality factor: 0.10 → 0.65

Phase 2 — Signal Quality (P1)
  GAP-03: Real feature signals (tx velocity, wallet entropy, token age, momentum, liquidity USD)
  GAP-04: Wire ProbabilityModel output into validation EV gate
  GAP-05: Kelly-adjacent capital sizing
  Expected Features × Probability × Capital: 0.30 × 0.40 × 0.40 → ~0.70 × 0.70 × 0.65

Phase 3 — Execution Quality (P2)
  GAP-06: EIP-1559 Type-2 transactions for ETH
  GAP-07: Live ETH/BNB price oracle
  GAP-10: Slippage model self-corrects once GAP-03 done (no extra work)
  GAP-08: Flashbots relay integration (ETH only)
  GAP-09: Provision SNIPER_WALLET_N env vars (operational)

Phase 4 — Coverage (P3)
  GAP-11: Add Solana programs (PumpFun AMM graduation, Raydium CLMM, Orca, Meteora)
  GAP-12: Add SushiSwap factories to ETH config
  GAP-13: Add PancakeSwap V3 to BSC config

Phase 5 — Adaptation (P4)
  GAP-14: Learning becomes real once Phase 1–2 complete; raise sample gate
  GAP-15: Multi-position ranking and exploration band
  GAP-16: Per-chain DQ threshold config

Phase 6 — Multi-chain Scaling (P5)
  GAP-17: Price oracle factory for new chains
```

**Phase 1 alone** moves the combined profit multiplier from ~0.1% to ~5–10% of theoretical maximum. Everything else is compounding improvement on top of that baseline.

---

## Key Architectural Rules for All Implementations

1. **Chain-specific logic belongs only in `internal/modules/ingestion*/` and `internal/modules/execution*/`** — all other modules must remain chain-agnostic via config and interfaces
2. **All DQ simulation (honeypot, tax) requires RPC calls** — the DQ module must accept an injected RPC client from the worker; it must not import `database/` or `internal/rpc/` directly
3. **PriceClient implementations go in `internal/rpc/`** — not in `internal/modules/position/`
4. **All new thresholds go in `config/chains.yaml` or `config/pipeline.yaml`** — no hardcoded values in module code
5. **All new DTO fields are additive only** — never modify existing fields in `contracts/`
