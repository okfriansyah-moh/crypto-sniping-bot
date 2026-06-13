---
name: strategy-decay-detector
type: skill
description: >
  Quantified strategy performance decay detection across 5 metrics with
  composite scoring and auto-disable thresholds. Use when implementing or
  reviewing the Learning Engine's strategy health monitoring (Layer 10).
  Requires minimum samples before triggering. Disabled strategies are never
  deleted — they are flagged for human review and version rollback.
---

# Strategy Decay Detector Skill

## Purpose

Detect when a deployed strategy version is degrading in performance before
catastrophic losses accumulate. Five metrics are tracked, each with a `baseline`
(first 30–50 trades after version activation) and `current` (rolling window).

**Decay score aggregation:**

```
DecayScore = Σ weights[i] × normalized_signal[i]
Range: [0.0, 1.0]. Higher = worse decay.
```

**Auto-disable thresholds (see config):**

- 2 critical signals → immediate disable
- 1 critical + 3 active → immediate disable
- 3+ active → reduce weight to 50%

---

## Rules

### 5 Decay Metrics

```go
type DecaySignal struct {
    Name        string
    Active      bool    // currently triggered
    Critical    bool    // exceeds critical threshold
    Value       float64 // current measured value
    Baseline    float64 // baseline value at activation
    Description string
}

// Metric 1: Win Rate Decay
func DetectWinRateDecay(
    currentWinRate float64,
    baselineWinRate float64,
    minSamples      int,
    totalTrades     int,
    declineThreshold float64, // config: 0.10 (10% relative decline)
) DecaySignal {
    if totalTrades < minSamples {
        return DecaySignal{Name: "win_rate", Active: false}
    }
    decline := baselineWinRate - currentWinRate
    active   := decline >= declineThreshold
    critical := currentWinRate < 0.35  // config: breakeven adjusted for fees
    return DecaySignal{
        Name:        "win_rate",
        Active:      active || critical,
        Critical:    critical,
        Value:       currentWinRate,
        Baseline:    baselineWinRate,
        Description: fmt.Sprintf("win_rate_%.2f_baseline_%.2f", currentWinRate, baselineWinRate),
    }
}

// Metric 2: Profit Factor Decay
func DetectProfitFactorDecay(
    currentPF  float64,
    baselinePF float64,
    minSamples int,
    totalTrades int,
) DecaySignal {
    if totalTrades < minSamples {
        return DecaySignal{Name: "profit_factor", Active: false}
    }
    active   := currentPF < 1.0 || currentPF < baselinePF*0.7
    critical := currentPF < 0.8  // losing money reliably
    return DecaySignal{
        Name:        "profit_factor",
        Active:      active || critical,
        Critical:    critical,
        Value:       currentPF,
        Baseline:    baselinePF,
    }
}

// Metric 3: Slippage Dominance (cost vs edge)
func DetectEdgeCaptureDegradation(
    avgSlippageBPS float64,
    avgEdgeBPS     float64,
    threshold      float64, // config: 0.60 (slippage > 60% of edge)
) DecaySignal {
    if avgEdgeBPS <= 0 {
        return DecaySignal{Name: "edge_capture", Active: false}
    }
    slippageFraction := avgSlippageBPS / avgEdgeBPS
    active   := slippageFraction > threshold
    critical := slippageFraction > 0.80  // slippage eating 80%+ of edge
    return DecaySignal{
        Name:        "edge_capture",
        Active:      active || critical,
        Critical:    critical,
        Value:       slippageFraction,
        Baseline:    threshold,
        Description: fmt.Sprintf("slippage_%.0fbps_vs_edge_%.0fbps", avgSlippageBPS, avgEdgeBPS),
    }
}

// Metric 4: Loss Streak
func DetectLossStreak(
    consecutiveLosses int,
    streakThreshold   int, // config: 7
) DecaySignal {
    active   := consecutiveLosses >= streakThreshold
    critical := consecutiveLosses >= 10
    return DecaySignal{
        Name:        "loss_streak",
        Active:      active || critical,
        Critical:    critical,
        Value:       float64(consecutiveLosses),
        Baseline:    0,
    }
}

// Metric 5: Holding Period Drift (time exit triggered more often than expected)
func DetectHoldingPeriodDrift(
    avgActualHoldMS   int64,
    avgExpectedHoldMS int64,
    driftMultiplier   float64, // config: 2.0
) DecaySignal {
    if avgExpectedHoldMS <= 0 {
        return DecaySignal{Name: "holding_period_drift", Active: false}
    }
    ratio  := float64(avgActualHoldMS) / float64(avgExpectedHoldMS)
    active := ratio > driftMultiplier
    critical := ratio > 4.0  // holding 4× longer than expected
    return DecaySignal{
        Name:        "holding_period_drift",
        Active:      active || critical,
        Critical:    critical,
        Value:       ratio,
        Baseline:    1.0,
    }
}
```

### Composite Decay Score

```go
type DecayAssessment struct {
    DecayScore      float64  // [0,1]: higher = worse
    ActiveSignals   int
    CriticalSignals int
    Status          string   // "healthy" | "degraded" | "disabled"
    Signals         []DecaySignal
    VersionID       string   // MANDATORY for versioned tracking
}

func ComputeDecayScore(signals []DecaySignal, versionID string) DecayAssessment {
    // Weights: loss_streak and profit_factor are most critical
    weights := map[string]float64{
        "win_rate":             0.20,
        "profit_factor":        0.25,
        "edge_capture":         0.25,
        "loss_streak":          0.20,
        "holding_period_drift": 0.10,
    }

    activeCount, criticalCount := 0, 0
    var weightedScore float64
    for _, s := range signals {
        if s.Active   { activeCount++ }
        if s.Critical { criticalCount++ }
        w := weights[s.Name]
        if s.Critical {
            weightedScore += w * 1.0
        } else if s.Active {
            weightedScore += w * 0.5
        }
    }

    // Auto-disable conditions (config-driven, overridable in config/)
    status := "healthy"
    if criticalCount >= 2 || (criticalCount >= 1 && activeCount >= 3) {
        status = "disabled"
    } else if activeCount >= 3 || weightedScore > 0.4 {
        status = "degraded"
    }

    return DecayAssessment{
        DecayScore:      weightedScore,
        ActiveSignals:   activeCount,
        CriticalSignals: criticalCount,
        Status:          status,
        Signals:         signals,
        VersionID:       versionID,
    }
}
```

### Action on Decay

```go
// DecayAction maps status to what the Learning Engine does.
type DecayAction string

const (
    DecayActionNone          DecayAction = "none"
    DecayActionReduceWeight  DecayAction = "reduce_weight"   // degraded → 50% alloc
    DecayActionDisable       DecayAction = "disable"          // disabled → 0% alloc
    DecayActionRollback      DecayAction = "rollback"         // trigger version rollback
)

func GetDecayAction(assessment DecayAssessment) DecayAction {
    switch assessment.Status {
    case "disabled":
        return DecayActionDisable
    case "degraded":
        return DecayActionReduceWeight
    default:
        return DecayActionNone
    }
}

// EmitDecayEvent publishes to the event bus for downstream consumption.
// strategy-auto-disable and strategy-versioning skills consume this.
func EmitDecayEvent(
    ctx        context.Context,
    adapter    database.Adapter,
    assessment DecayAssessment,
    action     DecayAction,
) error {
    return adapter.EmitSystemEvent(ctx, "strategy_decay_detected", map[string]any{
        "version_id":       assessment.VersionID,
        "decay_score":      assessment.DecayScore,
        "active_signals":   assessment.ActiveSignals,
        "critical_signals": assessment.CriticalSignals,
        "status":           assessment.Status,
        "action":           action,
    })
}
```

---

## Anti-Patterns

```
❌ Triggering disable on a single data point (require min samples per metric)
❌ Deleting disabled strategy versions (flag for review — never delete)
❌ Sharing decay baselines across version_id (each version has its own baseline)
❌ Auto-rollback without emitting a system_event for operator awareness
❌ Applying decay weights in real-time per-trade (run in periodic batch, not inline)
❌ Using win rate as the sole decay signal (need all 5 for stability)
```

---

## Config Reference (`config/pipeline.yaml`)

```yaml
strategy_decay:
  min_samples_win_rate: 30
  min_samples_profit_factor: 50
  win_rate_decline_threshold: 0.10
  critical_win_rate: 0.35
  profit_factor_critical: 0.80
  slippage_dominance_threshold: 0.60
  loss_streak_threshold: 7
  holding_period_drift_multiplier: 2.0
  disable_on_critical_signals: 2
  reduce_weight_on_active_signals: 3
```

---

## Checklist

- [ ] Each metric checks minimum sample count before activating
- [ ] Decay tracked per `(strategy_version_id, market)` — not global
- [ ] Disabled strategies flagged in DB, never deleted
- [ ] Decay event emitted to event bus when status changes
- [ ] Action (reduce_weight, disable, rollback) driven by config thresholds
- [ ] Baseline established in first N trades after version activation
- [ ] ComputeDecayScore is a pure function (no DB calls, no side effects)

---

## References

- `docs/reference/architecture.md` § 3.10 — Learning Engine (Layer 10)
- `docs/reference/architecture.md` § 4.1-4.2 — Strategy Versioning & Replay
- `.github/skills/learning-engine/SKILL.md` — Bounded updates and sample gates
- `.github/skills/strategy-versioning/SKILL.md` — Version rollback mechanics
- `.github/skills/strategy-auto-disable/SKILL.md` — Lifecycle state machine
- `contracts/evaluation.go` — `EvaluationDTO` fields
