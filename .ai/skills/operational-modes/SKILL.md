# Operational Modes Skill

## Purpose

Enforce correct implementation of the three-mode control system that governs the
pipeline's risk appetite. The system ALWAYS operates in exactly one of three modes.
Mode transitions are bounded and governed — not arbitrary.

---

## Rules

### Three Modes (Canonical Definitions)

```go
type OperationalMode string
const (
    ModeStrict      OperationalMode = "strict"
    ModeBalanced    OperationalMode = "balanced"
    ModeExploration OperationalMode = "exploration"
)
```

```yaml
# config/pipeline.yaml — mode threshold profiles
modes:
  strict:
    probability_threshold: 0.68
    ev_threshold: 1.25
    max_risk_score: 0.30
    max_tax: 8
    min_liquidity_usd: 20000
    explore_budget_pct: 1.0 # ≤1% of capital in exploration trades
    min_lp_lock_strength: 0.8

  balanced:
    probability_threshold: 0.60
    ev_threshold: 1.15
    max_risk_score: 0.45
    max_tax: 12
    min_liquidity_usd: 10000
    explore_budget_pct: 2.0
    min_lp_lock_strength: 0.6

  exploration:
    probability_threshold: 0.50
    ev_threshold: 1.05
    max_risk_score: 0.60
    max_tax: 15
    min_liquidity_usd: 5000
    explore_budget_pct: 5.0 # 3-5% of capital in exploration trades
    min_lp_lock_strength: 0.4

mode_transitions:
  max_transitions_per_window: 1 # bounded: one transition per window
  transition_window_sec: 3600 # 1-hour window
  starvation_trigger_sec: 1800 # auto-upgrade after 30 min no opportunity
  rug_rate_auto_downgrade: 0.15 # auto-downgrade if rug rate > 15%
  fp_rate_auto_downgrade: 0.25 # auto-downgrade if FP rate > 25%
```

### Valid Transitions (Direction-Constrained)

```
STRICT ↔ BALANCED ↔ EXPLORATION
         (no STRICT → EXPLORATION skip)

Auto-upgrade path:  STRICT → BALANCED → EXPLORATION
Auto-downgrade path: EXPLORATION → BALANCED → STRICT
Manual override:    any → any (with /mode command + logging)
```

```go
var validTransitions = map[OperationalMode][]OperationalMode{
    ModeStrict:      {ModeBalanced},       // can only move to balanced
    ModeBalanced:    {ModeStrict, ModeExploration},
    ModeExploration: {ModeBalanced},       // can only move back to balanced
}
```

### Transition Guard (Bounded: One Per Window)

```go
type ModeTransition struct {
    FromMode        OperationalMode
    ToMode          OperationalMode
    Reason          string   // "starvation" | "high_rug_rate" | "high_fp_rate" | "manual"
    TriggeredBy     string   // "auto" | operator_chat_id
    TransitionedAt  string   // ISO 8601
    WindowID        string   // SHA256(current_window_start)[:16]
}

func (m *ModeController) Transition(
    ctx context.Context,
    toMode OperationalMode,
    reason string,
    actor string,
) error {
    current := m.GetCurrentMode(ctx)

    // Validate direction
    if !isValidTransition(current, toMode) {
        return fmt.Errorf("invalid transition %s → %s", current, toMode)
    }

    // Enforce one-per-window guard
    windowID := computeWindowID(time.Now(), m.cfg.TransitionWindowSec)
    if m.lastTransitionWindowID == windowID {
        return fmt.Errorf("transition already occurred in this window (%s)", windowID)
    }

    // Log BEFORE executing (audit trail)
    adapter.EmitSystemEvent(ctx, SystemEventPayload{
        EventSubtype: "mode_change",
        Summary:      fmt.Sprintf("mode: %s → %s (%s)", current, toMode, reason),
        Details:      map[string]string{"from": string(current), "to": string(toMode), "reason": reason, "actor": actor},
    })

    // Persist mode change
    adapter.SetActiveMode(ctx, toMode)
    m.lastTransitionWindowID = windowID
    return nil
}
```

### Auto-Upgrade (Starvation Recovery)

```go
// Starvation: no opportunity for T seconds → upgrade to more permissive mode
func (m *ModeController) checkStarvation(ctx context.Context, lastOpportunityAt time.Time) {
    starvationDur := time.Since(lastOpportunityAt)
    if starvationDur < time.Duration(m.cfg.StarvationTriggerSec)*time.Second {
        return
    }

    current := m.GetCurrentMode(ctx)
    switch current {
    case ModeStrict:
        m.Transition(ctx, ModeBalanced, "starvation", "auto")
    case ModeBalanced:
        m.Transition(ctx, ModeExploration, "starvation", "auto")
    case ModeExploration:
        // Already at most permissive — emit alert instead
        adapter.EmitSystemEvent(ctx, SystemEventPayload{
            EventSubtype: "starvation_critical",
            Summary:      "Already in EXPLORATION with no opportunities — market conditions suspect",
            Severity:     "critical",
        })
    }
}
```

### Auto-Downgrade (Safety Response)

```go
// High rug/FP rate → downgrade to more conservative mode
func (m *ModeController) checkSafetyDowngrade(
    ctx context.Context,
    rugRate float64,
    fpRate float64,
) {
    current := m.GetCurrentMode(ctx)

    shouldDowngrade := rugRate > m.cfg.RugRateAutoDowngrade ||
        fpRate > m.cfg.FPRateAutoDowngrade

    if !shouldDowngrade { return }

    switch current {
    case ModeExploration:
        m.Transition(ctx, ModeBalanced, "high_rug_rate", "auto")
    case ModeBalanced:
        m.Transition(ctx, ModeStrict, "high_rug_rate", "auto")
    case ModeStrict:
        // Already at strictest — emit critical alert
        adapter.EmitSystemEvent(ctx, SystemEventPayload{
            EventSubtype: SysEventHighRugRate,
            Summary:      fmt.Sprintf("High rug rate %.1f%% in STRICT mode — review strategy", rugRate*100),
            Severity:     "critical",
        })
    }
}
```

### Mode Loading Per Pipeline Run

```go
// Always load active mode from database at pipeline start
// Never assume mode from previous run — may have changed via /mode command
func loadActiveMode(ctx context.Context, adapter database.Adapter) (OperationalMode, error) {
    mode, err := adapter.GetActiveMode(ctx)
    if err != nil {
        return ModeBalanced, fmt.Errorf("load active mode: %w", err)  // safe default
    }
    return mode, nil
}

// Load mode-specific thresholds at run start
func loadModeThresholds(mode OperationalMode, cfg PipelineConfig) ThresholdProfile {
    return cfg.Modes[string(mode)]
}
```

### Manual Override (/mode command)

```go
// /mode command from Telegram → event bus → mode controller
// No direction restriction for manual override (operator knows what they're doing)
// But STILL bounded to one-per-window AND logged to event bus
func handleModeCommand(ctx context.Context, operatorID int64, modeStr string) {
    toMode := OperationalMode(modeStr)
    if toMode != ModeStrict && toMode != ModeBalanced && toMode != ModeExploration {
        sendTelegramReply(operatorID, "Invalid mode. Use: strict|balanced|exploration")
        return
    }
    // Manual overrides can skip direction constraint but not window guard
    if err := m.TransitionManual(ctx, toMode, "manual", strconv.FormatInt(operatorID, 10)); err != nil {
        sendTelegramReply(operatorID, fmt.Sprintf("Transition failed: %v", err))
        return
    }
    sendTelegramReply(operatorID, fmt.Sprintf("Mode set to %s", toMode))
}
```

### Anti-Patterns

```go
// ❌ Hardcoded thresholds instead of mode-specific config
if score > 0.65 { return "pass" }  // Wrong — must use cfg.Modes[mode].EVThreshold

// ❌ Skipping transition (STRICT → EXPLORATION directly)
m.Transition(ctx, ModeExploration, "starvation", "auto")  // from STRICT — invalid

// ❌ Multiple transitions per window
m.Transition(ctx, ModeBalanced, ...)  // OK
m.Transition(ctx, ModeExploration, ...)  // Second transition in same window — blocked

// ❌ Mode not persisted to database
m.currentMode = ModeExploration  // In-memory only — lost on restart

// ✅ Correct
thresholds := loadModeThresholds(activeMode, cfg)
if ev < thresholds.EVThreshold { return "reject" }
```

---

## Checklist

```
[ ] Three modes only: strict | balanced | exploration
[ ] Mode persisted to database — never in-memory only
[ ] Loaded from database at every pipeline run start
[ ] Thresholds per mode defined in config/pipeline.yaml
[ ] Valid transitions: strict↔balanced↔exploration (no skip)
[ ] One transition per window (window size from config)
[ ] Auto-upgrade on starvation (configurable trigger duration)
[ ] Auto-downgrade on high rug/FP rate (configurable thresholds)
[ ] Manual /mode override allowed but still guarded by window limit
[ ] All transitions emitted to event bus as system_events
[ ] Exploration budget: 1% (strict) | 2% (balanced) | 3-5% (exploration)
[ ] Mode-specific thresholds loaded at pipeline start — no mid-run changes
```

---

## References

- Architecture: `docs/architecture.md` § 7 (Operational Modes)
- Architecture context: `docs/architecture-context/1_global_control_loop.md`
- Architecture context: `docs/architecture-context/13_observability_finalization.md` § 15
- Roadmap: `docs/implementation_roadmap.md` Phase 3 (Mode Controller)
- Config: `config/pipeline.yaml` → `modes`, `mode_transitions`