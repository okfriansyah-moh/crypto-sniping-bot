---
name: loss-pattern-analyzer
type: skill
description: >
  Categorize every losing trade into one of 7 root cause buckets and detect
  systemic loss patterns. Use when implementing or reviewing the LearningRecord
  production pipeline (Layer 10). Loss classification enables targeted corrective
  feedback to the right engine rather than blind threshold tightening.
---

# Loss Pattern Analyzer Skill

## Purpose

Every loss must be classified so the Learning Engine can route corrective feedback
to the correct upstream component. Blind parameter tightening without root cause
analysis leads to oscillation (see `docs/architecture.md` § 0.5.3).

**7 root cause categories — every loss gets exactly one primary category:**

```
1. data_quality     → DQ engine missed a rug/honeypot/manipulation
2. probability_error → P(success) model was overconfident
3. slippage_overshoot → actual slippage >> estimated slippage
4. execution_failure → tx failed, wallet issue, RPC timeout
5. regime_mismatch  → token entered wrong phase (post-pump, distribution)
6. latency_impact   → entry too late — EdgeDecayFactor killed expected return
7. black_swan       → uncategorizable external event
```

**Systemic pattern trigger:** If >60% of losses in a 30-day window share the same
root cause, it is a systemic issue → emit `system_event` and alert operator.

---

## Rules

### Classification Scoring

```go
// Each category gets a probability score [0,1].
// Primary category = argmax score.
// "unknown" is used ONLY when all scores < 0.1 — and triggers an alert.

type LossCategory string

const (
    LossCatDataQuality      LossCategory = "data_quality"
    LossCatProbabilityError LossCategory = "probability_error"
    LossCatSlippageOvershoot LossCategory = "slippage_overshoot"
    LossCatExecutionFailure LossCategory = "execution_failure"
    LossCatRegimeMismatch   LossCategory = "regime_mismatch"
    LossCatLatencyImpact    LossCategory = "latency_impact"
    LossCatBlackSwan        LossCategory = "black_swan"
    LossCatUnknown          LossCategory = "unknown"
)

type LossClassification struct {
    PrimaryCategory LossCategory
    Scores          map[LossCategory]float64
    SystemicAlert   bool   // >60% same category in rolling window
    VersionID       string // for segmented learning — mandatory
}

// ClassifyLoss is a pure function.
func ClassifyLoss(
    trade   contracts.LearningRecordDTO,
    context LossContext,
) LossClassification {
    scores := make(map[LossCategory]float64)

    // 1. DQ category: DQ flags present at entry time?
    if len(context.DQFlagsAtEntry) > 0 || context.DQRiskScore > 0.6 {
        scores[LossCatDataQuality] = math.Min(1.0, context.DQRiskScore+0.3*float64(len(context.DQFlagsAtEntry)))
    }

    // 2. Probability error: actual return << predicted return
    if trade.PredictedReturn > 0 && trade.ActualReturn < -0.01 {
        error := math.Abs(trade.PredictedReturn - trade.ActualReturn)
        scores[LossCatProbabilityError] = math.Min(1.0, error/trade.PredictedReturn)
    }

    // 3. Slippage overshoot: actual slippage >> estimated
    if context.EstimatedSlippageBPS > 0 {
        slippageRatio := context.ActualSlippageBPS / context.EstimatedSlippageBPS
        if slippageRatio > 2.0 {
            scores[LossCatSlippageOvershoot] = math.Min(1.0, (slippageRatio-2.0)/3.0+0.5)
        }
    }

    // 4. Execution failure: tx status
    if context.TxStatus == "failed" || context.TxStatus == "reverted" {
        scores[LossCatExecutionFailure] = 0.9
    }

    // 5. Regime mismatch: token age + phase signals
    if context.TokenAgeAtEntryMS > 600_000 { // older than 10 minutes
        scores[LossCatRegimeMismatch] = math.Min(1.0, float64(context.TokenAgeAtEntryMS)/3_600_000)
    }

    // 6. Latency impact: EdgeDecayFactor was low at execution
    if context.EdgeDecayFactor < 0.5 {
        scores[LossCatLatencyImpact] = 1.0 - context.EdgeDecayFactor
    }

    // Find primary category
    primary := LossCatBlackSwan
    maxScore := 0.0
    for cat, score := range scores {
        if score > maxScore {
            maxScore = score
            primary = cat
        }
    }
    if maxScore < 0.1 {
        primary = LossCatUnknown
    }

    return LossClassification{
        PrimaryCategory: primary,
        Scores:          scores,
        VersionID:       trade.VersionID,
    }
}
```

### Systemic Pattern Detection

```go
// Run over a rolling 30-day window of LossClassification records.
type PatternResult struct {
    Systemic          bool
    DominantCategory  LossCategory
    ConcentrationPct  float64 // e.g., 0.72 = 72% same cause
    WindowDays        int
    TotalLosses       int
    Alert             string
}

func DetectLossPatterns(
    classifications []LossClassification,
    windowDays       int,  // from config, default: 30
    systemicThreshold float64, // from config, default: 0.60
) PatternResult {
    if len(classifications) < 5 {
        return PatternResult{Systemic: false}
    }

    counts := make(map[LossCategory]int)
    for _, c := range classifications {
        counts[c.PrimaryCategory]++
    }

    total := len(classifications)
    dominant := LossCatUnknown
    maxCount := 0
    for cat, cnt := range counts {
        if cnt > maxCount {
            maxCount = cnt
            dominant = cat
        }
    }

    concentration := float64(maxCount) / float64(total)
    systemic := concentration >= systemicThreshold && dominant != LossCatUnknown

    alert := ""
    if systemic {
        alert = fmt.Sprintf("systemic_loss_pattern: %s at %.1f%% concentration over %d losses",
            dominant, concentration*100, total)
    }

    return PatternResult{
        Systemic:         systemic,
        DominantCategory: dominant,
        ConcentrationPct: concentration,
        WindowDays:       windowDays,
        TotalLosses:      total,
        Alert:            alert,
    }
}
```

### Feedback Routing Table

```go
// Each category routes corrective feedback to a specific engine.
// The Learning Engine uses this routing to know WHAT to update.
var FeedbackTargets = map[LossCategory]string{
    LossCatDataQuality:       "data_quality_thresholds",   // tighten DQ thresholds
    LossCatProbabilityError:  "probability_model_weights",  // recalibrate P model
    LossCatSlippageOvershoot: "slippage_estimate_model",    // update slippage model
    LossCatExecutionFailure:  "rpc_endpoint_health",        // trigger RPC check
    LossCatRegimeMismatch:    "edge_time_window",           // shrink pool age gate
    LossCatLatencyImpact:     "latency_decay_factor",       // adjust decay curve
    LossCatBlackSwan:         "strategy_version_review",    // human review
    LossCatUnknown:           "alert_operator",             // always alert
}
```

### LearningRecord Integration

```go
// ClassifyLoss runs inside the learning_recorder_worker.
// Classification result must be stored in LearningRecordDTO.
// The VersionID on the record is MANDATORY — enables per-version pattern analysis.

// Store pattern result in system_event if systemic:
func EmitSystemicLossAlert(ctx context.Context, adapter database.Adapter,
    result PatternResult, versionID string) error {
    if !result.Systemic {
        return nil
    }
    return adapter.EmitSystemEvent(ctx, "systemic_loss_pattern_detected", map[string]any{
        "dominant_category": result.DominantCategory,
        "concentration_pct": result.ConcentrationPct,
        "total_losses":      result.TotalLosses,
        "version_id":        versionID,
        "alert":             result.Alert,
    })
}
```

---

## Anti-Patterns

```
❌ Assigning multiple primary categories to one trade
❌ Using LossCatUnknown without emitting an operator alert
❌ Running pattern detection on <5 samples (too noisy)
❌ Omitting VersionID from LossClassification records
❌ Routing ALL losses to "tighten thresholds" without category analysis
❌ Deleting classified records after processing — they are the learning dataset
```

---

## Config Reference (`config/pipeline.yaml`)

```yaml
loss_pattern:
  window_days: 30
  systemic_threshold: 0.60 # >60% same category = systemic alert
  min_samples_for_detection: 5
```

---

## Checklist

- [ ] Every loss gets exactly one primary category
- [ ] LossCatUnknown always triggers an operator alert
- [ ] VersionID always present on LossClassification
- [ ] Systemic pattern runs over 30-day rolling window
- [ ] Feedback routing table drives Learning Engine updates (not manual tightening)
- [ ] Pattern result stored as `system_event` when `Systemic=true`
- [ ] ClassifyLoss is a pure function (no DB/network calls)

---

## References

- `docs/architecture.md` § 3.10 — Learning Engine
- `docs/architecture-context/12_learning_engine.md` — FP/FN computation
- `.github/skills/learning-engine/SKILL.md` — Bounded updates, shadow trades
- `.github/skills/observability/SKILL.md` — system_event constants
- `contracts/learning_record.go` — `LearningRecordDTO` fields
