# Drawdown Protection Skill

## Purpose

Enforce capital preservation via a tiered response system anchored to a
high-water mark (HWM). When drawdown crosses a configurable kill threshold
(default 10%), all new positions are blocked and the state persists across
restarts. Recovery requires explicit operator reset via `/resume` Telegram command.

**Core invariant:** Drawdown is always computed from HWM, never from starting
capital. HWM only ever increases, never decreases.

```
Drawdown(t) = (HWM - Equity(t)) / HWM
```

**Relationship to profit invariant:**

```
Profit = Edge × Probability × Execution × Capital × DataQuality × AdaptationQuality
                                           ↑
                   Drawdown Protection preserves Capital factor
```

---

## Rules

### High-Water Mark (Monotonic)

```go
// HWM is written to the database on every equity update.
// It is never reset automatically — only manual operator reset.
func UpdateHWM(ctx context.Context, adapter database.Adapter, currentEquity float64) error {
    current, err := adapter.GetSystemMetric(ctx, "hwm_equity")
    if err != nil { return err }

    hwm, _ := strconv.ParseFloat(current, 64)
    if currentEquity > hwm {
        return adapter.SetSystemMetric(ctx, "hwm_equity",
            strconv.FormatFloat(currentEquity, 'f', 8, 64))
    }
    return nil  // HWM never decreases
}
```

### Drawdown Computation

```go
func ComputeDrawdown(hwm, equity float64) float64 {
    if hwm <= 0 {
        return 0.0
    }
    dd := (hwm - equity) / hwm
    if dd < 0 {
        return 0.0  // equity above HWM = no drawdown
    }
    return dd
}
```

### Tiered Response

```go
// All thresholds live in config/pipeline.yaml — never hardcode here.
type DrawdownTier struct {
    Threshold      float64 // e.g., 0.05
    RiskMultiplier float64 // e.g., 0.5
    NewPositions   bool    // false = block new entries
    KillSwitch     bool    // true = halt all trading immediately
}

func GetDrawdownTier(drawdown float64, tiers []DrawdownTier) DrawdownTier {
    // Tiers must be sorted ascending by Threshold.
    // Return highest tier whose Threshold ≤ drawdown.
    active := DrawdownTier{RiskMultiplier: 1.0, NewPositions: true}
    for _, t := range tiers {
        if drawdown >= t.Threshold {
            active = t
        }
    }
    return active
}

// Default tiers (config/pipeline.yaml):
//   tier1: threshold=0.05, risk_multiplier=0.5, new_positions=true,  kill=false
//   tier2: threshold=0.08, risk_multiplier=0.25, new_positions=false, kill=false
//   tier3: threshold=0.10, risk_multiplier=0.0, new_positions=false,  kill=true
```

### Kill Switch (Persistent)

```go
// Kill switch state lives in the database — survives restarts.
func IsKillSwitchActive(ctx context.Context, adapter database.Adapter) (bool, error) {
    val, err := adapter.GetSystemMetric(ctx, "kill_switch_active")
    if err != nil { return false, err }
    return val == "true", nil
}

func ActivateKillSwitch(ctx context.Context, adapter database.Adapter, reason string) error {
    if err := adapter.SetSystemMetric(ctx, "kill_switch_active", "true"); err != nil {
        return err
    }
    return adapter.SetSystemMetric(ctx, "kill_switch_reason", reason)
}

// NEVER auto-reset the kill switch.
// Reset requires explicit Telegram /resume command from operator.
func ResetKillSwitch(ctx context.Context, adapter database.Adapter, operatorID string) error {
    if err := adapter.SetSystemMetric(ctx, "kill_switch_active", "false"); err != nil {
        return err
    }
    return adapter.SetSystemMetric(ctx, "kill_switch_reset_by", operatorID)
}
```

### Daily Loss Gate

```go
// Daily loss includes both realized AND unrealized PnL.
// Computed relative to start-of-day equity (not HWM).
func CheckDailyLossLimit(
    startOfDayEquity float64,
    realizedPnL float64,
    unrealizedPnL float64,
    limitPct float64,  // from config, floor: 0.02
) (exceeded bool, pct float64) {
    totalLoss := realizedPnL + unrealizedPnL
    if totalLoss >= 0 {
        return false, 0.0
    }
    pct = math.Abs(totalLoss) / startOfDayEquity
    return pct >= limitPct, pct
}
```

### Orchestrator Integration Pattern

```go
// The orchestrator checks drawdown before every allocation event is processed.
// If kill switch is active, ALL pipeline stages are blocked — not just capital.
func (o *Orchestrator) runDrawdownGuard(ctx context.Context) error {
    active, err := IsKillSwitchActive(ctx, o.adapter)
    if err != nil { return err }
    if active {
        return ErrKillSwitchActive  // sentinel — orchestrator halts the pipeline
    }

    equity, err := o.adapter.GetCurrentEquity(ctx)
    if err != nil { return err }
    hwm, err := o.adapter.GetSystemMetricFloat(ctx, "hwm_equity")
    if err != nil { return err }

    dd := ComputeDrawdown(hwm, equity)
    tier := GetDrawdownTier(dd, o.cfg.DrawdownTiers)

    if tier.KillSwitch {
        _ = ActivateKillSwitch(ctx, o.adapter,
            fmt.Sprintf("drawdown_%.4f_exceeded_threshold_%.4f", dd, tier.Threshold))
        return ErrKillSwitchActive
    }

    // Publish system event with current drawdown state
    o.adapter.EmitSystemEvent(ctx, "drawdown_check", map[string]any{
        "drawdown_pct":     dd,
        "tier":             tier.Threshold,
        "risk_multiplier":  tier.RiskMultiplier,
        "new_positions":    tier.NewPositions,
    })
    return nil
}
```

---

## Anti-Patterns

```
❌ Computing drawdown from starting capital — use HWM only
❌ Auto-resetting the kill switch after a cooldown period
❌ Separating unrealized PnL from daily loss calculation
❌ Hardcoding thresholds (0.05, 0.10) in Go code — all thresholds in config/
❌ Resetting HWM on strategy version change — HWM is a portfolio-level concept
❌ Skipping the drawdown guard for "exploration" or "paper" trades
```

---

## Config Reference (`config/pipeline.yaml`)

```yaml
drawdown:
  kill_threshold: 0.10 # 10% from HWM → kill switch
  daily_loss_limit: 0.02 # 2% of start-of-day equity
  tiers:
    - threshold: 0.05
      risk_multiplier: 0.5
      new_positions: true
      kill: false
    - threshold: 0.08
      risk_multiplier: 0.25
      new_positions: false
      kill: false
    - threshold: 0.10
      risk_multiplier: 0.0
      new_positions: false
      kill: true
```

---

## Checklist

- [ ] HWM written to DB on every equity update, never in memory only
- [ ] Drawdown computed as `(HWM - equity) / HWM`, not `(start - equity) / start`
- [ ] Kill switch persists across restarts (stored in DB)
- [ ] Kill switch reset requires operator Telegram `/resume` + confirmation
- [ ] Daily loss includes realized + unrealized PnL
- [ ] All thresholds in `config/pipeline.yaml`, zero hardcoded values
- [ ] Orchestrator checks drawdown guard before processing any allocation event
- [ ] Drawdown tier risk multiplier applied to capital allocation weight

---

## References

- `docs/architecture.md` § 7 — Operational Modes (STRICT mode correlates with tight drawdown)
- `docs/architecture.md` § 0.2.4 — Capital factor in profit invariant
- `.github/skills/capital-sizing/SKILL.md` — AllocationDTO production, where risk_multiplier applies
- `.github/skills/operational-modes/SKILL.md` — Mode transitions on drawdown events
- `.github/skills/telegram-dispatcher/SKILL.md` — `/resume` command pattern
- `config/pipeline.yaml` — All drawdown thresholds live here