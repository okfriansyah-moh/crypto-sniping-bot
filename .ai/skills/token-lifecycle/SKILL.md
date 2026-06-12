# Token Lifecycle Skill

## Purpose

Enforce correct implementation of the token state machine that tracks every token's
journey from detection through execution to outcome. Invalid state transitions cause
duplicate trades, missed exits, and corrupt learning data.

---

## Rules

### State Machine (Canonical)

```
detected → quality_passed → edge_detected → selected → allocated → submitted → included → closed
detected → quality_rejected (terminal)
detected → quality_risky_passed → edge_detected → ...
any_state → failed (terminal — after max retries)
any_state → expired (terminal — stale pool)
```

```go
// All valid states — no other values allowed
type TokenLifecycleState string
const (
    StateDetected        TokenLifecycleState = "detected"
    StateQualityPassed   TokenLifecycleState = "quality_passed"
    StateQualityRisky    TokenLifecycleState = "quality_risky_passed"
    StateQualityRejected TokenLifecycleState = "quality_rejected"   // terminal
    StateEdgeDetected    TokenLifecycleState = "edge_detected"
    StateEdgeRejected    TokenLifecycleState = "edge_rejected"       // no edge found
    StateSelected        TokenLifecycleState = "selected"
    StateAllocated       TokenLifecycleState = "allocated"
    StateSubmitted       TokenLifecycleState = "submitted"
    StateIncluded        TokenLifecycleState = "included"
    StateClosed          TokenLifecycleState = "closed"             // terminal
    StateFailed          TokenLifecycleState = "failed"             // terminal
    StateExpired         TokenLifecycleState = "expired"            // terminal
)
```

### Allowed Transitions (Strict — No Others)

```go
var allowedTransitions = map[TokenLifecycleState][]TokenLifecycleState{
    StateDetected:        {StateQualityPassed, StateQualityRisky, StateQualityRejected, StateExpired},
    StateQualityPassed:   {StateEdgeDetected, StateEdgeRejected, StateExpired},
    StateQualityRisky:    {StateEdgeDetected, StateEdgeRejected, StateExpired},
    StateQualityRejected: {},  // terminal
    StateEdgeDetected:    {StateSelected, StateExpired},
    StateEdgeRejected:    {},  // terminal
    StateSelected:        {StateAllocated, StateExpired},
    StateAllocated:       {StateSubmitted, StateFailed},
    StateSubmitted:       {StateIncluded, StateFailed},
    StateIncluded:        {StateClosed},
    StateClosed:          {},  // terminal
    StateFailed:          {},  // terminal
    StateExpired:         {},  // terminal
}

func isValidTransition(from, to TokenLifecycleState) bool {
    allowed, ok := allowedTransitions[from]
    if !ok { return false }
    for _, a := range allowed { if a == to { return true } }
    return false
}
```

### Compare-And-Swap (CAS) Updates

All state transitions use CAS to prevent race conditions. The adapter enforces this:

```go
// TransitionRequest includes expected current state — adapter rejects if mismatched
type TransitionRequest struct {
    LifecycleID    string
    TokenAddress   string
    ExpectedState  TokenLifecycleState  // CAS: must match current DB state
    NewState       TokenLifecycleState
    Reason         string
    TransitionedAt string
}

// Adapter returns ErrInvalidTransition if:
// 1. ExpectedState != current DB state (race condition)
// 2. Transition is not in allowedTransitions
// 3. From state is terminal
```

```go
// Usage: orchestrator calls adapter for state transitions
err := adapter.TransitionLifecycle(ctx, TransitionRequest{
    LifecycleID:   lifecycle.ID,
    TokenAddress:  token,
    ExpectedState: StateDetected,          // CAS: what we expect to find
    NewState:      StateQualityPassed,
    Reason:        "data_quality_passed",
    TransitionedAt: ISO8601UTC,
})
if errors.Is(err, database.ErrInvalidTransition) {
    // Another worker already transitioned — skip this token
    return nil
}
```

### Lifecycle ID (Content-Addressable)

```go
// LifecycleID = SHA256(token_address || first_detect_trace_id)[:16]
lifecycleID := sha256content(tokenAddress + traceID)[:16]
```

This ensures:

- Same token + same detection trace = same lifecycleID (idempotent)
- Different detection events for the same token = different lifecycles (correct)

### Terminal State Rules

```go
// Terminal states: quality_rejected, edge_rejected, closed, failed, expired
// Once in terminal state — no more transitions allowed

// Adapter rejects transitions out of terminal states with ErrTerminalState
func isTerminal(state TokenLifecycleState) bool {
    return state == StateQualityRejected ||
        state == StateEdgeRejected ||
        state == StateClosed ||
        state == StateFailed ||
        state == StateExpired
}
```

### Expiry Handling

```go
// Expiry worker: runs periodically to close stale tokens
// Token is expired if it never reached "allocated" within config.token_expire_sec
func expireStaleTokens(ctx context.Context, adapter database.Adapter, cfg LifecycleConfig) {
    cutoff := time.Now().Add(-time.Duration(cfg.TokenExpireSec) * time.Second)
    stale := adapter.GetTokensDetectedBefore(ctx, cutoff, []TokenLifecycleState{
        StateDetected, StateQualityPassed, StateQualityRisky, StateEdgeDetected,
    })
    for _, lc := range stale {
        adapter.TransitionLifecycle(ctx, TransitionRequest{
            LifecycleID:   lc.ID,
            ExpectedState: lc.State,
            NewState:      StateExpired,
            Reason:        "stale_token",
        })
    }
}
```

### Anti-Patterns

```go
// ❌ Skipping CAS — direct state set
lifecycle.State = StateQualityPassed  // Wrong — must use TransitionRequest with ExpectedState

// ❌ Invalid transition
adapter.TransitionLifecycle(ctx, TransitionRequest{
    ExpectedState: StateDetected,
    NewState:      StateIncluded,  // Wrong — skips intermediate states
})

// ❌ Transitioning terminal state
adapter.TransitionLifecycle(ctx, TransitionRequest{
    ExpectedState: StateFailed,    // terminal
    NewState:      StateSubmitted, // Wrong — ErrTerminalState
})

// ❌ Non-content-addressable lifecycle ID
lifecycleID := uuid.New().String()  // Wrong — must be SHA256(token + trace_id)[:16]

// ✅ Correct
lifecycleID := sha256content(token + traceID)[:16]
err := adapter.TransitionLifecycle(ctx, TransitionRequest{
    LifecycleID:   lifecycleID,
    ExpectedState: StateDetected,   // CAS
    NewState:      StateQualityPassed,
    Reason:        "dq_passed",
})
if errors.Is(err, database.ErrInvalidTransition) { return nil }  // already moved
```

---

## Checklist

```
[ ] All state values match the canonical constant definitions
[ ] Only transitions in allowedTransitions are executed
[ ] All transitions use CAS (ExpectedState in TransitionRequest)
[ ] ErrInvalidTransition handled gracefully (skip, not error)
[ ] Terminal states: quality_rejected, edge_rejected, closed, failed, expired
[ ] LifecycleID = SHA256(token + trace_id)[:16]
[ ] Expiry worker runs periodically for stale non-terminal tokens
[ ] TokenExpireSec loaded from config
[ ] Transitions logged with Reason and TransitionedAt timestamp
[ ] Adapter is the ONLY place that writes lifecycle state
[ ] Modules never write lifecycle state directly
```

---

## References

- Architecture: `docs/architecture.md` § 4.7 (Token Lifecycle State Machine)
- DB spec: `docs/db_adapter_spec.md` § 6.4 (token_lifecycle table)
- Roadmap: `docs/implementation_roadmap.md` Phase 0 (schema), Phase 2
- Config: `config/pipeline.yaml` → `lifecycle.token_expire_sec`