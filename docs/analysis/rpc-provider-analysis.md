# RPC Provider Analysis — Helius vs QuickNode

> **Historical snapshot** · 2026-05-14 · Operational reference — verify against current
> [`shared/config/chains.yaml`](../../shared/config/chains.yaml) before applying.

## Profit-First Infrastructure Decision Guide

> **Audience:** Operator deploying the crypto-sniping-bot for the first time with a ~$50/month total budget.
> **Date:** 2026-05-14
> **Canonical invariant:** `Profit = Edge × Probability × Execution × Capital × DataQuality × AdaptationQuality`
> Every infrastructure decision below is evaluated against this invariant — plans that don't improve at least one factor are not worth buying.

---

## 1. Current Codebase State — What Free Tiers Actually Block

Before comparing providers, understand what is **hard-blocked** by the current free-tier configuration. These are not software bugs — they are RPC quota constraints embedded as comments and config fallbacks throughout the codebase.

### 1.1 Solana Ingestion Layer (Layer 0)

| Constraint                     | Where in Code                      | Root Cause                                                              | Profit Factor Killed                                                  |
| ------------------------------ | ---------------------------------- | ----------------------------------------------------------------------- | --------------------------------------------------------------------- |
| QuickNode WS commented out     | `shared/config/chains.yaml` line 94–96    | Free tier = 1 logsSubscribe slot; 3 programs = 3 slots → `-32003` error | **Edge** — no real-time pool detection, only HTTP polling             |
| 3 programs disabled            | `shared/config/chains.yaml` lines 133–138 | Need 6 WS slots for all programs                                        | **Edge** — Raydium CLMM + Orca Whirlpool + Meteora DLMM = zero events |
| `get_transaction_rps: 2`       | `shared/config/chains.yaml` line 145      | Free daily quota — at 2 RPS = ~172k lookups/day                         | **DataQuality** — honeypot/tax simulation calls starved               |
| `transport.mode: "rpc"`        | `shared/config/chains.yaml` line 203      | gRPC disabled — no Yellowstone endpoint funded                          | **Execution** — ~200ms WS+RPC vs sub-100ms gRPC path unused           |
| `rate_limit_backoff_ms: 60000` | `shared/config/chains.yaml` line 148      | After a `-32003` quota error, suppresses all tx fetches for 60s         | **DataQuality** — entire signal pipeline stalls after burst           |

### 1.2 EVM Ingestion Layer (ETH + BSC)

| Constraint                                | Config                                                | Profit Factor Impact                                                                           |
| ----------------------------------------- | ----------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| ETH RPC on public/free fallback endpoints | `shared/config/chains.yaml` — `${ETH_RPC_1}`, `${ETH_RPC_2}` | **Edge** — public RPCs rate-limit `eth_getLogs` (needed for TxVelocity, WalletEntropy signals) |
| BSC WS on free endpoint                   | `shared/config/chains.yaml` — `${BSC_WS_1}`                  | **Edge** — free WS drops under load; misses PairCreated events                                 |
| Honeypot `eth_call` simulation            | `docs/analysis/profitability-gaps.md` GAP-01                   | **DataQuality** — can only run if RPC allows simulations at scale                              |
| `eth_getLogs` for feature extraction      | `docs/analysis/profitability-gaps.md` GAP-03                   | **Features** — TxVelocity, WalletEntropy, VolumeMomentum all need frequent getLogs calls       |

### 1.3 Jito / ZeroSlot (Solana MEV Protection)

| Feature                  | Current State                         | Unlock Requirement                                                                         |
| ------------------------ | ------------------------------------- | ------------------------------------------------------------------------------------------ |
| Jito bundle submission   | `shadow_mode: true` — no real bundles | Mainnet Jito is free via public endpoints; only the RPC provider matters for tx forwarding |
| ZeroSlot private mempool | `${SOLANA_ZEROSLOT_HTTP}` unset       | ZeroSlot has its own pricing ($50–200/mo) — separate from Helius/QuickNode                 |

### 1.4 Capital Configuration (from `shared/config/capital.yaml`)

```
base_size_usd: 5.0     # micro-capital mode
min_size_usd:  5.0
max_size_usd:  500.0   # ceiling ready but Kelly sizing needs real P values
```

The Kelly-adjacent capital engine (`use_dynamic_sizing: true`) requires real probability estimates from the probability model. With throttled RPC → degraded features → degraded probability estimates → Kelly outputs near zero → positions sized at `min_size_usd = $5.0`. Fixing RPC is a prerequisite for capital efficiency, not just signal quality.

---

## 2. Provider Feature Matrix at ~$49/month

### 2.1 Helius Developer ($49/month, $24.50 first month)

> Source: helius.dev/pricing — **Solana-only provider**

| Feature                 | Value                                     | Bot Impact                                                                                      |
| ----------------------- | ----------------------------------------- | ----------------------------------------------------------------------------------------------- |
| Monthly credits         | 10M                                       | Sufficient for Solana-only operation                                                            |
| RPC requests/sec        | 50                                        | Replace `get_transaction_rps: 2` → set to **45** (leave 5 headroom)                             |
| sendTransaction/sec     | 5                                         | Sufficient for `concurrency_limit: 10` + wallet sharding                                        |
| sendBundle/sec          | ❌ none                                   | Cannot use Jito via Helius RPC (must hit Jito block engine directly — already wired separately) |
| Enhanced WebSockets     | ✅ **unlimited concurrent logsSubscribe** | **Unlocks all 6 Solana programs** — the single most critical unlock at this tier                |
| Staked Connections      | ✅                                        | Faster tx confirmation — reduces `confirm_timeout_ms` risk                                      |
| LaserStream gRPC        | ⚠️ **devnet only** at $49                 | Cannot use `transport.mode: "grpc"` in production                                               |
| DAS requests/sec        | 10                                        | Token metadata probes (social link validation in DQ layer)                                      |
| getProgramAccounts/sec  | 25                                        | Useful for LP lock verification (GAP-01)                                                        |
| Archival data           | ✅                                        | Historical replay for learning engine                                                           |
| Webhooks                | ✅                                        | Alternative event delivery if WS drops                                                          |
| EVM chains covered      | ❌ **zero**                               | ETH and BSC require separate RPC provider                                                       |
| First-month price       | **$24.50** (50% off)                      | Saves $24.50 vs QuickNode in month 1                                                            |
| Uptime SLA              | 99.99%                                    | Better than QuickNode's 99.84%                                                                  |
| Dedicated RPC nodes     | ✅                                        | Exclusive node; not shared pool                                                                 |
| Solana domain expertise | ✅                                        | Better DAS, Transaction Parsing API, Indexing                                                   |

**Helius mainnet gRPC (Yellowstone/LaserStream) unlocks at:** Business tier = $499/month.

### 2.2 QuickNode Build ($49/month)

> Source: dashboard.quicknode.com/select-plan — **Multi-chain provider**

| Feature                  | Value                       | Bot Impact                                                           |
| ------------------------ | --------------------------- | -------------------------------------------------------------------- |
| Monthly API credits      | 80M                         | 8× Helius credits — covers ETH + BSC + Solana simultaneously         |
| Additional credits       | $0.62/1M                    | Burst protection                                                     |
| Requests/sec             | 50                          | Same as Helius Developer                                             |
| Endpoints                | 10                          | Create separate endpoints for ETH, BSC, Solana                       |
| All supported chains     | ✅ ETH + BSC + Solana + 50+ | Covers all 3 chains in your config                                   |
| Multi-chain endpoints    | ✅                          | Single API key for all markets                                       |
| Archive data             | ✅                          | Historical data for all chains                                       |
| Metered gRPC data        | ✅                          | gRPC access but metered — see note below                             |
| Solana active streams    | 5 (from Build Streams plan) | **5 < 6 programs** — cannot run all 6 Solana programs simultaneously |
| Active streams (general) | 10                          | Enough for ETH + BSC factory listeners                               |
| Logs retention           | 1 hour                      | Minimal — not useful for replay                                      |
| sendBundle/sec           | ❌ none at $49              | Jito via QuickNode requires Accelerate ($249+)                       |
| EVM chains covered       | ✅ **ETH + BSC included**   | No separate EVM provider needed                                      |
| First-month price        | $49 (no discount)           | $24.50 more expensive than Helius in month 1                         |
| Uptime SLA               | 99.84%                      | Lower than Helius                                                    |

**Critical QuickNode constraint:** The Streams plan shows **5 Solana active streams** at Build tier. Your codebase requires 6 concurrent `logsSubscribe` slots (one per program). QuickNode Build cannot run all 6 Solana programs simultaneously.

**QuickNode mainnet gRPC unlocks at:** Accelerate = $249/month.

### 2.3 Head-to-Head at $49/month

| Capability                   | Helius Developer         | QuickNode Build    | Winner               |
| ---------------------------- | ------------------------ | ------------------ | -------------------- |
| All 6 Solana programs via WS | ✅ unlimited Enhanced WS | ❌ 5-stream cap    | **Helius**           |
| ETH chain coverage           | ❌ Solana only           | ✅ included        | **QuickNode**        |
| BSC chain coverage           | ❌ Solana only           | ✅ included        | **QuickNode**        |
| Credits volume               | 10M                      | 80M                | **QuickNode**        |
| Mainnet gRPC (Yellowstone)   | ❌ devnet only           | ❌ metered/limited | Tie — neither        |
| Staked Connections           | ✅                       | ✅                 | Tie                  |
| sendBundle                   | ❌                       | ❌                 | Tie — neither at $49 |
| First-month cost             | **$24.50**               | $49                | **Helius**           |
| Uptime                       | **99.99%**               | 99.84%             | **Helius**           |
| Dedicated nodes              | ✅                       | shared pool        | **Helius**           |
| DAS / Token metadata         | ✅ Solana-native         | generic            | **Helius**           |
| EVM feature signals          | ❌ needs 2nd provider    | ✅ one plan        | **QuickNode**        |

---

## 3. Vultr VPS at $6/month — Reality Check

Vultr's $6/month tier is the **Regular Cloud Compute: 1 vCPU / 1GB RAM / 25GB SSD**.

Your bot's process requirements at steady state:

| Process                             | Minimum RAM    | Notes                                            |
| ----------------------------------- | -------------- | ------------------------------------------------ |
| PostgreSQL (event bus + all tables) | ~200–400MB     | With `max_open_conns: 5`, `shared_buffers=128MB` |
| Go bot binary + worker goroutines   | ~100–200MB     | 8 `processing_workers` + 14 pipeline stages      |
| WS connections (6 Solana programs)  | ~20–50MB       | One goroutine per stream                         |
| EVM WS listeners (ETH + BSC)        | ~30–60MB       | One goroutine per WS endpoint                    |
| OS baseline                         | ~150–200MB     | Ubuntu minimal                                   |
| **Total (all chains)**              | **~500–910MB** | Dangerously close to 1GB OOM kill zone           |
| **Total (Solana only)**             | **~400–700MB** | Viable with tuning                               |

### 3.1 Vultr $6/month — Viable Configurations

| Mode                    | Feasible?               | Required Config Tuning                                                                                         |
| ----------------------- | ----------------------- | -------------------------------------------------------------------------------------------------------------- |
| Solana-only, 6 programs | ✅ **Tight but viable** | `max_open_conns: 5`, `processing_workers: 4`, `compute_max_queue_depth: 200`, PostgreSQL `shared_buffers=64MB` |
| Solana + ETH            | ⚠️ Risky                | OOM likely during burst; swap partition required                                                               |
| Solana + ETH + BSC      | ❌ Will OOM             | 3 WS connections + 3 program listeners + PostgreSQL = 1GB+                                                     |

**For all-chains mode**: Vultr **$12/month** (2 vCPU / 4GB RAM) is the minimum viable target. Alternatively, Vultr High Performance 1vCPU / 2GB at $12/month with NVMe SSD (better PostgreSQL I/O).

### 3.2 Vultr Swap Workaround for $6 Tier

If using Vultr $6 with Solana-only mode:

```bash
# Add 1GB swap (one-time setup on server)
fallocate -l 1G /swapfile && chmod 600 /swapfile
mkswap /swapfile && swapon /swapfile
echo '/swapfile none swap sw 0 0' >> /etc/fstab
```

This buys ~1GB of overflow but swap thrash will degrade PostgreSQL I/O. Monitor with `free -h` and OOM killer logs.

---

## 4. Budget Scenarios — Month 1

All scenarios assume Vultr $6/mo unless noted. Total budget ceiling: ~$55 (plan + VPS).

### Scenario A — Helius Developer + Vultr $6 (Solana-first) ⭐ **Recommended for Month 1**

```
Helius Developer (50% first-month discount): $24.50
Vultr Cloud Compute 1vCPU/1GB:               $ 6.00
QuickNode Free (ETH/BSC fallback):           $ 0.00
──────────────────────────────────────────────────────
Total:                                       $30.50
Remaining budget buffer:                     $19.50
```

**What this unlocks:**

- All 6 Solana programs via Enhanced WebSockets → `logsSubscribe` to Raydium V4, PumpFun, PumpFun AMM, Raydium CLMM, Orca Whirlpool, Meteora DLMM
- `get_transaction_rps` raised to 45 → honeypot simulations, feature extraction calls, LP lock probes
- Staked Connections → faster tx confirmation, lower `confirm_timeout_ms` risk
- DAS requests 10/sec → social link metadata probes (DQ Layer 1 hard-reject checks)
- QuickNode Free tier handles ETH + BSC HTTP polling (limited, but functional for basic event detection)

**What this does NOT unlock:**

- mainnet gRPC → `transport.mode` stays `"rpc"` (~200ms latency vs sub-100ms)
- Jito bundles via Helius (hit Jito block engine directly — already wired in codebase)
- ETH/BSC real-time WS (QuickNode free only gives 1 WS connection per chain)

**Profit factor improvement over free tier:**

| Factor         | Free Tier | Scenario A | Delta                                                                |
| -------------- | --------- | ---------- | -------------------------------------------------------------------- |
| Edge (Solana)  | ~0.60     | ~0.80      | +33% — real-time events, all 6 DEX families                          |
| DataQuality    | ~0.10     | ~0.35      | +250% — 45 RPS enables honeypot/tax simulation                       |
| Features       | ~0.30     | ~0.55      | +83% — TxVelocity, WalletEntropy, VolumeMomentum from real RPC calls |
| Execution      | ~0.75     | ~0.80      | +7% — Staked Connections, lower confirmation latency                 |
| Combined delta | 0.1%      | ~1.2%      | **12× improvement in effective profit multiplier**                   |

### Scenario B — QuickNode Build + Vultr $6

```
QuickNode Build:                             $49.00
Vultr Cloud Compute 1vCPU/1GB:               $ 6.00
──────────────────────────────────────────────────────
Total:                                       $55.00
```

**What this unlocks:**

- ETH + BSC + Solana under one plan → no separate EVM provider needed
- 80M credits → covers all 3 chains without throttling
- 10 endpoints → dedicated endpoint per chain market

**What this does NOT unlock:**

- All 6 Solana programs simultaneously — **5-stream cap** at Build tier means you must choose 5 of 6 programs
- mainnet gRPC (metered/limited at Build)
- 1GB Vultr RAM — OOM risk with 3 concurrent chains + PostgreSQL

**Profit factor vs Scenario A:**

- ETH + BSC chains get real paid RPC → Edge improvement on EVM markets
- Solana loses one program family → Edge regression on Solana vs Scenario A
- No first-month discount → $24.50 extra cost vs Scenario A

**Verdict:** Scenario B costs $24.50 more in month 1 and still can't run all 6 Solana programs. Scenario A is strictly better at $50 budget.

### Scenario C — Helius Developer + Vultr $12 (2GB) — Month 2+ if profitable

```
Helius Developer (ongoing):                  $49.00
Vultr High Performance 2GB/1vCPU:            $12.00
──────────────────────────────────────────────────────
Total:                                       $61.00
```

**Adds vs Scenario A:**

- 2GB RAM → stable operation with all 6 Solana programs + ETH + BSC (QuickNode free for EVM)
- NVMe SSD → PostgreSQL I/O 3–5× faster → event bus throughput improves
- No OOM risk under burst

### Scenario D — Helius Developer + QuickNode Build + Vultr $12 — "All Chains Max"

```
Helius Developer:                            $49.00
QuickNode Build (EVM chains only):           $49.00
Vultr 2GB:                                   $12.00
──────────────────────────────────────────────────────
Total:                                       $110.00
```

**Only viable if bot shows positive EV in Scenario A first.** Provides:

- Helius: all 6 Solana programs, Enhanced WS, 45 RPS, Staked
- QuickNode: ETH/BSC on paid RPC, 50 RPS, 80M credits
- Full `eth_getLogs` throughput for EVM feature extraction
- All scam detectors (GAP-01) operational across all chains

---

## 5. All 6 Solana Programs — Unlock Analysis

Current `shared/config/chains.yaml` state — 3 active, 3 commented out:

```
ACTIVE:   raydium-v4        (675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8)
ACTIVE:   pumpfun            (6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P)
ACTIVE:   pumpfun-amm        (pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA)
DISABLED: raydium-clmm       (CAMMCzo5YL8w4VFF8KVHrK22GGUsp5VTaW7grrKgrWqK)
DISABLED: orca-whirlpool     (whirLbMiicVdio4qvUfM5KAg6Ct8VwpYzGff3uctyCc)
DISABLED: meteora-dlmm       (LBUZKhRxPF3XUpBCjp4YzTKgLLjLeNox4HgSehp9ZSe)
```

The code comment explains the reason: "3 active programs → 3 WS slots needed vs 6. Re-enable once a higher-tier RPC plan is in place."

### 5.1 What Each Disabled Program Covers

| Program          | DEX Family                         | New Pool Volume               | Edge Profile                           |
| ---------------- | ---------------------------------- | ----------------------------- | -------------------------------------- |
| `raydium-clmm`   | Raydium Concentrated Liquidity     | Medium — institutional tokens | Higher liquidity, lower pump magnitude |
| `orca-whirlpool` | Orca (Solana's largest DEX by TVL) | Medium-High                   | Quality tokens, less meme noise        |
| `meteora-dlmm`   | Meteora Dynamic Liquidity          | Growing rapidly in 2026       | New launch edge, similar to Raydium V4 |

**Enabling all 6 programs expands the total addressable signal universe by approximately 2×.** The bot currently sees only PumpFun launches and Raydium V4 pools. Orca Whirlpool and Meteora DLMM represent significant organic liquidity events that the current config misses entirely.

### 5.2 WS Slot Requirement per Provider

| Provider             | Plan    | logsSubscribe Slots       | All 6 Programs?   |
| -------------------- | ------- | ------------------------- | ----------------- |
| QuickNode Free       | $0      | 1                         | ❌                |
| QuickNode Build      | $49     | 5 (Streams limit)         | ❌ (5 of 6 only)  |
| QuickNode Accelerate | $249    | 5 (same)                  | ❌                |
| **Helius Developer** | **$49** | **Unlimited Enhanced WS** | **✅**            |
| Helius Business      | $499    | Unlimited                 | ✅ + mainnet gRPC |

**Conclusion: Helius is the only $49 provider that supports all 6 programs simultaneously.**

---

## 6. All Chains (ETH + BSC + Solana) — Activation Analysis

Your config has all three chains fully wired with factory addresses and base tokens. The marginal cost of activating ETH and BSC depends on which RPC you use.

### 6.1 Chains and Markets Already Configured

```
eth-uniswap-v2    → Uniswap V2 factory   (PairCreated events)
eth-uniswap-v3    → Uniswap V3 factory   (PoolCreated events)
eth-sushiswap-v2  → SushiSwap V2 factory (PairCreated events)
eth-sushiswap-v3  → SushiSwap V3 factory (PoolCreated events)
bsc-pancake-v2    → PancakeSwap V2       (PairCreated events)
bsc-pancake-v3    → PancakeSwap V3       (PoolCreated events)
solana (6 programs as above)
```

### 6.2 RPC Budget Estimate for All Chains Simultaneously

Per chain at steady state (estimated RPS load):

| Chain     | HTTP RPS (polling + feature extraction) | WS Connections         | Notes                              |
| --------- | --------------------------------------- | ---------------------- | ---------------------------------- |
| Solana    | 30–45 RPS                               | 6 logsSubscribe        | Helius handles this                |
| ETH       | 5–15 RPS                                | 4 WS (one per factory) | eth_getLogs for signals            |
| BSC       | 5–10 RPS                                | 2 WS (one per factory) | Same pattern as ETH                |
| **Total** | **40–70 RPS**                           | **12 total**           | Exceeds single $49 plan RPS budget |

Running all chains simultaneously with high-quality feature extraction requires either:

- Two paid plans (Helius for Solana + QuickNode for EVM) = ~$98/month
- One QuickNode plan with 50 RPS budget split across chains (signal quality degrades on each chain)

**Recommended approach:** Start with Solana-only on Helius Developer. Add EVM chains only after the Solana pipeline proves positive EV.

---

## 7. gRPC Upgrade Path (Yellowstone/LaserStream)

The codebase already supports gRPC via `transport.mode: "grpc"` in `shared/config/chains.yaml`. This path yields sub-100ms stream latency vs ~200ms for WS+RPC, directly improving the **Execution** profit factor.

| Provider  | Plan           | gRPC Availability       | Monthly Cost |
| --------- | -------------- | ----------------------- | ------------ |
| Helius    | Developer      | devnet only             | $49          |
| Helius    | **Business**   | **mainnet LaserStream** | **$499**     |
| QuickNode | Build          | metered, limited        | $49          |
| QuickNode | **Accelerate** | **mainnet Yellowstone** | **$249**     |

**Decision gate:** Only upgrade to gRPC when the bot generates ≥$200/month net profit. At that point, the $199–450 extra monthly cost for gRPC is justified by latency-sensitive alpha. Before that, the 100ms latency difference is dominated by software gaps (GAP-01 through GAP-05 in `PROFITABILITY_GAPS.md`) — fix the code first.

---

## 8. Exact Config Changes Per Scenario

### 8.1 Scenario A Config Changes (Helius Developer → apply to `shared/config/chains.yaml`)

**Step 1 — Uncomment QuickNode WS** (requires paid WS plan — OR use Helius WS directly):

```yaml
# Switch SOLANA_WS_2 (Helius) to priority 1, it now handles all 6 programs
- url: "${SOLANA_WS_2}"
  priority: 1
  kind: ws
  region: us-east
  provider: helius # ← now primary WS provider
```

**Step 2 — Uncomment all 3 disabled programs:**

```yaml
- program_id: "CAMMCzo5YL8w4VFF8KVHrK22GGUsp5VTaW7grrKgrWqK"
  family: raydium-clmm
- program_id: "whirLbMiicVdio4qvUfM5KAg6Ct8VwpYzGff3uctyCc"
  family: orca-whirlpool
- program_id: "LBUZKhRxPF3XUpBCjp4YzTKgLLjLeNox4HgSehp9ZSe"
  family: meteora-dlmm
```

**Step 3 — Raise transaction fetch rate limit:**

```yaml
get_transaction_rps: 45 # was: 2
rate_limit_backoff_ms: 5000 # was: 60000 — reduce dead-zone after quota error
```

**Step 4 — Tune stagger for 6 programs (10s × 6 = 60s startup ramp):**

```yaml
ws_subscribe_stagger_ms: 8000 # was: 10000 — slightly faster ramp for 6 programs
```

### 8.2 Vultr $6 Memory Tuning (Solana-only mode — `shared/config/pipeline.yaml`)

```yaml
# Reduce from defaults to fit 1GB RAM:
worker:
  max_retry_count: 3

# In PostgreSQL (not in config — server-side tuning):
# shared_buffers = 64MB
# work_mem = 4MB
# max_connections = 25
```

Also in `shared/config/pipeline.yaml`:

```yaml
# Reduce processing worker pool for 1GB VPS:
# (currently shared/config/chains.yaml)
processing_workers: 4 # was: 8
```

### 8.3 Jito Enable (free — no provider upgrade needed)

Jito block engine endpoints are free and public. Your codebase has Jito wired with `shadow_mode: true`. Once on a paid RPC plan, enable real Jito submissions:

```yaml
# In shared/config/execution.yaml Jito section — set via env vars:
# JITO_BUNDLE_URL=https://mainnet.block-engine.jito.wtf/api/v1/bundles
# JITO_TIP_ACCOUNT=96gYZGLnJYVFmbjzopPSU6QiEV5fGqZNyN9nmNhvrZU5
# shadow_mode: false  ← flip after 1 week of shadow mode validation
```

---

## 9. Decision Framework — What to Buy First

```
Budget ≤ $55/month?
    │
    ├─ YES → Helius Developer $24.50 (month 1) + Vultr $6 = $30.50
    │          ├── Run Solana-only, all 6 programs
    │          ├── Fix GAP-01/02/03 in parallel (software — no extra cost)
    │          ├── Validate positive EV over 2 weeks shadow trading
    │          └── Month 2: continue Helius $49 + Vultr $6 = $55 if profitable
    │
    ├─ $55–$65 → Helius Developer $49 + Vultr $12 (2GB) = $61
    │              └── Stable all-6-programs + add ETH via QuickNode free
    │
    ├─ $100–$120 → Helius $49 + QuickNode Build $49 + Vultr $12 = $110
    │                └── Full all-chains: ETH + BSC + Solana all at paid quality
    │
    └─ $250+ → QuickNode Accelerate $249 + Vultr $12 = $261
                 └── Mainnet Yellowstone gRPC → transport.mode: "grpc"
                     Only after bot proves ≥$200/month net profit
```

---

## 10. Priority Order — What Moves the Profit Needle Most per Dollar

Ranked by `(profit factor delta) / (monthly cost)`:

| Rank | Action                                 | Cost/month                             | Profit Factor                  | Delta                   |
| ---- | -------------------------------------- | -------------------------------------- | ------------------------------ | ----------------------- |
| 1    | Fix GAP-01 (DQ scam detectors)         | $0 — code change                       | DataQuality 0.10 → 0.45        | **4.5×**                |
| 2    | Fix GAP-02 (priceClient nil)           | $0 — code change                       | Execution (exits working)      | **critical**            |
| 3    | Fix GAP-03 (feature stubs)             | $0 — code change                       | Features 0.30 → 0.70           | **2.3×**                |
| 4    | **Helius Developer** (RPC unlock)      | $24.50                                 | Edge + DataQuality + Features  | **enables 1–3 to work** |
| 5    | Enable all 6 Solana programs           | $0 — config change (needs Helius paid) | Edge ~0.80 → 0.90              | +13%                    |
| 6    | Vultr $6 → $12 upgrade                 | +$6/month                              | Stability (no OOM)             | reliability             |
| 7    | QuickNode Build (EVM paid)             | +$49/month                             | Edge on ETH + BSC              | +20% on EVM markets     |
| 8    | Jito bundle submission                 | $0 — env var flip                      | Execution (MEV protection)     | reduces sandwich loss   |
| 9    | Helius Business / QN Accelerate (gRPC) | +$200–450/month                        | Execution latency 200ms → 80ms | ~15% on fast tokens     |

**The top 3 are software changes that cost nothing and dwarf all RPC provider upgrades in impact.** The RPC upgrade (rank 4) is the prerequisite that makes ranks 1–3 computationally feasible — you can't run honeypot simulations at `get_transaction_rps: 2`.

---

## 11. Summary Recommendation

**Month 1 (shadow trading / micro-capital validation):**

- Subscribe: **Helius Developer at $24.50** (50% first-month discount)
- VPS: **Vultr Regular Cloud Compute 1vCPU/1GB at $6.00**
- Total: **$30.50**
- Action: Apply all Scenario A config changes above; enable all 6 Solana programs; continue fixing GAP-01/02/03 in parallel

**Month 2 (if shadow mode shows positive EV):**

- Continue: **Helius Developer at $49**
- Upgrade VPS: **Vultr 2GB at $12** for stability
- Total: **$61** — fund from trading profits

**Month 3+ (if net positive after costs):**

- Add: **QuickNode Build at $49** for ETH + BSC paid RPC
- Total: **$110** — only justified by demonstrated EVM edge

**Never buy gRPC tier before demonstrating positive expected value** — the latency improvement from gRPC (100ms faster) only matters when the edge, signal quality, and capital sizing are already working. At the current software state, 100ms of latency is not the bottleneck.
