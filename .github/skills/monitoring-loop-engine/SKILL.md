---
name: monitoring-loop-engine
type: skill
description: >
  Price-driven continuous position polling loop for the Position Engine (Layer 9).
  Checks kill switch FIRST at every iteration, then evaluates exits in priority order.
  One position failure must not abort others. Peak price is monotonically non-decreasing.
  Use when implementing or reviewing the position monitoring loop, exit priority
  ordering, and polling cadence configuration.
---

# Monitoring Loop Engine Skill

## Purpose

Poll live prices continuously and evaluate exit conditions for all open positions.
The loop is the backbone of Layer 9 — without it, no exits fire and capital stays
locked regardless of price movement.

**Priority order per tick (non-negotiable):**

```
1. kill_switch          → abort all positions immediately
2. drawdown_tier2       → reduce / pause per drawdown-protection skill
3. EOD_flatten          → flatten before market close (if applicable)
4. max_hold_time        → time-based exit
5. stop_loss            → hard stop
6. take_profit_1        → partial TP
7. take_profit_2        → full TP
8. trailing_stop        → dynamic trail
9. update_peak_price    → monotonically update peak
```

**Polling cadence:** crypto = 1s, staleness threshold = 5s (from config).

---

## Rules

### Loop Structure

```go
// The monitoring loop is the ONLY component that evaluates position exits.
// It MUST NOT import from execution/, capital/, or probability/ directly.
// All exits are signaled via the event bus — the execution module consumes them.

type PositionMonitorConfig struct {
    PollIntervalMS   int64   // 1000 for crypto
    StalenessLimitMS int64   // 5000
}

func RunPositionMonitorLoop(
    ctx     context.Context,
    clock   Clock,  // injected clock — never call time.Now() directly
    adapter database.Adapter,
    pricer  PriceFeed,
    cfg     PositionMonitorConfig,
) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        // --- KILL SWITCH: ALWAYS FIRST ---
        killActive, err := adapter.IsKillSwitchActive(ctx)
        if err != nil {
            log.Error("kill_switch_check_failed", "err", err)
            time.Sleep(time.Duration(cfg.PollIntervalMS) * time.Millisecond)
            continue
        }
        if killActive {
            if err := flattenAllPositions(ctx, adapter); err != nil {
                log.Error("flatten_on_kill_switch_failed", "err", err)
            }
            return ErrKillSwitchActive
        }

        // --- Load open positions ---
        positions, err := adapter.GetOpenPositions(ctx)
        if err != nil {
            log.Error("get_positions_failed", "err", err)
            time.Sleep(time.Duration(cfg.PollIntervalMS) * time.Millisecond)
            continue
        }

        now := clock.Now()

        // --- Process each position independently ---
        for _, pos := range positions {
            if err := processPosition(ctx, pos, now, adapter, pricer, cfg); err != nil {
                // ONE failure must NOT abort other positions
                log.Error("position_processing_error",
                    "position_id", pos.PositionID,
                    "err", err)
                // continue to next position
            }
        }

        // --- Always sleep, even if positions is empty ---
        time.Sleep(time.Duration(cfg.PollIntervalMS) * time.Millisecond)
    }
}
```

### Per-Position Processing (Priority Order)

```go
func processPosition(
    ctx     context.Context,
    pos     contracts.PositionState,
    now     time.Time,
    adapter database.Adapter,
    pricer  PriceFeed,
    cfg     PositionMonitorConfig,
) error {
    // Check price freshness
    price, priceAge, err := pricer.GetPrice(ctx, pos.TokenAddress)
    if err != nil || priceAge > cfg.StalenessLimitMS {
        return fmt.Errorf("stale_price: age=%dms", priceAge)
    }

    // Priority 2: Drawdown tier check (from drawdown-protection skill)
    tier := adapter.GetCurrentDrawdownTier(ctx)
    if tier == DrawdownTierKill {
        return emitExitSignal(ctx, adapter, pos, "drawdown_kill_switch")
    }

    // Priority 3: EOD flatten (if market has a close — most crypto markets don't,
    // but configurable for TradFi markets)
    if shouldFlattenEOD(now, pos, cfg) {
        return emitExitSignal(ctx, adapter, pos, "eod_flatten")
    }

    // Priority 4: Max hold time
    holdMS := now.Sub(pos.EntryTime).Milliseconds()
    if holdMS > pos.MaxHoldMS {
        return emitExitSignal(ctx, adapter, pos, "max_hold_time")
    }

    // Priority 5: Stop loss
    if price <= pos.StopLossPrice {
        return emitExitSignal(ctx, adapter, pos, "stop_loss")
    }

    // Priority 6: Take profit 1 (partial)
    if !pos.TP1Hit && price >= pos.TakeProfit1Price {
        if err := emitExitSignal(ctx, adapter, pos, "take_profit_1"); err != nil {
            return err
        }
        adapter.MarkTP1Hit(ctx, pos.PositionID)
        return nil
    }

    // Priority 7: Take profit 2 (full exit)
    if pos.TP1Hit && price >= pos.TakeProfit2Price {
        return emitExitSignal(ctx, adapter, pos, "take_profit_2")
    }

    // Priority 8: Trailing stop
    if pos.TrailingStopEnabled && price <= pos.PeakPrice*(1-pos.TrailingStopPct) {
        return emitExitSignal(ctx, adapter, pos, "trailing_stop")
    }

    // Priority 9: Update peak price (monotonically non-decreasing)
    if price > pos.PeakPrice {
        adapter.UpdatePeakPrice(ctx, pos.PositionID, price)
    }

    return nil
}
```

### Peak Price Update (Monotonic Invariant)

```go
// peak_price MUST NEVER decrease.
// The adapter enforces this at the SQL level (UPDATE ... SET peak_price = MAX(peak_price, $1)).

// PeakPrice invariant:
//   new_peak = max(current_peak, current_price)
// This is enforced in the adapter, not here, but monitored from this loop.

func UpdatePeakPriceMonotonic(
    ctx        context.Context,
    adapter    database.Adapter,
    positionID string,
    newPrice   float64,
    oldPeak    float64,
) {
    if newPrice > oldPeak {
        adapter.UpdatePeakPrice(ctx, positionID, newPrice)
    }
    // Never call UpdatePeakPrice with a value <= oldPeak
}
```

### Exit Signal Emission (NOT Direct Execution)

```go
// The monitoring loop NEVER executes trades directly.
// It emits "position.exit_signal" events — the execution module consumes them.

func emitExitSignal(
    ctx     context.Context,
    adapter database.Adapter,
    pos     contracts.PositionState,
    reason  string,
) error {
    return adapter.EmitSystemEvent(ctx, "position.exit_signal", map[string]any{
        "position_id":   pos.PositionID,
        "token_address": pos.TokenAddress,
        "reason":        reason,
        "exit_price":    0,  // pricer fills this in execution module
        "version_id":    pos.VersionID,
        "trace_id":      pos.TraceID,
    })
}
```

---

## Anti-Patterns

```
❌ Calling time.Now() directly inside the loop (inject clock, use clock.Now())
❌ Letting one position error abort the entire loop or other positions
❌ Not checking kill switch on every iteration (add dedicated heartbeat check)
❌ Calling execution module directly from the monitor loop (emit events only)
❌ Decreasing peak_price ever (monotonic invariant — use max())
❌ Skipping the sleep when position list is empty (burns CPU and RPC quota)
❌ Evaluating trailing stop BEFORE stop-loss (wrong priority order)
```

---

## Config Reference (`shared/config/pipeline.yaml`)

```yaml
position_monitor:
  poll_interval_ms: 1000 # crypto: every 1 second
  staleness_limit_ms: 5000 # reject prices older than 5s
  eod_flatten_enabled: false # crypto markets run 24/7
```

---

## Checklist

- [ ] Kill switch checked FIRST before any position processing
- [ ] Each position wrapped in independent error recovery (one failure ≠ loop abort)
- [ ] Sleep called even when position list is empty
- [ ] Peak price update uses max() — never decreases
- [ ] Exit signals emitted to event bus (never direct execution call)
- [ ] Priority order: kill → drawdown → EOD → max_hold → SL → TP1 → TP2 → trail → peak
- [ ] Clock injected (not `time.Now()` directly)

---

## References

- `docs/reference/architecture.md` § 3.9 — Position Engine (Layer 9)
- `docs/archive/architecture-context/11_position_engine.md` — TP/SL/trailing patterns
- `.github/skills/position-management/SKILL.md` — TP/SL exit configuration
- `.github/skills/drawdown-protection/SKILL.md` — Drawdown tier integration
- `shared/contracts/position.go` — `PositionState` DTO fields
