---
name: log-reviewer
type: skill
description: >
  Log and Telegram-dispatch review skill. Use when classifying structured bot
  logs, heartbeats, and Telegram dispatcher events as GOOD / DEGRADED / BAD /
  STUBBED / STUCK / NOISE; when diagnosing pipeline health from `stage_completed`
  trails, trace_id flows, and operator commands; and when producing a remediation
  plan that delegates fixes to the correct agents and skills per the
  skeleton-parallel architecture. This is a read-first, classify-then-route
  skill — it never silently mutates code.
---

# Log Reviewer Skill

## Purpose

Turn a stream of structured logs (`{"time":..,"level":..,"msg":..,...}`) and
Telegram dispatcher events into an **actionable health verdict** plus a
**remediation plan** routed to the correct agents/skills.

The skill answers four questions in order:

1. **Identify** — what is this log line / heartbeat / Telegram event?
2. **Classify** — `GOOD | DEGRADED | BAD | STUBBED | STUCK | NOISE`.
3. **Diagnose** — which layer, which DTO, which stage, which invariant is
   violated (per `docs/architecture.md` § 3 layers and § 2 backbone)?
4. **Plan** — concrete fix routed to the right agent/skill, with citation of
   the canonical doc section.

The skill is read-first. It never modifies code on its own — it produces a
verdict and a delegation manifest.

---

## Rules

### R1 — Only structured logs are reviewable

Per `observability` skill: every reviewable line MUST be valid JSON with at
minimum `time`, `level`, `msg`. Any unstructured line (e.g. `panic:`,
`runtime error`, raw `fmt.Println`) is by itself a finding (`code-quality`

- `observability` violation) — flag and route to `refactor` agent.

### R2 — Trace-id is the unit of analysis

Group log lines by `trace_id` (or `correlation_id` when present). One
`trace_id` represents one token's journey through the pipeline. Per
`traceability` skill: every event MUST carry `trace_id`, `correlation_id`,
`causation_id` (`event_id` of input), `version_id`. Missing any of these →
finding.

### R3 — Pipeline-stage completeness check

For each `trace_id`, the canonical sequence (per `docs/architecture.md` § 3
and § 1) is:

```
solana_ingestion_emitted | dex_pool_detected           # Layer 0
  → dq_decision                                        # Layer 1
  → features_extracted                                 # Layer 2
  → edge_decision                                      # Layer 3
  → probability_scored / slippage_estimated / latency  # Layer 4
  → validation_decision                                # Layer 5
  → selection_decision                                 # Layer 6
  → allocation_decision                                # Layer 7
  → execution_submitted / execution_confirmed          # Layer 8
  → position_opened → position_closed                  # Layer 9
  → learning_record_emitted                            # Layer 10
```

A trace that **terminates before validation_decision** is normal noise (most
candidates die in DQ or EV). A trace that **terminates AT validation with
REJECT for >95% of cases over a window** is a finding (Layer 5 is starving
Layers 6–10 — see operational-modes skill).

### R4 — Invariant detectors

The following exact patterns MUST be flagged when observed:

| Pattern                                                                                              | Class    | Layer | Route to                                          |
| ---------------------------------------------------------------------------------------------------- | -------- | ----- | ------------------------------------------------- |
| `level=ERROR` or `level=WARN` (any)                                                                  | BAD      | any   | refactor + module-builder                         |
| `level=PANIC`/`FATAL`                                                                                | BAD      | any   | doctor                                            |
| `dq_decision` with `risk_score=0` for **>95%** of tokens                                             | STUBBED  | 1     | data-quality-engine, anti-manipulation            |
| `features_extracted` with **identical** numeric values across distinct trace_ids                     | STUBBED  | 2     | feature-stability-checker                         |
| `probability_scored` with the **same float** across distinct trace_ids                               | STUBBED  | 4     | probability-modeling                              |
| `slippage_estimated` with constant p50/p95 across distinct trace_ids                                 | STUBBED  | 4     | probability-modeling, execution-quality-analyzer  |
| `edge_decision` with constant `edge_strength`/`edge_confidence`                                      | STUBBED  | 3     | edge-detection                                    |
| `validation_decision.reject_reason` containing `prob_join_timeout` or `*_join_timeout`               | BAD      | 5     | orchestrator, event-bus                           |
| `validation_decision.probability_used` ≠ matching `probability_scored.probability` for same trace_id | BAD      | 4–5   | orchestrator, event-bus                           |
| `output_event_id=""` after a non-terminal stage                                                      | DEGRADED | 1–9   | event-bus, orchestrator                           |
| Zero `selection_decision` / `allocation_decision` / `execution_*` over the window                    | STUCK    | 6–8   | operational-modes, edge-detection, capital-sizing |
| `*_heartbeat` with `events_emitted=0` AND `notifications_received>>0`                                | BAD      | 0     | dex-scanning, rpc-management                      |
| `*_heartbeat` with rising `dto_nil_skip` / `process_errors` / `rate_limit_skip`                      | DEGRADED | 0     | dex-scanning, rpc-management                      |
| `failed_tx / notifications_received > 0.20`                                                          | DEGRADED | 0     | rpc-management                                    |
| `telegram_destructive_command_executed` without prior confirmation log                               | BAD      | meta  | telegram-dispatcher                               |
| `telegram_command_received` with no matching response event within 5s                                | DEGRADED | meta  | telegram-dispatcher                               |
| Any direct Telegram API log line outside the dispatcher                                              | BAD      | meta  | telegram-dispatcher (bus-only rule)               |
| Stage X `stage_completed` count >> Stage X+1 input count (sustained ≥3 windows)                      | STUCK    | any   | event-bus (consumer_offsets lag)                  |
| Same `event_id` appears as input to a worker_group **>1×**                                           | BAD      | any   | idempotency, event-bus                            |
| Missing `version_id` on any event                                                                    | BAD      | any   | strategy-versioning, traceability                 |

### R5 — Stub detection heuristic

A field is **stubbed** when, across ≥ N=20 distinct `trace_id`s in a single
window, its value is constant or one of ≤2 discrete values, AND the field is
expected to vary with token characteristics (DQ score, features,
probability, slippage, edge strength).

Stubbed values produce uniform-input → uniform-output, which collapses
`Profit = Edge × Probability × Execution × Capital × DataQuality ×
AdaptationQuality` because every factor becomes constant. This is NEVER
acceptable in production mode — flag immediately, route to the layer's
canonical skill, and gate any STRICT/BALANCED operation behind a fix
(operational-modes).

### R6 — Telegram dispatcher rules (per `telegram-dispatcher` skill)

- Telegram messaging MUST go through the event bus → dispatcher; any
  `msg=telegram_*` event from a non-dispatcher worker is a finding.
- `telegram_command_received` MUST be followed by `telegram_response_sent`
  (or `telegram_command_rejected`) with same `correlation_id` within the
  configured ack window.
- Destructive commands (`/mode`, `/kill`, `/resume`) MUST log
  `telegram_destructive_command_executed` AND emit an audit event linking
  `issuer_id` to the resulting state change (`telegram_mode_changed` etc.).

### R7 — Severity scoring (deterministic, no randomness)

```
severity = max(per-finding severity)
  CRITICAL  any BAD touching capital/execution/destructive cmd, or
            kill-switch should fire (drawdown-protection)
  HIGH      STUBBED on Layer 1/4/5, validation prob mismatch,
            heartbeat events_emitted=0
  MEDIUM    DEGRADED, NOISE-spike, high reject-rate, latency creep
  LOW       isolated NOISE, single transient WARN
  OK        no findings, full pipeline observed end-to-end ≥1×
```

CRITICAL and HIGH MUST emit a `system_event` (per `observability` skill) and
trigger the kill-switch evaluation (per `drawdown-protection` skill).

### R8 — Plan output is a delegation manifest

The skill never edits code. It outputs a plan keyed by finding → agent →
skills → doc anchor. Implementation is performed by the routed agent
following its own contract (and `subagent-driven-development` always-on
skill).

### R9 — Confirmation Gate (MANDATORY, NON-SKIPPABLE)

**Every `/log-reviewer` invocation MUST end with an explicit user-facing
confirmation question before ANY plan item is executed.** This rule is
non-negotiable and applies regardless of severity, mode, or operator role.

Required behavior:

1. After emitting Sections 1 (Verdict), 2 (Findings), and 3 (Plan), the skill
   MUST emit a final **Confirmation Gate** block that explicitly asks the user
   whether to proceed with executing the proposed delegation plan.
2. The question MUST offer at minimum these choices:
   - `yes` — execute the full plan in dependency order
   - `no` — stop; do not invoke any agent or modify any code
   - `modify` — user will specify which finding ids to execute, skip, or reorder
   - `dry-run` — print the agent invocations that would run, without executing
3. The skill MUST NOT auto-execute, auto-delegate to subagents, or modify any
   file until the user has explicitly answered `yes` (or an equivalent
   affirmative such as `proceed`, `go`, `execute`, `approved`).
4. Silence, timeouts, or absence of an answer is treated as **`no`** (default
   deny). Never assume consent.
5. If the user answers `modify`, the skill MUST re-emit the Confirmation Gate
   with the revised plan and wait for `yes` again.
6. The Confirmation Gate is REQUIRED even when the verdict is `OK` and the
   plan is empty — in that case the question becomes "No findings — confirm
   you accept the OK verdict and want no further action? [yes / re-scan]".
7. Operators MAY pre-authorize via a documented config flag
   (`log_reviewer.auto_execute: true`) — in that case the gate still emits the
   plan and a notice that auto-execute is enabled, but no question is asked.
   Default MUST be `false`.

This rule is enforced verbatim per the operator instruction:
**"ALWAYS ASK USER IF WE WANT TO IMPLEMENT THE SUGGESTED PLAN OR NOT."**

---

## Inputs

- **Logs** — newline-delimited JSON (Docker `bot-1 |` prefix tolerated and
  stripped). May be raw stdin, a tailed file, or a captured snippet.
- **Window** — time range or last-N-lines being reviewed. Default: full
  attached snippet.
- **Mode hint** (optional) — current `STRICT | BALANCED | EXPLORATION`
  (per `operational-modes`). Affects severity gating only.
- **Known stubs allowlist** (optional, from `config/`) — fields the operator
  has explicitly marked as not-yet-implemented; downgrades STUBBED → DEGRADED
  for those fields only.

---

## Outputs

A single review object with three sections:

### 1. Verdict

```
status:    OK | DEGRADED | BAD | CRITICAL
window:    <start..end | last N lines>
traces:    <count>
end_to_end_traces: <count reaching execution_submitted or position_opened>
```

### 2. Findings (one row per detected pattern)

```
- id:            F-<n>
  pattern:       <R4 or R5 row>
  class:         GOOD | DEGRADED | BAD | STUBBED | STUCK | NOISE
  severity:      CRITICAL | HIGH | MEDIUM | LOW
  layer:         0..10 | meta
  evidence:
    - sample_event_ids: [...]   # ≤3
    - sample_trace_ids: [...]   # ≤3
    - count: <n>
    - example_line: "<verbatim>"
  hypothesis:    <single sentence>
  doc_anchor:    docs/architecture.md § <n>  | docs/<spec>.md § <n>
```

### 3. Plan (delegation manifest)

```
- finding: F-<n>
  agent:   <agent name from .github/agents/>
  skills:  [<skill1>, <skill2>, ...]
  action:  fix | adjust | investigate | scaffold-test | rollback
  preconditions: [<other finding ids that must be resolved first>]
  exit_criteria:
    - <observable log signal that proves the fix>
```

The plan MUST be ordered by dependency (preconditions first) and severity
(CRITICAL before HIGH before MEDIUM).

### 4. Confirmation Gate (MANDATORY — per R9)

```
confirmation_gate:
  question: |
    The above plan contains <N> delegation items across <M> agents and will
    modify code in: <comma-separated module paths>.
    Do you want me to execute this plan now?
  choices:
    - yes        # execute full plan in dependency order
    - no         # stop, do not invoke any agent
    - modify     # I will list finding ids to include / skip / reorder
    - dry-run    # print agent invocations only, no execution
  default_on_silence: no
  auto_execute_config_flag: log_reviewer.auto_execute (default false)
```

The skill MUST NOT proceed to execution until the user replies with an
affirmative (`yes` / `proceed` / `go` / `execute` / `approved`). Any other
answer — or no answer — means do nothing.

---

## Routing Matrix (finding → agent → skills)

| Finding kind                               | Agent          | Primary skills                                       |
| ------------------------------------------ | -------------- | ---------------------------------------------------- |
| Stubbed Layer-1 DQ                         | module-builder | data-quality-engine, anti-manipulation, dto          |
| Stubbed Layer-2 features                   | module-builder | feature-stability-checker, dto                       |
| Stubbed Layer-3 edge                       | module-builder | edge-detection, momentum-detector, signal-normalizer |
| Stubbed Layer-4 probability/slippage       | module-builder | probability-modeling, price-feed-integration         |
| Validation join-timeout / prob mismatch    | orchestrator   | event-bus, idempotency, failure, traceability        |
| Pipeline starvation (no select/alloc/exec) | orchestrator   | operational-modes, capital-sizing, edge-detection    |
| Heartbeat zero-emitted / dto_nil_skip      | module-builder | dex-scanning, rpc-management                         |
| Reject-rate >95% sustained                 | orchestrator   | operational-modes, edge-detection, learning-engine   |
| Telegram-direct or unacked command         | refactor       | telegram-dispatcher, observability, traceability     |
| Missing trace/version fields               | dto-guardian   | dto, traceability, strategy-versioning               |
| Duplicate event_id consumption             | orchestrator   | idempotency, event-bus                               |
| Drawdown breach signal                     | orchestrator   | drawdown-protection, operational-modes               |
| Unstructured / WARN / ERROR / panic        | refactor       | code-quality, coding-standards, observability        |
| Coverage gap (no end-to-end trace)         | test-builder   | test-generation, integration                         |
| Suspected schema drift                     | doctor         | dto, docs-sync, dependency-analysis                  |

When unsure which agent: route to `doctor` for triage. `doctor` may further
delegate per its own contract.

---

## Examples

### Example A — Stubs everywhere, pipeline starved (matches the attached snippet)

```yaml
verdict:
  status: BAD
  window: 2026-04-30T11:27:13Z..11:30:10Z (~3 min)
  traces: 47
  end_to_end_traces: 0

findings:
  - id: F-1
    pattern: probability_scored constant value across distinct trace_ids
    class: STUBBED
    severity: HIGH
    layer: 4
    evidence:
      sample_trace_ids: [80ba4623305d7014, 586274ee401203b6, 4f32b509a92f6d68]
      count: 18
      example_line: '"msg":"probability_scored","probability":0.9700552787112123'
    hypothesis: probability worker emits a hard-coded constant; model is not wired.
    doc_anchor: docs/architecture.md § 3.4 (Probability/Slippage/Latency Models)

  - id: F-2
    pattern: features_extracted identical (1, 0.7, 0.9) across distinct trace_ids
    class: STUBBED
    severity: HIGH
    layer: 2
    doc_anchor: docs/architecture.md § 3.2

  - id: F-3
    pattern: dq_decision PASS with risk_score=0 for 100% of tokens
    class: STUBBED
    severity: HIGH
    layer: 1
    doc_anchor: docs/architecture.md § 3.1

  - id: F-4
    pattern: edge_decision constant (0.91, 0.7)
    class: STUBBED
    severity: HIGH
    layer: 3

  - id: F-5
    pattern: validation_decision.probability_used=0.35 ≠ probability_scored=0.97
      with reject_reason containing "prob_join_timeout"
    class: BAD
    severity: CRITICAL
    layer: 5
    hypothesis:
      validation worker times out joining probability event; falls back
      to default 0.35 → all rejects with ev_bps=-1900. The bus join is
      broken or the worker reads before the producer commits.
    doc_anchor: docs/orchestrator_spec.md (event join / dependency wait);
      docs/architecture.md § 2.2 (event bus consumer_offsets)

  - id: F-6
    pattern: solana_ingestion_heartbeat raydium-v4 events_emitted=0 with
      notifications_received>>0 and high dto_nil_skip
    class: BAD
    severity: HIGH
    layer: 0
    doc_anchor: docs/architecture-context/* ingestion notes

  - id: F-7
    pattern: zero selection/allocation/execution events in window
    class: STUCK
    severity: HIGH
    layer: 6-8
    hypothesis: Layer 5 rejects 100%, so 6–10 are starved by design — root cause
      is F-5, not the downstream layers.

plan:
  - finding: F-5
    agent: orchestrator
    skills: [event-bus, idempotency, failure, traceability]
    action: fix
    preconditions: []
    exit_criteria:
      - validation_decision.probability_used matches probability_scored.probability
        for the same trace_id in ≥99% of cases
      - reject_reason no longer contains prob_join_timeout

  - finding: F-1
    agent: module-builder
    skills: [probability-modeling, price-feed-integration, dto]
    action: fix
    preconditions: [F-5]
    exit_criteria:
      - probability_scored shows >100 distinct values over a 1k-trace window

  - finding: F-2
    agent: module-builder
    skills: [feature-stability-checker, dto]
    action: fix
    preconditions: []

  - finding: F-3
    agent: module-builder
    skills: [data-quality-engine, anti-manipulation]
    action: fix
    preconditions: []
    exit_criteria:
      - dq_decision rejects ≥1 token with risk_score>0 in any 100-trace window
        on a fresh pump.fun feed

  - finding: F-4
    agent: module-builder
    skills: [edge-detection, momentum-detector, signal-normalizer]
    action: fix
    preconditions: [F-2]

  - finding: F-6
    agent: module-builder
    skills: [dex-scanning, rpc-management]
    action: investigate
    preconditions: []

  - finding: F-7
    agent: orchestrator
    skills: [operational-modes, edge-detection, capital-sizing]
    action: investigate
    preconditions: [F-1, F-2, F-3, F-4, F-5]
    exit_criteria:
      - at least one selection_decision and allocation_decision per hour
        once F-1..F-5 are resolved
```

### Example B — Healthy pipeline (reference shape, not from snippet)

```yaml
verdict:
  status: OK
  window: last 60m
  traces: 5_421
  end_to_end_traces: 12
findings: []
plan: []
```

---

## Checklist

Before emitting a review object:

- [ ] Every line either parsed as JSON or counted as an unstructured-log finding.
- [ ] Lines grouped by `trace_id` / `correlation_id`.
- [ ] All R4 invariant detectors evaluated against the window.
- [ ] R5 stub detector run for DQ, features, edge, probability, slippage.
- [ ] Heartbeat counters compared across consecutive heartbeats (drift detection).
- [ ] Telegram events checked for ack pairing and bus-only origin.
- [ ] Each finding cites a sample line, an event/trace id, and a doc anchor.
- [ ] Plan ordered by `preconditions` then severity.
- [ ] No code edits performed by this skill — only the verdict + plan emitted.
- [ ] Routing uses only agents in `.github/agents/` and skills in `.github/skills/`.
- [ ] **Confirmation Gate emitted** with explicit yes/no/modify/dry-run question (R9).
- [ ] No subagent invoked, no file modified before user replies affirmatively.

---

## Cross-references

- `observability` — the structured-logging contract this skill consumes.
- `traceability` — defines `trace_id` / `correlation_id` / `causation_id` / `version_id`.
- `event-bus` — explains worker join semantics and `consumer_offsets` lag.
- `operational-modes` — STRICT/BALANCED/EXPLORATION gating affects severity.
- `drawdown-protection` — kill-switch trigger evaluation on CRITICAL findings.
- `telegram-dispatcher` — bus-only Telegram contract.
- `docs/architecture.md` — canonical layer and pipeline definitions.
- `docs/orchestrator_spec.md` — failure handling, join logic, retries.
