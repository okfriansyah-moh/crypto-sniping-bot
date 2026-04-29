---
name: capital-sizing
type: skill
description: >
  Capital allocation and risk-aware position sizing patterns (Layer 7). Use when
  implementing or reviewing the capital engine: base allocation, risk-adjusted sizing,
  cohort multipliers, exploration budget, and AllocationDTO production. Wrong sizing
  destroys profit even with perfect edges.
---

# Capital Sizing Skill

## Purpose

Enforce correct implementation of the capital engine that converts selected edges into
position sizes. This is a **risk allocator**, not a profit maximizer. Oversizing causes
ruin; undersizing wastes edge.

**Core formula:**

```
size_i = size_i* × (1 - SlippagePenalty_i) × (1 - LatencyPenalty_i) × CohortMultiplier_i

Where:
  size_i* = ŵ_i × C_alloc     (base allocation)
  ŵ_i = w_i / Σ w_j           (normalized weight)
  w_i = Score_i_norm × P_i × Confidence_i  (raw weight)
```

---

## Rules

### Base Allocation Rule (Deterministic)

```go
// Step 1: Compute raw weights per selected edge
func computeRawWeight(edge contracts.ValidatedEdgeDTO) float64 {
    return edge.Score * edge.Probability * edge.Confidence
}

// Step 2: Normalize weights
func normalizeWeights(weights []float64) []float64 {
    sum := 0.0
    for _, w := range weights { sum += w }
    if sum == 0 { return make([]float64, len(weights)) }  // guard zero division
    normalized := make([]float64, len(weights))
    for i, w := range weights { normalized[i] = w / sum }
    return normalized
}

// Step 3: Propose sizes
func proposeSizes(normalizedWeights []float64, cAlloc float64) []float64 {
    sizes := make([]float64, len(normalizedWeights))
    for i, w := range normalizedWeights {
        sizes[i] = w * cAlloc
    }
    return sizes
}
```

### Risk Adjustment

```go
// Apply penalties + cohort multipliers to proposed sizes
func riskAdjustSize(
    proposed float64,
    slip contracts.SlippageEstimateDTO,
    lat contracts.LatencyProfileDTO,
    cohort CohortStats,
    cfg CapitalConfig,
) float64 {
    slippagePenalty := clamp(slip.EstimatedSlippage * cfg.SlippagePenaltyMultiplier, 0, 0.5)
    latencyPenalty  := clamp(1 - lat.EdgeDecayFactor, 0, 0.3)  // cap at 30% penalty
    cohortMult      := getCohortMultiplier(cohort, cfg)

    adjusted := proposed * (1 - slippagePenalty) * (1 - latencyPenalty) * cohortMult
    return clamp(adjusted, 0, cfg.MaxPositionUSD)
}
```

### Hard Constraints (All Must Pass)

```go
type CapitalConstraints struct {
    MaxPositionUSD         float64  // cap per position (config)
    MaxConcurrentPositions int      // cap total open positions (config)
    MaxPortfolioRiskPct    float64  // max % of total capital at risk (config)
    ExplorationBudgetPct   float64  // % reserved for exploration (1-5% per mode)
    MinPositionUSD         float64  // minimum viable position size
}

// Enforce ALL constraints before allocating
func enforceConstraints(size float64, c CapitalConstraints, portfolio PortfolioState) float64 {
    if portfolio.ActivePositions >= c.MaxConcurrentPositions { return 0 }
    if portfolio.TotalRiskUSD/portfolio.TotalCapitalUSD >= c.MaxPortfolioRiskPct { return 0 }
    size = math.Min(size, c.MaxPositionUSD)
    if size < c.MinPositionUSD { return 0 }  // not worth the gas
    return size
}
```

Config:

```yaml
# config/capital.yaml
capital:
  max_position_usd: 500.0
  max_concurrent_positions: 10
  max_portfolio_risk_pct: 0.20 # 20% max at risk
  min_position_usd: 20.0
  exploration_budget_pct:
    strict: 0.01 # 1% exploration
    balanced: 0.02 # 2% exploration
    exploration: 0.05 # 5% exploration
```

### Exploration Budget (Mandatory)

```go
// Reserve exploration budget for lower-scoring edges
func applyExplorationBudget(
    allocs []AllocationProposal,
    totalCapital float64,
    cfg CapitalConfig,
) []AllocationProposal {
    exploreBudget := totalCapital * cfg.ExplorationBudgetPct
    result := []AllocationProposal{}

    for _, alloc := range allocs {
        if alloc.IsExploration {
            alloc.Size = math.Min(alloc.Size, exploreBudget/float64(cfg.MaxExplorePositions))
        }
        result = append(result, alloc)
    }
    return result
}
```

**Exploration positions** are edges below the normal score threshold but above the
exploration threshold. They feed false-negative data to the learning engine.

### Cohort Multipliers (Adaptive)

```go
// Multipliers adjusted by learning engine per cohort performance
func getCohortMultiplier(cohort CohortStats, cfg CapitalConfig) float64 {
    if cohort.ExpectancyPct > cfg.CohortBoostThreshold {
        return cfg.MaxCohortMultiplier  // e.g., 1.3 — profitable cohort boost
    }
    if cohort.ExpectancyPct < cfg.CohortPenaltyThreshold {
        return cfg.MinCohortMultiplier  // e.g., 0.5 — underperforming cohort penalty
    }
    return 1.0
}
```

**Rule:** Cohort multipliers are config-driven, updated by learning engine.
Never hardcode multipliers. Range: `[0.3, 1.5]` (bounded).

### AllocationDTO Output

```go
AllocationDTO{
    EventID:          SHA256(canonical_json(dto))[:16],
    ExecutionID:      SHA256(token+wallet+amount+block)[:16],  // idempotency key
    TokenAddress:     edge.TokenAddress,
    AllocatedUSD:     finalSize,
    AllocatedNative:  finalSize / tokenPriceNative,
    SlippageTolerance: cfg.SlippageTolerance,
    IsExploration:    bool,
    CohortID:         edge.CohortID,
    Score:            edge.Score,
    Probability:      edge.Probability,
    Confidence:       edge.Confidence,
    AllocatedAt:      ISO8601UTC,
    // Traceability
    TraceID:          edge.TraceID,
    CorrelationID:    edge.CorrelationID,
    CausationID:      edge.EventID,
    VersionID:        activeStrategyVersion.VersionID,
}
```

### Anti-Patterns

```go
// ❌ Fixed position size
size := 100.0  // Wrong — must be proportional to Score × P × Confidence

// ❌ No exploration budget
// Allocate only to top-scoring edges... but never learn what you're missing

// ❌ Unlimited concurrent positions
for _, edge := range edges { allocate(edge) }  // Wrong — enforce MaxConcurrentPositions

// ❌ Negative position size (from penalties)
size := proposed - slippagePenalty  // Wrong — size can't go negative; use clamp

// ❌ Cohort multiplier outside bounds
multiplier := learnedValue  // Wrong — clamp to [0.3, 1.5]

// ✅ Correct
w := computeRawWeight(edge)
normalized := normalize(w, totalWeight)
proposed := normalized * cAlloc
riskAdjusted := riskAdjustSize(proposed, slip, lat, cohort, cfg)
final := enforceConstraints(riskAdjusted, constraints, portfolio)
```

### Anti-Patterns

```go
// ❌ Fixed position size
size := 100.0  // Wrong — must be proportional to Score × P × Confidence

// ❌ No exploration budget
// Allocate only to top-scoring edges... but never learn what you're missing

// ❌ Unlimited concurrent positions
for _, edge := range edges { allocate(edge) }  // Wrong — enforce MaxConcurrentPositions

// ❌ Negative position size (from penalties)
size := proposed - slippagePenalty  // Wrong — size can't go negative; use clamp

// ❌ Cohort multiplier outside bounds
multiplier := learnedValue  // Wrong — clamp to [0.3, 1.5]

// ✅ Correct
w := computeRawWeight(edge)
normalized := normalize(w, totalWeight)
proposed := normalized * cAlloc
riskAdjusted := riskAdjustSize(proposed, slip, lat, cohort, cfg)
final := enforceConstraints(riskAdjusted, constraints, portfolio)
```

---

### Capital Preservation Integration

**Settlement-aware capital (crypto T+0):**

```go
// Crypto settles immediately (T+0) — available capital is liquid capital.
// No T+2 settlement lag. Cash not yet received from open positions IS available.
// AvailableCapital = PortfolioValue - (sum of all AllocationDTO.Amount for open positions)
func AvailableCryptoCapital(portfolioValue float64, openAllocations []contracts.AllocationDTO) float64 {
    var committed float64
    for _, a := range openAllocations {
        committed += a.Amount
    }
    return portfolioValue - committed
}
```

**Regime multipliers (applied to C_alloc before sizing):**

```go
// Regime multipliers compress or expand allocation based on market state.
// A crisis regime blocks ALL allocation (multiplier=0.0).
// Values from config/capital.yaml — never hardcoded.

type RegimeType string

const (
    RegimeTrendingBull  RegimeType = "trending_bull"   // multiplier: 1.2
    RegimeTrendingBear  RegimeType = "trending_bear"   // multiplier: 0.6
    RegimeMeanReverting RegimeType = "mean_reverting"  // multiplier: 1.0
    RegimeHighVol       RegimeType = "high_volatility" // multiplier: 0.4
    RegimeLowVol        RegimeType = "low_volatility"  // multiplier: 0.8
    RegimeCrisis        RegimeType = "crisis"           // multiplier: 0.0 → BLOCK ALL
)

func GetRegimeMultiplier(regime RegimeType, cfg RegimeMultiplierConfig) float64 {
    m, ok := cfg.Multipliers[string(regime)]
    if !ok { return 1.0 }  // default: no adjustment
    return m
}

func ApplyRegimeMultiplier(baseAlloc float64, regime RegimeType, cfg RegimeMultiplierConfig) float64 {
    m := GetRegimeMultiplier(regime, cfg)
    if m == 0.0 { return 0.0 }  // crisis: no allocation at all
    return baseAlloc * m
}
```

**Correlation limit (max 25% portfolio in correlated positions):**

```go
// Prevent over-concentration in correlated assets.
// Correlation threshold: 0.7 (pearson, rolling 20-bar).
// Maximum combined exposure of correlated positions: 25% of portfolio.

func CheckCorrelationLimit(
    proposedToken     string,
    openPositions     []contracts.PositionState,
    correlations      map[string]map[string]float64,  // token → token → pearson corr
    portfolioValue    float64,
    cfg               CorrelationConfig,  // MaxCorrelatedExposurePct=0.25, Threshold=0.7
) (allowed bool, currentCorrelatedPct float64) {
    var correlatedExposure float64
    for _, pos := range openPositions {
        corr := correlations[proposedToken][pos.TokenAddress]
        if math.Abs(corr) >= cfg.Threshold {
            correlatedExposure += pos.CurrentMarketValue
        }
    }
    currentPct := correlatedExposure / portfolioValue
    return currentPct < cfg.MaxCorrelatedExposurePct, currentPct
}
```

> See `.github/skills/drawdown-protection/SKILL.md` for HWM drawdown tier
> integration — drawdown tier multiplies the final allocation before output.
> See `.github/skills/exposure-monitor/SKILL.md` for hard position count and
> single-position ceiling enforcement.

---

## Checklist

```
[ ] Regime multiplier applied (crisis regime = 0 allocation, block all)
[ ] Correlated position exposure checked (max 25% in corr>0.7 positions)
[ ] AvailableCryptoCapital uses T+0 logic (committed = sum of open allocations)
[ ] Raw weight = Score × P × Confidence (all three)
[ ] Weights normalized before applying to C_alloc
[ ] SlippagePenalty applied: size × (1 - penalty)
[ ] LatencyPenalty via EdgeDecayFactor
[ ] CohortMultiplier applied — config-driven, bounded [0.3, 1.5]
[ ] MaxPositionUSD enforced per allocation
[ ] MaxConcurrentPositions enforced — no more positions when at cap
[ ] ExplorationBudget reserved: 1% (strict) to 5% (exploration mode)
[ ] MinPositionUSD gate — zero-out tiny allocations
[ ] ExecutionID is content-addressable (idempotency key)
[ ] AllocationDTO is immutable — new DTO for each allocation
[ ] CausationID = ValidatedEdgeDTO.EventID or SelectionOutputDTO.EventID
[ ] Division-by-zero guard on weight normalization
```

---

## Phase 9 Notes (Profitability Restoration)

Per `docs/implementation_roadmap.md` § 9.4, Phase 9 closes **GAP-05** by replacing the
fixed `cfg.FixedEntrySizeUsd = $50` constant with a Kelly-adjacent edge-proportional
sizing function. Configuration lives in `config/capital.yaml`.

**Phase 9 mandates:**

```
f_kelly = clamp((P × R − (1−P)) / R, 0, kelly_cap)
   where R = PriorGainBps / PriorLossBps
         P = probDTO.Probability   (joined from probability_event)

size_raw   = base_size_usd × f_kelly × confidence × cohort_multiplier × mode_multiplier
size_final = clamp(size_raw, min_size_usd, max_size_usd)
```

- **Mode multipliers** (from `config/capital.yaml`): STRICT 0.5×, BALANCED 1.0×, EXPLORATION 1.3×
- **Exploration band**: when `SelectionOutputDTO.IsExploration=true`, allocate 1–5 % of total capital regardless of edge score (intentional FN-frontier probing per architecture § 7)
- **Kelly cap**: `kelly_cap` (0.25 default), tightened to `kelly_cap_exploration` (0.05) during EXPLORATION mode
- **Confidence floor**: `Aggregate < min_aggregate_confidence` (0.40) → reject allocation
- **Negative Kelly**: `f_kelly < 0` (negative EV pre-clamp) → reject with `Reason=negative_kelly`
- **Mode lookup stale**: if `SystemStateDTO.Mode` older than `mode_freshness_sec` → default to BALANCED
- **Phase 6 envelope caps remain in force** — dynamic sizing applies BEFORE the four envelope rejections; all Phase 6 contracts preserved (regression check in exit criteria)

**Phase 9 exit criterion:** `SizeUsd` over 200 fixtures shows `stddev > 30 %` of mean;
mode-multiplier effect observable (STRICT mean ≈ 0.5 × BALANCED mean within ±10 %).

**Files added in Phase 9:** `kelly.go`, `cohort_multiplier.go`, `mode_multiplier.go`,
`exploration_band.go`.

---

## References

- Architecture context: `docs/architecture-context/9_capital_engine.md`
- DTO spec: `docs/dto_contracts.md` § 3.8 (AllocationDTO)
- Roadmap: `docs/implementation_roadmap.md` Phase 2.7
- Config: `config/capital.yaml`, `config/cohorts.yaml`
- `.github/skills/drawdown-protection/SKILL.md` — HWM tier multiplier on final allocation
- `.github/skills/exposure-monitor/SKILL.md` — Hard position count and ceiling enforcement
