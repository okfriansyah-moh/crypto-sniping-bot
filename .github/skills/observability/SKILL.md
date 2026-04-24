---
name: observability
type: skill
description: >
  Observability, KPI tracking, and structured alerting patterns. Use when
  implementing or reviewing metrics emission, Telegram alert templates, system_event
  production, health monitoring, and pipeline throughput tracking. Every KPI must
  be measurable per StrategyVersion. Unstructured console logs are forbidden.
---

# Observability Skill

## Purpose

Enforce structured observability that makes the system's health and KPIs visible in
real time. Every key decision, execution, and learning action emits structured data —
not console strings.

**Measurement principle:** You cannot improve what you cannot measure. Every factor in
`Profit = Edge × Probability × Execution × Capital × DataQuality × AdaptationQuality`
must have a corresponding metric.

---

## Rules

### KPIs Per StrategyVersion (Mandatory)

```go
// These metrics MUST be computed per version — not just globally
type VersionMetrics struct {
    VersionID       string

    // Trading outcomes
    TotalTrades     int
    WinRate         float64  // profitable trades / total trades
    PnLTotalUSD     float64
    PnLTotalPct     float64
    ExpectancyPct   float64  // mean(PnL%) = expected value per trade
    MaxDrawdownPct  float64  // worst peak-to-trough loss

    // Quality signals
    FalsePositiveRate float64  // accepted → loss/rug / total accepted
    FalseNegativeRate float64  // rejected → pump / total rejected

    // Execution quality
    AvgSlippageExpected float64
    AvgSlippageActual   float64
    SlippageError       float64  // actual - expected (positive = worse than expected)
    AvgLatencyExpectedMs int64
    AvgLatencyActualMs   int64

    // Pipeline health
    ScanRatePerSec   float64
    PassRateByLayer  map[string]float64  // per layer pass %
    SelectionRate    float64

    UpdatedAt        string  // ISO 8601
}
```

### Structured Logging (Required)

```go
// All logging via structured logger — no fmt.Println, no log.Print
// Use: slog (Go 1.21+) or zerolog

// ✅ Correct
logger.Info("trade executed",
    slog.String("event_id", result.EventID),
    slog.String("token", result.TokenAddress),
    slog.Float64("pnl_pct", result.PnLPct),
    slog.String("exit_reason", result.ExitReason),
    slog.String("version_id", result.VersionID),
    slog.String("trace_id", result.TraceID),
)

// ❌ Forbidden
fmt.Printf("Trade executed: token=%s pnl=%.2f\n", token, pnl)
log.Println("execution failed:", err)
```

### System Event Emission

```go
// System events: mode changes, version promotions, circuit breaker trips, halts
// Emitted to event bus → Telegram dispatcher reads and sends alerts

type SystemEventPayload struct {
    EventSubtype string            // see constants below
    Summary      string
    Details      map[string]string
    Severity     string            // "info" | "warning" | "critical"
    VersionID    string
}

const (
    SysEventModeChange         = "mode_change"
    SysEventVersionPromotion   = "version_promotion"
    SysEventVersionRollback    = "version_rollback"
    SysEventCircuitBreakerOpen = "circuit_breaker_open"
    SysEventStarvation         = "starvation_detected"
    SysEventHighRugRate        = "high_rug_rate"
    SysEventOperatorCommand    = "operator_command"
    SysEventLearningUpdate     = "learning_update"
    SysEventSystemHalt         = "system_halt"
    SysEventSystemResume       = "system_resume"
)
```

### Alert Templates (Structured Format)

```go
// Consistent Telegram message templates — used by dispatcher

// Pipeline health
const tmplHealth = `[HEALTH]
scan/s: %.0f
pass_rate: %.1f%%
mode: %s
starvation: %v
version: %s`

// Edge decision
const tmplEdge = `[EDGE]
token: %s
score: %.0f
prob: %.2f
decision: %s`

// Trade execution
const tmplBuy = `[BUY]
wallet: %s
latency: %dms
slippage: %.1f%% → %.1f%%
tx: %s`

// Trade exit
const tmplSell = `[SELL]
token: %s
PnL: %+.1f%%
reason: %s
duration: %ds
version: %s`

// Execution failure
const tmplFail = `[FAIL]
stage: %s
reason: %s
token: %s`

// Learning update
const tmplLearn = `[LEARN]
signal: %s
action: %s
param: %s: %.3f → %.3f`
```

### Pipeline Health Monitor

```go
// Health monitor: runs on rolling window, emits NO_OPPORTUNITY_ALERT
type PipelineHealthMonitor struct {
    windowSec int
    metrics   *PipelineMetrics
}

func (m *PipelineHealthMonitor) Check(ctx context.Context, adapter database.Adapter, cfg MonitorConfig) {
    stats := m.metrics.WindowStats(m.windowSec)

    // Starvation: pass_rate == 0 for entire window
    if stats.PassRate == 0 {
        adapter.EmitSystemEvent(ctx, SystemEventPayload{
            EventSubtype: SysEventStarvation,
            Summary:      "Pipeline starvation detected",
            Details: map[string]string{
                "window_sec": strconv.Itoa(m.windowSec),
                "pass_rate":  "0.0",
                "mode":       stats.Mode,
            },
            Severity: "warning",
        })
    }

    // Overtrading: pass_rate > 10% = thresholds too loose
    if stats.PassRate > cfg.OvertradingThreshold {
        adapter.EmitSystemEvent(ctx, SystemEventPayload{
            EventSubtype: "overtrading_detected",
            Summary:      fmt.Sprintf("Pass rate %.1f%% exceeds threshold", stats.PassRate*100),
            Severity:     "warning",
        })
    }

    // High rug rate
    if stats.RugRate > cfg.HighRugRateThreshold {
        adapter.EmitSystemEvent(ctx, SystemEventPayload{
            EventSubtype: SysEventHighRugRate,
            Summary:      fmt.Sprintf("Rug rate %.1f%% above threshold", stats.RugRate*100),
            Severity:     "critical",
        })
    }
}
```

### Pass Rate by Layer

```go
// Track how many tokens pass each layer — reveals where attrition happens
type LayerPassRate struct {
    Layer     string  // "data_quality" | "edge" | "validation" | "selection"
    Total     int
    Passed    int
    Rejected  int
    PassRate  float64
    WindowSec int
}

// Emit pass rate metrics on every batch
// Acceptable ranges (from config):
//   data_quality:  60-90% pass
//   edge:          5-20% pass
//   validation:    50-90% of edge
//   selection:     top-K from validated
```

### /status Command Response

```go
// Response to /status operator command
func buildStatusResponse(
    metrics VersionMetrics,
    mode string,
    activePositions int,
    cfg StatusConfig,
) string {
    return fmt.Sprintf(
        "[STATUS]\nmode: %s\nversion: %s\ntrades_24h: %d\npnl_24h: %+.1f%%\nwin_rate: %.1f%%\nfp_rate: %.1f%%\npositions: %d\nscan/s: %.0f\npass_rate: %.1f%%",
        mode,
        metrics.VersionID,
        metrics.TotalTrades,
        metrics.PnLTotalPct,
        metrics.WinRate*100,
        metrics.FalsePositiveRate*100,
        activePositions,
        metrics.ScanRatePerSec,
        metrics.PassRateByLayer["edge"]*100,
    )
}
```

### Anti-Patterns

```go
// ❌ Unstructured logging
fmt.Println("Trade executed, token:", token)
log.Printf("error: %v", err)

// ❌ Global-only metrics (not per version)
totalPnL += result.PnLUSD  // Without segmenting by version_id — invalid for A/B

// ❌ No system_event on mode change
currentMode = "exploration"  // Without emitting system_event → no audit trail

// ❌ Blocking pipeline on metrics collection
if err := recordMetric(...); err != nil { return err }  // metrics must be non-fatal

// ✅ Correct
logger.Info("mode transition",
    slog.String("from", oldMode),
    slog.String("to", newMode),
    slog.String("reason", reason),
)
emitSystemEvent(ctx, SysEventModeChange, ...)  // non-blocking, best-effort
```

### Anti-Patterns

```go
// ❌ Unstructured logging
fmt.Println("Trade executed, token:", token)
log.Printf("error: %v", err)

// ❌ Global-only metrics (not per version)
totalPnL += result.PnLUSD  // Without segmenting by version_id — invalid for A/B

// ❌ No system_event on mode change
currentMode = "exploration"  // Without emitting system_event → no audit trail

// ❌ Blocking pipeline on metrics collection
if err := recordMetric(...); err != nil { return err }  // metrics must be non-fatal

// ✅ Correct
logger.Info("mode transition",
    slog.String("from", oldMode),
    slog.String("to", newMode),
    slog.String("reason", reason),
)
emitSystemEvent(ctx, SysEventModeChange, ...)  // non-blocking, best-effort
```

---

### Event Bus Forensics

**Purpose:** Read-only inspection of the event bus to detect operational problems:
stalled workers, dead letter accumulation, queue depth spikes, and archival compliance.
Never modify events during forensics — forensics is always read-only.

```go
// Stalled worker detection: processing > 5 minutes
// SELECT event_id, event_type, worker_group, claimed_at
// FROM events WHERE processed = FALSE AND claimed_at < NOW() - INTERVAL '5 minutes'

type StalledWorkerAlert struct {
    EventID     string
    EventType   string
    WorkerGroup string
    ClaimedAt   string
    StalledSec  int64
}

func DetectStalledWorkers(
    ctx     context.Context,
    adapter database.Adapter,
    stalledThresholdSec int64,  // config: 300 (5 min)
) ([]StalledWorkerAlert, error) {
    return adapter.QueryStalledEvents(ctx, stalledThresholdSec)
}
```

```go
// Dead letter classification: classify unprocessable events
// Dead letter types:
//   transient  — temporary error (network/DB); retry eligible
//   permanent  — invalid schema or unrecoverable; archive
//   poison     — causes repeated crashes; quarantine + alert immediately
//   stale      — older than archival window (>7 days); archive

type DeadLetterClass string

const (
    DeadLetterTransient DeadLetterClass = "transient"
    DeadLetterPermanent DeadLetterClass = "permanent"
    DeadLetterPoison    DeadLetterClass = "poison"
    DeadLetterStale     DeadLetterClass = "stale"
)

func ClassifyDeadLetter(
    ev      database.Event,
    retries int,
    ageHours float64,
    cfg     DeadLetterConfig,  // max_retries=3, stale_hours=168 (7 days)
) DeadLetterClass {
    if ageHours > cfg.StaleHours     { return DeadLetterStale }
    if retries >= cfg.MaxRetries     { return DeadLetterPoison }   // failed too many times
    if ev.SchemaInvalid              { return DeadLetterPermanent }
    return DeadLetterTransient
}
```

```go
// Queue depth monitoring: track pending events per event_type
// Alert when queue depth exceeds threshold (signals consumer lag or crash)

type QueueDepthReport struct {
    EventType   string
    PendingCount int
    OldestPendingAgeMS int64
    AlertLevel  string  // "ok" | "warn" | "critical"
}

func MonitorQueueDepths(
    ctx     context.Context,
    adapter database.Adapter,
    cfg     QueueMonitorConfig,  // warn_threshold=100, critical_threshold=1000
) ([]QueueDepthReport, error) {
    return adapter.QueryQueueDepths(ctx, cfg)
}
```

```go
// Archival compliance: ensure no events older than 7 days remain unprocessed
// This prevents the events table from growing unbounded.
// Forensics ONLY — the forensics worker emits a system_event, it does NOT archive.

func CheckArchivalCompliance(
    ctx     context.Context,
    adapter database.Adapter,
    maxAgeDays int,  // config: 7
) (compliant bool, overdueCount int, err error) {
    return adapter.QueryOverdueEvents(ctx, maxAgeDays)
}
```

> Forensics workers are read-only consumers on the `events` table.
> They NEVER call `UPDATE events SET ...` or `DELETE FROM events`.
> All archival actions are performed by a dedicated archive worker, not forensics.

---

## Checklist

```
[ ] Event bus forensics: stalled workers detected (processing > 5min threshold)
[ ] Dead letter events classified (transient/permanent/poison/stale)
[ ] Queue depth per event_type monitored with warn/critical thresholds
[ ] Archival compliance checked (no events > 7 days unprocessed)
[ ] Forensics workers are READ-ONLY — never modify events
[ ] All metrics computed per VersionID (not just globally)
[ ] Structured logging only — no fmt.Printf, no log.Print
[ ] System events emitted for: mode changes, version promotions, halts, circuit breaks
[ ] Pipeline health monitor running on rolling window (5-15 min)
[ ] Starvation detected when pass_rate == 0 for full window
[ ] Overtrading detected when pass_rate > threshold
[ ] High rug rate triggers system_event + potential mode change
[ ] KPIs tracked: pnl, win_rate, fp_rate, fn_rate, slippage_error, latency_error
[ ] Pass rate tracked per layer (data_quality, edge, validation, selection)
[ ] /status response includes mode, version, 24h PnL, win_rate, fp_rate
[ ] Observability failures are non-fatal — never block the trading pipeline
[ ] Alert templates are structured (see tmpl constants above)
```

---

## References

- Architecture context: `docs/architecture-context/13_observability_finalization.md` § 16
- Architecture: `docs/architecture.md` § 5 (KPIs), § 7 (Operational Modes)
- Architecture context: `docs/architecture-context/1_global_control_loop.md`
- Roadmap: `docs/implementation_roadmap.md` Phase 6
- Config: `config/observability.yaml`
- `.github/skills/event-bus/SKILL.md` — Event bus worker patterns (SKIP LOCKED, offsets)
