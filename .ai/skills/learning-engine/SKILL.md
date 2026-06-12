# Learning Engine Skill

## Purpose

Enforce correct implementation of the learning engine that converts trade outcomes into
controlled parameter updates. This is the only layer that changes system behavior.

**AdaptationQuality (AQ) = LearningSpeed × LearningAccuracy**

- Learning too slow → missed corrections → repeated losses
- Learning too fast → oscillation → instability
- Wrong adjustments → systematic degradation

---

## Rules

### What the Learning Engine Updates (Only These 5)

```
1. Threshold adjustments    (θ_p, θ_slippage, θ_latency, θ_ev)
2. Feature weights          (momentum, tx_rate, buy_sell, etc.)
3. Model calibration        (probability/slippage/latency coefficients)
4. Execution parameters     (concurrency, fee strategy)
5. Capital sizing rules     (cohort multipliers, exploration budget)
```

**Rule:** Never update multiple parameter families in the same cycle.
Single-family updates prevent oscillation. The learning engine decides which family
needs adjustment most urgently based on error signals.

### Mandatory Learning Safety Rules (Non-Negotiable)

```
Rule 1: Bounded updates      — Δparameter ≤ 5-10% per cycle
Rule 2: Sample-gated         — N ≥ 30 samples per cohort before update
Rule 3: Versioned            — every update creates a new StrategyVersion
Rule 4: Rollback-able        — revert to previous version if performance degrades
Rule 5: Single-family only   — only ONE parameter family updated per cycle
Rule 6: Shadow trades stored — rejected tokens tracked via shadow observer
```

### Complete Learning Record (Both Accepted AND Rejected)

```go
// CRITICAL: Must store BOTH accepted trades AND rejected candidates
// Without rejected samples → cannot compute false negatives

type LearningRecord struct {
    RecordID      string  // SHA256(content)[:16]
    TokenAddress  string

    // Decisions across all layers
    DataQualityDecision string  // "pass" | "risky-pass" | "reject"
    EdgeDetected        bool
    EdgeScore           float64
    Selected            bool
    AllocatedSizeUSD    float64

    // Feature snapshot (at decision time — frozen for learning)
    FeatureVector map[string]float64  // NOT map across boundaries — use FeatureDTO fields

    // Execution metrics
    SlippageExpected    float64
    SlippageActual      float64
    LatencyExpectedMs   int64
    LatencyActualMs     int64
    TxSuccess          bool

    // Outcome
    EntryPrice  float64
    ExitPrice   float64
    PeakPrice   float64
    PnLPct      float64
    PnLUSD      float64
    DurationSec int64
    ExitReason  string

    // Labels (set after outcome observed)
    OutcomeLabel string  // "success" | "fail" | "rug" | "missed"
    IsFalsePositive bool  // accepted → loss/rug
    IsFalseNegative bool  // rejected → later pump (shadow tracked)

    // Meta
    CohortID         string
    StrategyVersionID string
    RecordedAt       string  // ISO 8601
}
```

### False Negative Computation (Requires Shadow Observer)

```go
// Shadow observer: periodic job that checks rejected tokens' price action
// After T minutes (config.shadow_observation_window_min), check if token pumped
func computeFalseNegative(rejected LearningRecord, observedPricePct float64, cfg LearningConfig) bool {
    // If rejected token gained more than FN threshold → we missed a real edge
    return observedPricePct >= cfg.FalseNegativeThresholdPct  // e.g., +30%
}
```

**Rule:** Without shadow trade tracking, FN rate is unmeasurable → system cannot
distinguish "we correctly rejected" from "we missed a profitable trade."

### Cohort Analysis (Core Learning Unit)

```go
// Group outcomes by cohort — compute expectancy per group
type CohortStats struct {
    CohortID        string
    WinRate         float64  // trades with positive PnL / total trades
    ExpectancyPct   float64  // mean(PnL%) weighted by win/loss
    AvgPnLPct       float64
    FalsePositiveRate float64
    FalseNegativeRate float64
    SampleCount     int
    UpdatedAt       string
}

// Cohorts are defined by feature bands: liquidity × tax × entropy
// e.g., "small_liquidity_low_tax_high_entropy" → CohortID
```

### Bounded Update Logic

```go
// Apply bounded update to a parameter
func boundedUpdate(current, proposed, maxDeltaPct float64) float64 {
    delta := proposed - current
    maxDelta := current * maxDeltaPct / 100  // e.g., 5% of current value
    delta = clamp(delta, -maxDelta, maxDelta)
    return current + delta
}

// Example: update probability threshold
func updateProbabilityThreshold(
    current float64,
    cohortStats []CohortStats,
    cfg LearningConfig,
) float64 {
    if totalSamples(cohortStats) < cfg.MinSamplesBeforeUpdate { return current }  // sample gate

    // Compute signal: are we passing too much (FP↑) or too little (FN↑)?
    fpRate := avgFalsePositiveRate(cohortStats)
    fnRate := avgFalseNegativeRate(cohortStats)

    if fpRate > cfg.FPAlertThreshold {
        return boundedUpdate(current, current*1.05, cfg.MaxDeltaPct)  // tighten threshold
    }
    if fnRate > cfg.FNAlertThreshold {
        return boundedUpdate(current, current*0.95, cfg.MaxDeltaPct)  // relax threshold
    }
    return current
}
```

### Version Creation on Every Update

```go
// Every parameter change creates an immutable StrategyVersion
func applyUpdate(adapter database.Adapter, updates ParameterUpdates, current StrategyVersion) StrategyVersion {
    newVersion := StrategyVersion{
        VersionID:      SHA256(canonical_json(updates) + current.VersionID)[:16],
        ParentVersion:  current.VersionID,
        Thresholds:     applyThresholdUpdates(current.Thresholds, updates.Thresholds),
        FeatureWeights: applyWeightUpdates(current.FeatureWeights, updates.FeatureWeights),
        CreatedAt:      ISO8601UTC,
    }
    adapter.SaveStrategyVersion(ctx, newVersion)
    return newVersion
}
```

### Rollback on Performance Degradation

```go
// A/B promotion gate — only promote if:
//   expectancy(new) > expectancy(old) × 1.05  (5% improvement)
//   drawdown(new) ≤ drawdown(old)
//   N ≥ 30-50 samples
func shouldPromote(newStats, oldStats VersionStats, cfg LearningConfig) bool {
    return newStats.Expectancy > oldStats.Expectancy*cfg.PromotionMinImprovement &&
        newStats.Drawdown <= oldStats.Drawdown &&
        newStats.SampleCount >= cfg.MinSamplesForPromotion
}
```

### LearningRecord Production

```go
// Produce LearningRecord on position close (and for rejected tokens via shadow observer)
LearningRecord{
    RecordID:      SHA256(token + version + entry_time)[:16],
    TokenAddress:  pos.TokenAddress,
    // ... all fields above
    TraceID:       pos.TraceID,
    CorrelationID: pos.CorrelationID,
    CausationID:   pos.EventID,
    VersionID:     pos.VersionID,
    RecordedAt:    ISO8601UTC,
}
```

### Anti-Patterns

```go
// ❌ Updating multiple parameter families in one cycle
updateThresholds(...)
updateWeights(...)   // Wrong — one family per cycle

// ❌ Updating without sample gate
updateParam(newValue)  // Wrong — require N ≥ 30

// ❌ Unbounded update
threshold += 0.2  // Wrong — must be bounded (≤ 5-10% of current)

// ❌ Not storing rejected trades
// learning only on accepted trades → cannot compute FN rate

// ❌ Promoting without performance gate
activeVersion = newVersion  // Wrong — check expectancy improvement AND drawdown

// ✅ Correct
if samples >= minSamples {
    newThreshold := boundedUpdate(current, proposed, maxDeltaPct)
    newVersion := applyUpdate(adapter, ParameterUpdates{Thresholds: newThreshold}, current)
    if shouldPromote(newVersionStats, currentStats, cfg) {
        adapter.ActivateVersion(ctx, newVersion.VersionID)
    }
}
```

### Anti-Patterns

```go
// ❌ Updating multiple parameter families in one cycle
updateThresholds(...)
updateWeights(...)   // Wrong — one family per cycle

// ❌ Updating without sample gate
updateParam(newValue)  // Wrong — require N ≥ 30

// ❌ Unbounded update
threshold += 0.2  // Wrong — must be bounded (≤ 5-10% of current)

// ❌ Not storing rejected trades
// learning only on accepted trades → cannot compute FN rate

// ❌ Promoting without performance gate
activeVersion = newVersion  // Wrong — check expectancy improvement AND drawdown

// ✅ Correct
if samples >= minSamples {
    newThreshold := boundedUpdate(current, proposed, maxDeltaPct)
    newVersion := applyUpdate(adapter, ParameterUpdates{Thresholds: newThreshold}, current)
    if shouldPromote(newVersionStats, currentStats, cfg) {
        adapter.ActivateVersion(ctx, newVersion.VersionID)
    }
}
```

---

### Overfit Guard Integration

Before any weight update, the Learning Engine MUST run the overfit audit.
If the audit fails, the update is blocked — not deferred, blocked.

```go
// Call at start of every learning cycle that modifies indicator weights.
audit := overfit.RunOverfitAudit(currentIndicators, m.sampleCount)
if !audit.Valid {
    adapter.EmitSystemEvent(ctx, "learning_update_blocked", map[string]any{
        "reason":  "overfit_audit_failed",
        "issues":  audit.Issues,
        "version": currentVersion.VersionID,
    })
    return  // do NOT apply the update
}
// Also apply confidence decay if in cold start
if audit.SampleStatus == overfit.SampleColdStart {
    proposedWeights = scaleWeights(proposedWeights, audit.ConfidenceDecay)
}
```

> See `.github/skills/overfit-detector/SKILL.md` for `RunOverfitAudit()`
> and `SampleSizeStatus` definitions.

---

### Loss Classification Integration

The learning recorder worker calls `ClassifyLoss()` on every realized loss
and stores the category in `LearningRecord.LossCategory`. This category
determines WHICH engine receives the feedback signal.

```go
// In the learning recorder worker (orchestrator-managed):
if record.OutcomePnLPct < 0 {
    category := loss_pattern.ClassifyLoss(loss_pattern.LossInput{
        PnLPct:              record.OutcomePnLPct,
        EstimatedSlippageBPS: record.EstimatedSlippageBPS,
        ActualSlippageBPS:   record.ActualSlippageBPS,
        DataQualityScore:    record.DataQualityScore,
        ExecutionStatus:     record.ExecutionStatus,
        HoldDurationMS:      record.HoldDurationMS,
        MaxHoldMS:           record.MaxHoldMS,
    })
    record.LossCategory = string(category.PrimaryCategory)
}
// FeedbackTargets tells which engine should adjust:
//   data_quality → DataQuality module thresholds
//   slippage_overshoot → SlippageModel recalibration
//   execution_failure → ExecutionEngine concurrency + RPC rotation
//   probability_error → ProbabilityModel weight update
//   regime_mismatch → operational mode adjustment
```

> See `.github/skills/loss-pattern-analyzer/SKILL.md` for `ClassifyLoss()`
> implementation and the 7-category taxonomy.

---

### Decay Detection Integration

The Learning Engine runs `ComputeDecayScore()` periodically (every N evaluations,
from config) per active strategy version. On decay detection, it emits the event
and triggers the appropriate action.

```go
// Periodic decay check in learning worker:
signals := []strategy_decay.DecaySignal{
    strategy_decay.DetectWinRateDecay(stats.WinRate, baseline.WinRate, cfg.MinSamplesWinRate, stats.TotalTrades, cfg.WinRateDeclineThreshold),
    strategy_decay.DetectProfitFactorDecay(stats.ProfitFactor, baseline.ProfitFactor, cfg.MinSamplesPF, stats.TotalTrades),
    strategy_decay.DetectEdgeCaptureDegradation(stats.AvgSlippageBPS, stats.AvgEdgeBPS, cfg.SlippageDominanceThreshold),
    strategy_decay.DetectLossStreak(stats.ConsecutiveLosses, cfg.LossStreakThreshold),
    strategy_decay.DetectHoldingPeriodDrift(stats.AvgActualHoldMS, stats.AvgExpectedHoldMS, cfg.HoldingDriftMultiplier),
}
assessment := strategy_decay.ComputeDecayScore(signals, currentVersion.VersionID)
action := strategy_decay.GetDecayAction(assessment)
if action != strategy_decay.DecayActionNone {
    strategy_decay.EmitDecayEvent(ctx, adapter, assessment, action)
}
```

> See `.github/skills/strategy-decay-detector/SKILL.md` for the full 5-metric
> implementation and `ComputeDecayScore()` aggregation logic.

---

## Checklist

```
[ ] Overfit audit runs before every weight update (blocks if fails)
[ ] Loss category stored in LearningRecord.LossCategory for all realized losses
[ ] Decay detection runs periodically per version_id (config-driven interval)
[ ] Δparameter ≤ 5-10% per cycle (bounded updates)
[ ] N ≥ 30 samples before any threshold update
[ ] Only ONE parameter family updated per cycle
[ ] Every update creates a new StrategyVersion (immutable snapshot)
[ ] Shadow trades stored for rejected tokens
[ ] False negatives computed via shadow observer
[ ] Cohort analysis groups by: liquidity × tax × entropy bands
[ ] Rollback: promote only if expectancy +5% AND drawdown ≤ current
[ ] LearningRecord carries StrategyVersionID for attribution
[ ] Rollback = switch version pointer, never modify config directly
[ ] Module has zero direct DB writes — goes via adapter only (orchestrator routes)
```

---

## References

- Architecture context: `docs/architecture-context/12_learning_engine.md`
- Architecture: `docs/architecture.md` § 3.10 (Learning Engine)
- DTO spec: `docs/dto_contracts.md` § 3.11 (LearningRecord, EvaluationDTO)
- Roadmap: `docs/implementation_roadmap.md` Phase 5
- Config: `config/learning.yaml`
- `.github/skills/overfit-detector/SKILL.md` — Overfit audit gate before weight updates
- `.github/skills/loss-pattern-analyzer/SKILL.md` — Loss classification (7 categories)
- `.github/skills/strategy-decay-detector/SKILL.md` — 5-metric decay detection