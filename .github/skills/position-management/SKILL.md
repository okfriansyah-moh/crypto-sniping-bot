---
name: position-management
type: skill
description: >
  Position management and exit patterns (Layer 9). Use when implementing or reviewing
  TP1/TP2/SL/TIME exit logic, trailing stop, cohort-adaptive parameters, PositionStateDTO
  production, and the position polling loop. Exit timing determines actual PnL.
---

# Position Management Skill

## Purpose

Enforce correct implementation of the position engine that converts open positions into
realized profit or limited losses. Entry gives potential — exit determines actual PnL.

**Core objective:**

```
ExitDecision = argmax(realized_value - risk_of_reversal - time_decay)
```

**Baseline exit rules (config-driven, cohort-adaptive):**

```
TP1: +20%  → sell 50% of position  (lock partial profit)
TP2: +50%  → sell remaining 50%   (full exit)
SL:  -10%  → sell 100%            (hard loss limit)
TIME: 10-30 min → force exit 100% (critical for sniper — prevent bag holding)
```

---

## Rules

### Position State Machine

Each position goes through these states:

```
open → tp1_hit → tp2_hit → closed
open → sl_hit           → closed
open → time_expired     → closed
open → trailing_stopped → closed
```

```go
type ExitStage string
const (
    ExitStageNone     ExitStage = "none"
    ExitStageTP1Hit   ExitStage = "tp1_hit"
    ExitStageTP2Hit   ExitStage = "tp2_hit"
    ExitStageSLHit    ExitStage = "sl_hit"
    ExitStageTime     ExitStage = "time_expired"
    ExitStageTrailing ExitStage = "trailing_stop"
    ExitStageClosed   ExitStage = "closed"
)
```

### Exit Priority Order (Strict — Do Not Reorder)

```go
func evaluateExit(pos PositionState, currentPrice float64, nowSec int64, cfg ExitConfig) ExitDecision {
    pnlPct := (currentPrice - pos.EntryPrice) / pos.EntryPrice * 100

    // Priority 1: Stop Loss (protect capital — highest priority)
    if pnlPct <= -cfg.StopLossPct {
        return ExitDecision{Action: "sell_all", Reason: "sl_hit", SizePct: 1.0}
    }

    // Priority 2: Take Profit 2 (full exit — realize all profit)
    if pnlPct >= cfg.TP2Pct {
        return ExitDecision{Action: "sell_all", Reason: "tp2_hit", SizePct: 1.0}
    }

    // Priority 3: Take Profit 1 (partial exit — only if not already taken)
    if pnlPct >= cfg.TP1Pct && pos.ExitStage == ExitStageNone {
        return ExitDecision{Action: "sell_partial", Reason: "tp1_hit", SizePct: cfg.TP1SellPct}
    }

    // Priority 4: Trailing stop (after TP1 hit, protect gains from peak)
    if pos.ExitStage == ExitStageTP1Hit {
        peakDropPct := (pos.PeakPrice - currentPrice) / pos.PeakPrice * 100
        if peakDropPct >= cfg.TrailingStopPct {
            return ExitDecision{Action: "sell_all", Reason: "trailing_stop", SizePct: 1.0}
        }
    }

    // Priority 5: Time exit (critical — prevents bag holding)
    ageSeconds := nowSec - pos.OpenedAtUnix
    if ageSeconds >= int64(cfg.MaxHoldingSec) {
        return ExitDecision{Action: "sell_all", Reason: "time_expired", SizePct: 1.0}
    }

    return ExitDecision{Action: "hold"}
}
```

### Cohort-Adaptive Parameters

Exit thresholds are adapted per cohort by the learning engine:

```yaml
# config/position.yaml
position:
  default:
    tp1_pct: 20.0
    tp2_pct: 50.0
    sl_pct: 10.0
    tp1_sell_pct: 0.50 # sell 50% at TP1
    trailing_stop_pct: 12.0
    max_holding_sec: 600 # 10 minutes (sniper mode)
  cohort_overrides:
    # Learning engine updates these per cohort
    small_liquidity_high_tax:
      max_holding_sec: 300 # shorter hold for risky cohort
      sl_pct: 8.0 # tighter SL
    large_liquidity_low_tax:
      max_holding_sec: 900 # allow longer hold for safer cohort
      tp2_pct: 80.0 # higher TP2 for quality cohort
```

### Price Polling Loop (Deterministic)

```go
// Position poller: runs every config.price_poll_interval_ms
func pollPositions(ctx context.Context, adapter database.Adapter, cfg PositionConfig) {
    ticker := time.NewTicker(time.Duration(cfg.PollIntervalMs) * time.Millisecond)
    for {
        select {
        case <-ticker.C:
            positions := adapter.GetOpenPositions(ctx)
            for _, pos := range positions {
                price := priceCache.GetLatest(pos.TokenAddress)  // cached — no RPC per position
                decision := evaluateExit(pos, price, time.Now().Unix(), cfg)
                if decision.Action != "hold" {
                    // Emit position_event → execution worker handles the actual sell
                    adapter.EmitPositionEvent(ctx, buildPositionDTO(pos, decision))
                }
            }
        case <-ctx.Done():
            return
        }
    }
}
```

**Rule:** Price is read from a local cache (updated by a separate subscription), never
via RPC per-position-check. RPC per position = too slow for sniper timing.

### PositionStateDTO Output

Emitted multiple times per position: on open, on each state change, on close.

```go
PositionStateDTO{
    EventID:       SHA256(canonical_json(dto))[:16],
    PositionID:    SHA256(token+wallet+open_tx_hash)[:16],
    TokenAddress:  pos.TokenAddress,
    Wallet:        pos.Wallet,
    EntryPrice:    float64,
    CurrentPrice:  float64,
    PeakPrice:     float64,
    PnLPct:        float64,  // percentage, signed
    PnLUSD:        float64,  // absolute, signed
    Size:          float64,
    RemainingSize: float64,
    ExitStage:     ExitStage,
    ExitReason:    string,   // "sl_hit", "tp1_hit", etc. — empty if open
    AgeSec:        int64,
    CohortID:      string,
    SnapshotAt:    ISO8601UTC,
    // Traceability
    TraceID:       exec.TraceID,
    CorrelationID: exec.CorrelationID,
    CausationID:   exec.EventID,
    VersionID:     activeStrategyVersion.VersionID,
}
```

### TIME Exit Is Non-Negotiable

```
For sniper mode: holding beyond the time window turns a edge trade into speculation.
TIME exit MUST be enforced — never disable it in config without operator override logged.

Warning threshold at 80% of max_holding_sec:
  if age > 0.8 × max_holding_sec → emit system_event "approaching_time_exit"
```

### Anti-Patterns

```go
// ❌ Wrong priority order (SL after TP checks)
if pnlPct >= tp1Pct { ... }
if pnlPct <= -slPct { ... }  // Wrong — SL must be checked FIRST

// ❌ Hardcoded exit thresholds
if pnlPct >= 20.0 { ... }  // Wrong — use config-driven cohort thresholds

// ❌ RPC call per position in polling loop
price := rpcClient.GetTokenPrice(pos.TokenAddress)  // Wrong — use price cache

// ❌ Never exiting (time exit disabled)
cfg.MaxHoldingSec = 9999999  // Wrong — sniper relies on time exit

// ❌ TP1 re-triggered
if pnlPct >= tp1Pct { sellPartial() }  // Wrong — check pos.ExitStage == "none"

// ✅ Correct: priority order with state gate
if pnlPct <= -cfg.SL { return sellAll("sl_hit") }
if pnlPct >= cfg.TP2 { return sellAll("tp2_hit") }
if pnlPct >= cfg.TP1 && pos.ExitStage == "none" { return sellPartial("tp1_hit") }
```

---

## Checklist

```
[ ] Exit priority order: SL → TP2 → TP1 → trailing → time
[ ] TP1 only executed once (gated on ExitStage == "none")
[ ] TIME exit is enforced (never disabled silently)
[ ] All thresholds loaded from config — not hardcoded
[ ] Cohort-adaptive overrides applied from config/cohorts
[ ] Price polling uses local cache — not RPC per position
[ ] PeakPrice is tracked and updated on each poll cycle
[ ] PositionStateDTO emitted on every state change
[ ] CausationID = ExecutionResultDTO.EventID
[ ] PositionID is content-addressable
[ ] Module has zero DB writes — emits DTOs only
[ ] Learning engine receives position close events (via event bus)
```

---

## Phase 9 Notes (Profitability Restoration)

Per `docs/implementation_roadmap.md` § 9.5, Phase 9 closes **GAP-02** by wiring a real
`PriceClient` into `RunPositionPoll`. Without this, every TP/SL/Trail rule in this skill
is dead code — positions exit only on `max_hold_seconds` time expiry. **This is the
single highest-impact fix in Phase 9.**

**Phase 9 mandates:**

- `cmd/server.go` MUST call `rpc.NewPriceClientForChain(...)` and assert `priceClient != nil` before passing it to `workers.RunPositionPoll`
- The `priceClient == nil` early-return guard at the top of `run_position_poll.go` MUST be removed
- Per-fetch context timeout `price_fetch_timeout_ms` (default 500 ms) enforced
- Per-cycle wall budget: `max_open_positions × price_fetch_timeout_ms × 1.2` — exceeded → `system_event level=warn`, reduce poll concurrency
- Pool drained between polls (reserve = 0) → emergency `IsRug=true` exit signal → fire SL with `Reason=pool_drained`
- **Never** return a fabricated price on RPC error — either real price or skip cycle
- Native-token (ETH/BNB/SOL) USD price cached with TTL 60 s; max-stale 300 s; beyond → halt new TP/SL evaluations

**Phase 9 exit criterion:** ≥ 80 % of position exits in a 1h replay are TP/SL/Trail
triggers (not `max_hold_seconds`); realized PnL right-tail observable.

**Cross-reference:** See `.github/skills/price-feed-integration/SKILL.md` for the full
PriceClient implementation contract (interface, per-chain implementations, factory
pattern, failure handling).

---

## References

- Architecture context: `docs/architecture-context/11_position_engine.md`
- DTO spec: `docs/dto_contracts.md` § 3.10 (PositionStateDTO)
- Roadmap: `docs/implementation_roadmap.md` Phase 2.9
- Config: `config/position.yaml`, `config/cohorts.yaml`
