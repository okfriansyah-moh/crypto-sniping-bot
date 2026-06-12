# Strategy Versioning Skill

## Purpose

Enforce correct implementation of the strategy versioning system that makes every
configuration change auditable, reversible, and attributable to trade outcomes.

**Core principle:** If you cannot answer "which config made this trade?", your learning
system is invalid and your A/B comparisons are noise.

---

## Rules

### StrategyVersion is Immutable

```go
// StrategyVersion is a complete snapshot of ALL tunable configuration
// Once created — NEVER modified. Every change = new version.
type StrategyVersion struct {
    VersionID        string            // SHA256(canonical_json(snapshot))[:16]
    ParentVersionID  string            // previous version (empty for v0)
    Thresholds       map[string]float64
    FeatureWeights   map[string]float64
    SlippageParams   map[string]float64
    LatencyParams    map[string]float64
    CapitalRules     map[string]float64
    ExecutionParams  map[string]float64
    CohortMultipliers map[string]float64
    OperationalMode  string            // "strict" | "balanced" | "exploration"
    CreatedAt        string            // ISO 8601
    ActivatedAt      *string           // nil = not yet active
    DeactivatedAt    *string           // nil = currently active
}
```

**VersionID rule:** `SHA256(canonical_json(all_params_snapshot))[:16]`
Same parameters → same VersionID. Deterministic.

### Every Trade Stores VersionID

```go
// MANDATORY: every DTO that flows through the event bus carries VersionID
// Set at orchestrator start — never changed mid-pipeline for a single trace

// In orchestrator (start of each pipeline run):
func loadActiveVersion(ctx context.Context, adapter database.Adapter) (StrategyVersion, error) {
    version, err := adapter.GetActiveStrategyVersion(ctx)
    if err != nil {
        return StrategyVersion{}, fmt.Errorf("load active strategy version: %w", err)
    }
    return version, nil
}

// Pin to all DTOs:
dto.VersionID = activeVersion.VersionID
```

### Version Creation (Learning Engine Trigger)

```go
// Only the learning engine creates new versions — never modules
func createNewVersion(
    ctx context.Context,
    adapter database.Adapter,
    current StrategyVersion,
    updates ParameterFamily,
) (StrategyVersion, error) {
    // Merge updates onto current snapshot
    newSnapshot := mergeUpdates(current, updates)

    newVersion := StrategyVersion{
        VersionID:       SHA256(canonical_json(newSnapshot))[:16],
        ParentVersionID: current.VersionID,
        CreatedAt:       ISO8601UTC,
        // ... all params from newSnapshot
    }

    // Persist first — then can activate
    if err := adapter.SaveStrategyVersion(ctx, newVersion); err != nil {
        return StrategyVersion{}, fmt.Errorf("save strategy version: %w", err)
    }
    return newVersion, nil
}
```

### A/B Promotion Gate (Strict)

```
Promote V2 over V1 only when ALL three conditions hold:
  1. expectancy(V2) > expectancy(V1) × 1.05    — at least 5% improvement
  2. drawdown(V2) ≤ drawdown(V1)               — no regression in worst losses
  3. N ≥ 30-50 samples                         — sufficient statistical basis
```

```go
func evaluatePromotion(
    v1Stats, v2Stats VersionStats,
    cfg VersioningConfig,
) PromotionDecision {
    if v2Stats.SampleCount < cfg.MinSamplesForPromotion {
        return PromotionDecision{Promote: false, Reason: "insufficient_samples"}
    }
    if v2Stats.ExpectancyPct <= v1Stats.ExpectancyPct*cfg.MinImprovementFactor {
        return PromotionDecision{Promote: false, Reason: "no_expectancy_improvement"}
    }
    if v2Stats.MaxDrawdownPct > v1Stats.MaxDrawdownPct {
        return PromotionDecision{Promote: false, Reason: "drawdown_regression"}
    }
    return PromotionDecision{Promote: true, Reason: "all_gates_passed"}
}
```

Config:

```yaml
# config/versioning.yaml
versioning:
  min_samples_for_promotion: 30
  min_improvement_factor: 1.05 # 5% improvement required
  max_active_experiments: 1 # only 1 A/B test at a time
  rollback_trigger_drawdown: 0.20 # rollback if drawdown > 20%
  max_promotion_checks_per_day: 4
```

### Rollback = Version Pointer Switch (Not Config Revert)

```go
// Rollback: activate the previous version — never modify config files
func rollbackVersion(ctx context.Context, adapter database.Adapter, targetVersionID string) error {
    // 1. Deactivate current version
    adapter.DeactivateVersion(ctx, currentVersion.VersionID)
    // 2. Activate previous version
    adapter.ActivateVersion(ctx, targetVersionID)
    // Log the rollback event to event bus
    adapter.EmitSystemEvent(ctx, SystemEvent{
        EventType: "version_rollback",
        Details:   fmt.Sprintf("rolled back to %s", targetVersionID),
    })
    return nil
}
```

**Rule:** Rollback NEVER touches config files or SQL schema. It only changes which
VersionID is marked as `active = true` in the `strategy_versions` table.

### Automatic Rollback Watchdog

```go
// Rollback watchdog worker: monitors active version KPIs
// If performance drops below threshold, auto-rollback to parent
func watchdogCheck(ctx context.Context, activeVersion StrategyVersion, cfg VersioningConfig) {
    stats := adapter.GetVersionStats(ctx, activeVersion.VersionID)
    if stats.MaxDrawdownPct > cfg.RollbackTriggerDrawdown ||
        stats.WinRate < cfg.RollbackTriggerWinRate {
        rollbackVersion(ctx, adapter, activeVersion.ParentVersionID)
        notifyOperator("auto_rollback", activeVersion.VersionID)
    }
}
```

### Metric Segmentation by Version

```
Always compute per-version:
  - pnl_total
  - win_rate
  - false_positive_rate
  - false_negative_rate
  - slippage_error (actual vs estimated)
  - latency_error
  - expectancy (mean PnL × win_rate - mean loss × loss_rate)
```

### Replay Determinism

```
For replay to be bit-for-bit identical:
  1. Use event timestamps only — never wall clock (time.Now())
  2. Same VersionID → same parameters → same decisions
  3. No random number generation (seed = 0 or from deterministic source)
  4. All RPC calls use cached/stored responses during replay
```

### Anti-Patterns

```go
// ❌ Modifying existing version
version.Thresholds["probability"] = 0.7  // Wrong — immutable, create new version

// ❌ Not storing VersionID on trades
alloc := AllocationDTO{TokenAddress: token}  // Wrong — missing VersionID

// ❌ Promoting without all three gates
if v2.Expectancy > v1.Expectancy { promote(v2) }  // Wrong — check drawdown + samples too

// ❌ Rollback by editing config.yaml
// sed -i 's/probability: 0.7/probability: 0.6/' config/models.yaml  // Wrong

// ❌ Multiple parameter families per version update
newVersion.Thresholds = updatedThresholds
newVersion.FeatureWeights = updatedWeights  // Wrong — one family per learning cycle

// ✅ Correct
newVersion := createNewVersion(ctx, adapter, currentVersion, thresholdUpdatesOnly)
if shouldPromote(v1Stats, v2Stats, cfg) { activateVersion(newVersion) }
```

---

## Checklist

```
[ ] VersionID = SHA256(canonical_json(all_params))[:16] — deterministic
[ ] StrategyVersion is immutable — never update, always create new
[ ] Every trade DTO carries VersionID (set by orchestrator at start)
[ ] A/B promotion requires: +5% expectancy AND drawdown ≤ old AND N ≥ 30
[ ] Rollback = activate parent version pointer — never edit config files
[ ] Automatic rollback watchdog monitors active version KPIs
[ ] Only one parameter family updated per learning cycle per version
[ ] All version metrics segmented per VersionID in analytics
[ ] ParentVersionID tracked for version lineage
[ ] Replay produces bit-for-bit identical results with same VersionID
[ ] Version creation is orchestrator-only — modules never create versions
```

---

## References

- Architecture context: `docs/architecture-context/13_observability_finalization.md`
- Architecture: `docs/architecture.md` § 4.1–4.2 (Strategy Versioning)
- DTO spec: `docs/dto_contracts.md` (StrategyVersion, StrategyConfig)
- Roadmap: `docs/implementation_roadmap.md` Phase 5
- Config: `config/versioning.yaml`