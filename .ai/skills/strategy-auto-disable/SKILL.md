# Strategy Auto-Disable Skill

## Purpose

Automatically reduce capital allocation or halt trading for a strategy version
that is losing money or operating outside acceptable parameters. This protects
against slow-burn capital erosion — strategies that don't hit a single stop-loss
but gradually underperform across many trades.

**State machine:**

```
probation (25% alloc, 30 trades) ──→ active
        ↓ trigger
    degraded (50% alloc, 24h obs)
        ↓ trigger persists
     disabled (0% alloc)
        ↓ human review
      review → re-enable (paper trade) → active
```

---

## Rules

### 5 Disable Triggers

```go
type DisableTrigger struct {
    Name     string
    Active   bool
    Critical bool
    Detail   string
}

// Trigger 1: Slippage dominance — cost eating more than 50% of edge
func CheckSlippageDominance(avgSlippageBPS, avgEdgeBPS float64) DisableTrigger {
    if avgEdgeBPS <= 0 {
        return DisableTrigger{Name: "slippage_dominance"}
    }
    frac   := avgSlippageBPS / avgEdgeBPS
    active := frac > 0.50  // config: 0.50
    return DisableTrigger{
        Name:   "slippage_dominance",
        Active: active,
        Detail: fmt.Sprintf("slippage=%.0fbps edge=%.0fbps ratio=%.2f", avgSlippageBPS, avgEdgeBPS, frac),
    }
}

// Trigger 2: Hit rate below breakeven over last 30 trades (min sample gate)
func CheckLowHitRate(winRate float64, totalTrades int, breakevenRate float64) DisableTrigger {
    if totalTrades < 30 {
        return DisableTrigger{Name: "low_hit_rate"}
    }
    active := winRate < breakevenRate
    return DisableTrigger{
        Name:   "low_hit_rate",
        Active: active,
        Detail: fmt.Sprintf("win_rate=%.2f breakeven=%.2f trades=%d", winRate, breakevenRate, totalTrades),
    }
}

// Trigger 3: Loss streak
func CheckLossStreak(consecutive int, threshold int) DisableTrigger {
    active   := consecutive >= threshold  // config: 7
    critical := consecutive >= 10
    return DisableTrigger{
        Name:     "loss_streak",
        Active:   active,
        Critical: critical,
        Detail:   fmt.Sprintf("consecutive_losses=%d", consecutive),
    }
}

// Trigger 4: Session drawdown breach — IMMEDIATE disable
func CheckSessionDrawdown(sessionPnLPct float64, threshold float64) DisableTrigger {
    active   := sessionPnLPct <= -threshold  // config: 0.02 (2%)
    critical := sessionPnLPct <= -0.03       // config: 3%
    return DisableTrigger{
        Name:     "session_drawdown",
        Active:   active,
        Critical: critical,
        Detail:   fmt.Sprintf("session_pnl=%.2f%%", sessionPnLPct*100),
    }
}

// Trigger 5: Probability calibration degradation (Brier score +0.15 vs baseline)
func CheckCalibrationDegradation(currentBrier, baselineBrier float64, minSamples, trades int) DisableTrigger {
    if trades < minSamples {
        return DisableTrigger{Name: "calibration_degradation"}
    }
    degradation := currentBrier - baselineBrier
    active   := degradation > 0.15  // config: 0.15
    critical := degradation > 0.25
    return DisableTrigger{
        Name:     "calibration_degradation",
        Active:   active,
        Critical: critical,
        Detail:   fmt.Sprintf("brier_current=%.3f brier_baseline=%.3f delta=%.3f", currentBrier, baselineBrier, degradation),
    }
}
```

### State Machine Transitions

```go
type StrategyState string

const (
    StateActive    StrategyState = "active"
    StateProbation StrategyState = "probation"
    StateDegraded  StrategyState = "degraded"
    StateDisabled  StrategyState = "disabled"
    StateReview    StrategyState = "review"
    StateReEnable  StrategyState = "re_enable"  // paper trading phase
)

// AllocationMultiplier returns the capital multiplier for a given state.
// All sizes computed by capital engine MUST be multiplied by this.
func AllocationMultiplier(state StrategyState) float64 {
    switch state {
    case StateActive:    return 1.0
    case StateProbation: return 0.25
    case StateDegraded:  return 0.50
    case StateDisabled:  return 0.0
    case StateReview:    return 0.0
    case StateReEnable:  return 0.0  // paper only — no real capital
    default:             return 0.0
    }
}

// EvaluateTriggers returns the correct next state.
// The session_drawdown trigger is always treated as critical (immediate disable).
func EvaluateTriggers(
    triggers   []DisableTrigger,
    current    StrategyState,
) StrategyState {
    activeCritical := 0
    activeNonCritical := 0
    immediateDisable := false

    for _, t := range triggers {
        if t.Active {
            if t.Critical { activeCritical++ }
            else          { activeNonCritical++ }
        }
        // Session drawdown is always immediate disable
        if t.Name == "session_drawdown" && t.Active {
            immediateDisable = true
        }
    }

    if immediateDisable || activeCritical >= 2 {
        return StateDisabled
    }
    if activeCritical >= 1 || activeNonCritical >= 2 {
        if current == StateDegraded {
            return StateDisabled  // already degraded + more triggers = disable
        }
        return StateDegraded
    }
    if activeNonCritical >= 1 && current == StateActive {
        return StateProbation
    }
    return current
}
```

### Re-Enable Gate

```go
// Strategies can only be re-enabled through human review + paper trading.
type ReEnableCheck struct {
    CoolingHoursElapsed bool   // 48h since disable
    PaperTradesComplete  bool   // 20 paper trades logged
    PaperWinRate        float64 // must be >= 0.50
    PaperPF             float64 // must be >= 1.5
    Approved            bool    // human reviewer must set this
}

func CanReEnable(check ReEnableCheck) bool {
    return check.CoolingHoursElapsed &&
        check.PaperTradesComplete &&
        check.PaperWinRate >= 0.50 &&
        check.PaperPF >= 1.5 &&
        check.Approved
}
```

### Event Emission (Mandatory on Every Transition)

```go
// Every state transition MUST emit to the event bus.
// Operators and the Telegram dispatcher consume "strategy.disabled" events.
func EmitStrategyStateChange(
    ctx        context.Context,
    adapter    database.Adapter,
    versionID  string,
    from, to   StrategyState,
    triggers   []DisableTrigger,
) error {
    activeTriggerNames := make([]string, 0)
    for _, t := range triggers {
        if t.Active { activeTriggerNames = append(activeTriggerNames, t.Name) }
    }

    eventType := "strategy.state_change"
    if to == StateDisabled {
        eventType = "strategy.disabled"  // triggers Telegram alert
    }

    return adapter.EmitSystemEvent(ctx, eventType, map[string]any{
        "version_id":       versionID,
        "from_state":       from,
        "to_state":         to,
        "active_triggers":  activeTriggerNames,
        "alloc_multiplier": AllocationMultiplier(to),
    })
}
```

---

## Anti-Patterns

```
❌ Deleting disabled strategies (must keep in DB with status="disabled" for audit)
❌ Auto-re-enabling without human review (requires operator approval)
❌ Sharing state machine state across version_id (each version has independent state)
❌ Allowing re-enable without paper trading phase (too risky)
❌ Applying multiplier AFTER capital sizing (multiplier must gate sizing, not post-adjust)
❌ Not emitting "strategy.disabled" event (Telegram operator won't be notified)
```

---

## Config Reference (`config/pipeline.yaml`)

```yaml
strategy_auto_disable:
  slippage_dominance_threshold: 0.50
  hit_rate_min_samples: 30
  loss_streak_threshold: 7
  session_drawdown_threshold: 0.02 # 2%
  calibration_brier_threshold: 0.15
  probation_alloc_multiplier: 0.25
  degraded_alloc_multiplier: 0.50
  re_enable_cooling_hours: 48
  re_enable_paper_trades: 20
  re_enable_min_win_rate: 0.50
  re_enable_min_profit_factor: 1.50
```

---

## Checklist

- [ ] Allocation multiplier applied in capital engine, not inline in auto-disable
- [ ] Session drawdown trigger always causes immediate disable (no probation)
- [ ] All state transitions emit `strategy.state_change` or `strategy.disabled` event
- [ ] Disabled strategies stored in DB with `status="disabled"` — never deleted
- [ ] Re-enable requires human approval AND passing paper trade metrics
- [ ] State machine scoped per `version_id` — not per strategy name
- [ ] `EvaluateTriggers` is a pure function (no DB/side effects)

---

## References

- `docs/architecture.md` § 3.10 — Learning Engine (Layer 10)
- `docs/architecture.md` § 4.2 — A/B Promotion (re-enable gating)
- `.github/skills/strategy-decay-detector/SKILL.md` — Feeding DecaySignals to trigger checks
- `.github/skills/strategy-versioning/SKILL.md` — VersionID scoping
- `.github/skills/drawdown-protection/SKILL.md` — Session drawdown trigger feeds from HWM
- `.github/skills/capital-sizing/SKILL.md` — Applying AllocationMultiplier