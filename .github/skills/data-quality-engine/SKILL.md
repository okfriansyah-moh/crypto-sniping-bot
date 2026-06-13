---
name: data-quality-engine
type: skill
description: >
  Adaptive firewall for rug/honeypot/wash/manipulation detection. Use when implementing
  or reviewing the DataQuality layer (Layer 1), detector algorithms, threshold profiles,
  and DataQualityDTO production. The most critical safety layer — if it fails, profit → 0.
---

# Data Quality Engine Skill

## Purpose

Enforce correct implementation of the adaptive firewall that blocks scams, manipulation,
and fake edges BEFORE they consume capital. This layer is a hard gate — it cannot be
optimistic or lenient by default.

**Core invariant:** `Profit = Edge × Probability × Execution × Capital × DataQuality × AQ`
If `DataQuality = 0` (all traps pass through), `Profit → 0`. No other layer compensates.

---

## Rules

### Decision Outputs

Every `DataQualityDTO` MUST have `Decision ∈ {"pass", "risky-pass", "reject"}`.
No other values are valid. Downstream modules MUST gate on this field.

```go
// Correct: downstream module gates on Decision
if dq.Decision == "reject" {
    return nil, ErrRejectedByDataQuality
}
// "risky-pass" is allowed through with reduced capital (handled by capital engine)
```

### Five Core Detectors

Each detector outputs a `[0,1]` risk score and a list of flags. The final `RiskScore` is
a weighted aggregate — weights are config-driven.

#### 1. Wash Trading Detector

```
Signals:
  tx_count_1m      > config.wash.max_tx_count_1m         → flag "HIGH_TX_VOLUME"
  unique_wallets   < config.wash.min_unique_wallets       → flag "LOW_WALLET_DIVERSITY"
  wallet_entropy   < config.wash.min_wallet_entropy       → flag "LOW_ENTROPY"

Risk contribution = weighted combination of above flags
```

```yaml
# config/data_quality.yaml
wash_trading:
  max_tx_count_1m: 50
  min_unique_wallets: 5
  min_wallet_entropy: 1.5
  weight: 0.25
```

#### 2. Rug Pull Detector

```
Signals:
  lp_unlocked                    → flag "UNLOCKED_LP"
  owner_has_mint_privilege       → flag "OWNER_MINT"
  owner_has_pause_privilege      → flag "OWNER_PAUSE"
  top_holder_pct > 30%           → flag "CONCENTRATED_HOLDERS"
  deployer_sold_in_N_blocks      → flag "DEPLOYER_SOLD"
```

**Rule:** `lp_unlocked == true` alone is NOT sufficient to reject — weight it with other signals.
LP locking is common but not universal on new launches.

#### 3. Honeypot Detector

```
Signals:
  sell_simulation_failed         → flag "SELL_BLOCKED" (near-certain reject)
  buy_tax > config.max_buy_tax   → flag "HIGH_BUY_TAX"
  sell_tax > config.max_sell_tax → flag "HIGH_SELL_TAX"
  tax_is_dynamic                 → flag "DYNAMIC_TAX"
```

**Rule:** `sell_simulation_failed` = `Decision: reject` regardless of other signals.
A token you cannot sell is a guaranteed loss.

#### 4. Fake Liquidity Detector

```
Signals:
  lp_add_remove_within_N_blocks  → flag "LP_CHURN"
  liquidity_usd < threshold      → flag "LOW_LIQUIDITY"
  single_lp_provider_pct > 90%  → flag "SINGLE_LP_PROVIDER"
```

#### 5. Tax Manipulation Detector

```
Signals:
  current_tax != initial_tax     → flag "TAX_CHANGED"
  tax_changed_within_N_blocks    → flag "RECENT_TAX_CHANGE"
  buy_tax + sell_tax > threshold → flag "EXCESSIVE_TAX"
```

### Risk Score Aggregation

```go
// Deterministic weighted sum — no randomness
func aggregateRiskScore(scores map[string]float64, weights map[string]float64) float64 {
    total := 0.0
    for detector, score := range scores {
        total += score * weights[detector]
    }
    return clamp(total, 0.0, 1.0)  // always in [0,1]
}
```

### Decision Gate (Config-Driven Thresholds)

```yaml
# config/data_quality.yaml — per operational mode
thresholds:
  strict:
    reject_above: 0.3
    risky_pass_above: 0.15
    max_tax: 8
    min_liquidity_usd: 20000.0
  balanced:
    reject_above: 0.5
    risky_pass_above: 0.25
    max_tax: 12
    min_liquidity_usd: 10000.0
  exploration:
    reject_above: 0.65
    risky_pass_above: 0.35
    max_tax: 15
    min_liquidity_usd: 5000.0
```

```go
// Apply thresholds from active operational mode
func makeDecision(riskScore float64, flags []string, profile ThresholdProfile) string {
    // Hard overrides — these trump risk score
    for _, flag := range flags {
        if flag == "SELL_BLOCKED" { return "reject" }
    }
    if riskScore >= profile.RejectAbove { return "reject" }
    if riskScore >= profile.RiskyPassAbove { return "risky-pass" }
    return "pass"
}
```

### Adaptive Threshold Controller

```
Rule: Only ONE parameter family adjusted per learning cycle.
Rule: Δthreshold ≤ 5% per cycle (bounded updates).
Rule: Require N ≥ 30 samples before any threshold update.
Rule: Every update bumps config_version and creates a StrategyVersion snapshot.

Controller logic:
  if false_negative_rate > config.fn_alert_threshold → relax thresholds (→ EXPLORATION mode)
  if rug_loss_rate > config.rug_alert_threshold      → tighten thresholds (→ STRICT mode)
  if false_positive_rate > config.fp_alert_threshold → relax filters (reduce sensitivity)
```

### DataQualityDTO Output

```go
// All fields REQUIRED — adapter rejects partial writes
DataQualityDTO{
    EventID:       SHA256(canonical_json(dto))[:16],
    TokenAddress:  checksummedAddress,
    Decision:      "pass" | "risky-pass" | "reject",
    RiskScore:     float64,  // [0.0, 1.0]
    Flags:         []string, // non-nil, may be empty
    WashScore:     float64,  // per-detector contribution
    RugScore:      float64,
    HoneypotScore: float64,
    FakeLiqScore:  float64,
    TaxScore:      float64,
    EvaluatedAt:   ISO8601UTC,
    // Traceability
    TraceID:       copied from MarketDataDTO,
    CorrelationID: copied from MarketDataDTO,
    CausationID:   MarketDataDTO.EventID,   // MUST be set — not Layer 0
    VersionID:     activeStrategyVersion.VersionID,
}
```

### Anti-Patterns

```go
// ❌ Hardcoded thresholds
if riskScore > 0.5 { return "reject" }  // Wrong — use config

// ❌ Single-flag reject (except SELL_BLOCKED)
if lp_unlocked { return "reject" }  // Wrong — weighted aggregate

// ❌ Mutable DTO
dto.Decision = "pass"  // Wrong — DTO is immutable; create new one

// ❌ Missing flags on pass decision
return DataQualityDTO{Decision: "pass", Flags: nil}  // Wrong — Flags must be []string{}

// ❌ Bypassing sell simulation
if honeypot_api_unavailable { return "pass" }  // Wrong — should be "risky-pass" at minimum

// ✅ Correct
return DataQualityDTO{
    Decision: makeDecision(aggregateRiskScore(scores, weights), flags, profile),
    Flags:    flags,  // always a non-nil slice
    RiskScore: score,
    // ... all other fields
}
```

---

## Learning Signals (Feed to Learning Engine)

```
false_positive: accepted (pass/risky-pass) → later rug/loss
false_negative: rejected → later pump (observed via shadow observer)

These signals feed: docs/reference/architecture.md § 3.10 (Learning Engine)
Shadow trades table: database schema tracks rejected tokens' subsequent price action
```

---

## Performance Constraints

| Detector                | Max Latency | Notes                 |
| ----------------------- | ----------- | --------------------- |
| Tax check               | < 50ms      | Cached per token      |
| Sell simulation         | < 200ms     | Forked EVM call       |
| Wash trading            | < 10ms      | In-memory computation |
| Rug indicators          | < 100ms     | Cached contract state |
| **Total DQ evaluation** | **< 500ms** | Hard SLA              |

---

## Checklist

```
[ ] Decision is exactly "pass", "risky-pass", or "reject" — no other values
[ ] SELL_BLOCKED flag always results in "reject" regardless of risk score
[ ] All 5 detectors contribute to RiskScore
[ ] Thresholds loaded from config — not hardcoded
[ ] Threshold profiles exist for strict/balanced/exploration modes
[ ] RiskScore is always in [0.0, 1.0]
[ ] CausationID = MarketDataDTO.EventID (not empty)
[ ] Adaptive threshold updates are bounded (≤5% per cycle)
[ ] Adaptive updates require N ≥ 30 samples
[ ] Shadow trades stored for false negative computation
[ ] Module has zero DB imports — pure function consuming/returning DTOs
[ ] Sell simulation failure is treated as hard reject
```

---

## Phase 9 Notes (Profitability Restoration)

Per `docs/reference/implementation_roadmap.md` § 9.1, Phase 9 closes **GAP-01** by replacing the
five hardcoded `false` flags in `internal/modules/data_quality/data_quality.go` with
real RPC-backed detectors. This skill is the canonical reference for that work.

**Phase 9 mandates:**

- Detector inventory MUST be implemented per chain (EVM/Solana) per the table in § 9.1
- All detectors run concurrently via `errgroup` with per-detector context timeout `detector_timeout_ms` (default 800 ms from `config/data_quality.yaml`)
- LRU cache (`detector_cache.go`) keyed by `(chain, token_address, detector_name)` with per-detector TTL
- RPC timeout → `Indeterminate` flag → treated as risky-pass (never as safe)
- Module remains DB-free; RPC clients (`evm_simulator`, `solana_simulator`) injected at worker construction
- Honeypot fixtures MUST be rejected at L1 in 100 % of replays (Phase 9 exit criterion)
- DQ wall-time p95 ≤ `detector_timeout_ms × 1.1` (concurrent budget)
- Cache hit ratio ≥ 60 % over 24h replay (bounded RPC pressure)

**Files added in Phase 9:** `honeypot.go`, `tax_detector.go`, `lp_lock.go`,
`rug_authority.go`, `contract_verified.go`, `detector_cache.go`, plus
`internal/rpc/{evm_simulator.go, solana_simulator.go}`.

---

## References

- Architecture: `docs/reference/architecture.md` § 3.1 (Data Quality Engine)
- Architecture context: `docs/archive/architecture-context/3_data_quality_engine.md`
- DTO spec: `docs/reference/dto_contracts.md` § 3.2 (DataQualityDTO)
- Roadmap: `docs/reference/implementation_roadmap.md` Phase 2.1
- Config: `config/data_quality.yaml`
