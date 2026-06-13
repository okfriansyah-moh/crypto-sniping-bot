---
name: profit-first
type: skill
description: >
  Profit-first design philosophy and decision framework. Use when designing new
  features, modifying pipeline stages, tuning thresholds, or evaluating any change.
  Every modification must preserve all six profit factors. Features that don't
  improve profit are not features — they are complexity costs.
---

# Profit-First Skill

## Purpose

Apply the profit-first design lens to every implementation decision. The core
invariant is not a slogan — it is a mathematical constraint that governs every
architectural and implementation choice.

**Core Invariant:**
$$\text{Profit} = \text{Edge} \times \text{Probability} \times \text{Execution} \times \text{Capital} \times \text{DataQuality} \times \text{AdaptationQuality}$$

**If any factor approaches zero, profit approaches zero.**

---

## Rules

### The Six Factors (Definitions and Owners)

```
Factor              Layer       What it measures
─────────────────────────────────────────────────────────────────────
Edge                Layer 3     Statistical advantage over random entry
                                Measured by: edge_score, expectancy vs baseline
Probability         Layer 4     P(success | edge, features)
                                Measured by: win_rate, calibration error
Execution           Layer 8     How much of edge survives to the trade
                                Measured by: slippage_error, latency, inclusion_rate
Capital             Layer 7     Correct bet sizing (Kelly-adjacent)
                                Measured by: cohort multiplier accuracy, allocation_error
DataQuality         Layer 1     Fraction of real opportunities vs scams
                                Measured by: fp_rate, fn_rate, rug_rate
AdaptationQuality   Layer 10    Rate of system self-improvement
                                Measured by: error_decay_rate, calibration_improvement
```

### Design Gate: Does This Feature Improve At Least One Factor?

```
Before implementing ANY change, answer:
  Q1: Which factor does this improve?
  Q2: How will we measure the improvement?
  Q3: Does it degrade any other factor?
  Q4: What is the A/B gate to confirm the improvement?

If you cannot answer Q1 and Q2 → do not implement.
If the answer to Q3 is "yes" without mitigation → do not implement.
```

### Measurement Discipline (Per Factor)

```go
// Each factor must have a computable metric — no unmeasured factors

// Edge measurement
type EdgeMetrics struct {
    EdgeScore       float64  // per-token
    ExpectancyPct   float64  // vs baseline of random timing
    WinRate         float64
}

// Probability measurement
type ProbabilityMetrics struct {
    CalibrationError float64  // |predicted_P - observed_win_rate|
    AvgPredictedP    float64
    AvgObservedWinRate float64
}

// Execution measurement
type ExecutionMetrics struct {
    SlippageError      float64  // actual_slippage - predicted_slippage
    AvgLatencyMs       int64
    InclusionRatePct   float64  // tx submitted vs included in block
    EdgeDecayActual    float64  // exp(-decay × actual_latency)
}

// Capital measurement
type CapitalMetrics struct {
    AllocationError    float64  // actual_size vs optimal_size
    CohortMultiplierAccuracy float64
    PortfolioConcentration float64
}

// DataQuality measurement
type DataQualityMetrics struct {
    FalsePositiveRate float64  // accepted → rug/loss
    FalseNegativeRate float64  // rejected → pump
    RugRate           float64
    PassRate          float64
}

// Adaptation measurement
type AdaptationMetrics struct {
    ErrorDecayRate          float64  // is FP/FN rate decreasing?
    CalibrationImprovement  float64  // is slippage/latency error decreasing?
    SamplesPerUpdate        int      // quality of learning signal
}
```

### Edge vs Non-Edge Filter (Design Principle)

```
A feature has edge if:
  win_rate(with feature) > win_rate(without feature)  AND
  expectancy(with feature) > expectancy(without feature)
  with N ≥ 30 samples

A feature does NOT have edge if it only adds complexity.
  → Remove it (complexity is a cost, not neutral)
```

### Profit-First Priority Ordering

```
When two improvements compete for engineering time, prioritize:

1. DataQuality — a rug gets through = total loss of that allocation
2. Execution   — latency/slippage kills edge even when signals are perfect
3. Edge        — more accurate signal detection = compounding returns
4. Probability — better calibration = better bet sizing
5. Capital     — marginal improvement to allocation sizing
6. Adaptation  — longer-horizon improvement; critical but not immediate
```

### Feature Evaluation Template

```
Feature: [name]
Proposed change: [description]

Factor impact:
  Edge:            [improve/degrade/neutral] — [how measured]
  Probability:     [improve/degrade/neutral] — [how measured]
  Execution:       [improve/degrade/neutral] — [how measured]
  Capital:         [improve/degrade/neutral] — [how measured]
  DataQuality:     [improve/degrade/neutral] — [how measured]
  AdaptationQuality: [improve/degrade/neutral] — [how measured]

Net assessment:
  - Improves: [list factors]
  - Degrades: [list factors + mitigation]
  - Complexity cost: [lines of code, new dependencies, maintenance burden]
  - A/B gate: [what result triggers promotion — specific metric threshold]

Decision: [implement / reject / defer]
```

### Implementation Discipline

```go
// Every new config parameter must have:
//   1. A comment explaining which profit factor it affects
//   2. A metric that reflects its impact
//   3. A valid range with bounds

// ✅ Good config parameter documentation
type ModelConfig struct {
    // ProbabilityThreshold: affects Probability factor
    // Lower → more trades pass (higher FP risk), higher → fewer trades (higher FN risk)
    // Optimal range: [0.50, 0.75] per operational mode
    ProbabilityThreshold float64 `yaml:"probability_threshold"`

    // SlippageModel.TaxPenaltyMultiplier: affects Execution factor
    // Scales tax contribution to slippage estimate
    // Valid range: [0.5, 2.0]
    TaxPenaltyMultiplier float64 `yaml:"tax_penalty_multiplier"`
}
```

### The Compounding Principle

```
Small improvements across all factors compound:

  Edge +10%: Profit × 1.10
  Execution +10%: Profit × 1.10 × 1.10 = × 1.21
  DataQuality +10%: × 1.33
  Probability +10%: × 1.46
  Capital +10%: × 1.61
  Adaptation +10%: × 1.77

vs. massive improvement in one factor:
  Edge +100% alone: × 2.00

Compounding small improvements across all factors > single large improvement.
→ Fix all factors incrementally. Never neglect one.
```

### Anti-Patterns

```go
// ❌ Feature that adds complexity without measurable improvement
// "Let's add a sentiment analyzer from Twitter"
// → Cannot measure impact on any factor reliably → reject

// ❌ Optimizing one factor while ignoring degradation of another
// "Lowering probability threshold gets more trades"
// → If FP rate rises by 5% → DataQuality degrades → net negative

// ❌ Deploying without A/B gate
// Changing probability threshold directly to production without test → FORBIDDEN

// ❌ Untraceable changes
// Editing config values without creating new StrategyVersion
// → Learning attribution breaks → AdaptationQuality → 0

// ✅ Correct decision framework
if feature.ImprovesAtLeastOneFactor() &&
    !feature.DegradeAnyFactor() &&
    feature.HasMeasurableABGate() {
    implement(feature)
} else {
    defer(feature)
}
```

---

## Checklist

```
[ ] New feature identified which profit factor it improves
[ ] Improvement is measurable (specific metric + threshold)
[ ] No factor degraded without explicit mitigation
[ ] A/B gate defined before implementation begins
[ ] New config parameters documented with factor impact + valid range
[ ] Changes deployed as new StrategyVersion (not direct config edit)
[ ] Complexity cost assessed (is the improvement worth the maintenance burden?)
[ ] Learning signal will attribute outcomes to this change via VersionID
[ ] All six factors have metrics defined and tracked
[ ] Priority ordering respected: DQ > Execution > Edge > Probability > Capital > Adaptation
```

---

## References

- Architecture: `docs/reference/architecture.md` § 1 (Core Invariant), § 5 (KPIs)
- Architecture context: `docs/archive/architecture-context/0_system_definition.md`
- Architecture context: `docs/archive/architecture-context/1_global_control_loop.md`
- DTO spec: `docs/reference/dto_contracts.md` (all DTO types)
- Roadmap: `docs/reference/implementation_roadmap.md` (all phases)
