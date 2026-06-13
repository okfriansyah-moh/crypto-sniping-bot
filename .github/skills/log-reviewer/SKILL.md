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

> **Skill section order and priority:**
>
> 1. **Rules** (R1–R9) — mandatory constraints; apply all
> 2. **Inputs** — parse and validate log stream first
> 3. **Outputs** — emit Verdict → Findings → Plan → Confirmation Gate in order
> 4. **Production Readiness Score** — compute after findings are complete
>
> When rules appear to conflict, R9 (Confirmation Gate) takes highest precedence
> over all others because it protects against unintended code mutation.

## Purpose

Turn a stream of structured logs (`{"time":..,"level":..,"msg":..,...}`) and
Telegram dispatcher events into an **actionable health verdict** plus a
**remediation plan** routed to the correct agents/skills.

The skill answers four questions in order:

1. **Identify** — what is this log line / heartbeat / Telegram event?
2. **Classify** — `GOOD | DEGRADED | BAD | STUBBED | STUCK | NOISE`.
3. **Diagnose** — which layer, which DTO, which stage, which invariant is
   violated (per `docs/reference/architecture.md` § 3 layers and § 2 backbone)?
4. **Plan** — concrete fix routed to the right agent/skill, with citation of
   the canonical doc section.

The skill is read-first. It never modifies code on its own — it produces a
verdict and a delegation manifest.

---

## Rules

### R1 — Only structured logs are reviewable

Per `observability` skill: every reviewable line MUST be valid JSON with at
minimum `time`, `level`, `msg`. Apply the following rules to non-conforming lines:

- **Fully unstructured** (e.g. `panic:`, `runtime error`, raw `fmt.Println`): flag as
  `code-quality` / `observability` violation and route to `refactor` agent.
- **Partially valid JSON** (parseable object but missing required fields, or a line
  where only a prefix is valid JSON): attempt to extract any parseable fields
  (`time`, `level`, `msg`, `trace_id`) and treat the remainder as an opaque string.
  Flag the line as `NOISE` and note which fields were unrecoverable. Route to
  `refactor` agent for structured-logging remediation.
- **Fully malformed JSON** (parse error on the entire line): treat as unstructured;
  include the raw line as `example_line` in the finding.

### R2 — Trace-id is the unit of analysis

Group log lines by `trace_id` (or `correlation_id` when present). One
`trace_id` represents one token's journey through the pipeline. Per
`traceability` skill: every event MUST carry `trace_id`, `correlation_id`,
`causation_id` (`event_id` of input), `version_id`. Missing any of these →
finding.

### R3 — Pipeline-stage completeness check

For each `trace_id`, the canonical sequence (per `docs/reference/architecture.md` § 3
and § 1) is:

```
solana_ingestion_emitted | dex_pool_detected | rescan_band_completed   # Layer 0 / 0.5
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

Rescanned traces (events with `transport="rescan_<band>"`) start at Layer
0.5 and follow the same canonical sequence from Layer 1 onward. They are
identified by a fresh `trace_id` but share `token_address` with their
original Layer-0 ingestion event.

A trace that **terminates before validation_decision** is normal noise (most
candidates die in DQ or EV). A trace that **terminates AT validation with
REJECT for >95% of cases over a window** is a finding (Layer 5 is starving
Layers 6–10 — see operational-modes skill).

### R4 — Invariant detectors

The following exact patterns MUST be flagged when observed:

| Pattern                                                                                                                               | Class    | Layer | Route to                                                                                                      |
| ------------------------------------------------------------------------------------------------------------------------------------- | -------- | ----- | ------------------------------------------------------------------------------------------------------------- |
| `level=ERROR` or `level=WARN` (any)                                                                                                   | BAD      | any   | refactor + module-builder                                                                                     |
| `level=PANIC`/`FATAL`                                                                                                                 | BAD      | any   | doctor                                                                                                        |
| `dq_decision` with `risk_score=0` for **>95%** of tokens                                                                              | STUBBED  | 1     | data-quality-engine, anti-manipulation                                                                        |
| `dq_decision` with `reject_reason=high_total_supply` for **>50%** of tokens AND no legit-supply tokens passing                        | DEGRADED | 1     | data-quality-engine (raise `max_total_supply` or check ingestion populates `total_supply_known`)              |
| `features_extracted` with **identical** numeric values across distinct trace_ids                                                      | STUBBED  | 2     | feature-stability-checker                                                                                     |
| `probability_scored` with the **same float** across distinct trace_ids                                                                | STUBBED  | 4     | probability-modeling                                                                                          |
| `slippage_estimated` with constant p50/p95 across distinct trace_ids                                                                  | STUBBED  | 4     | probability-modeling, execution-quality-analyzer                                                              |
| `edge_decision` with constant `edge_strength`/`edge_confidence`                                                                       | STUBBED  | 3     | edge-detection                                                                                                |
| `validation_decision.reject_reason` containing `prob_join_timeout` or `*_join_timeout`                                                | BAD      | 5     | orchestrator, event-bus                                                                                       |
| `validation_decision.probability_used` ≠ matching `probability_scored.probability` for same trace_id                                  | BAD      | 4–5   | orchestrator, event-bus                                                                                       |
| `output_event_id=""` after a non-terminal stage                                                                                       | DEGRADED | 1–9   | event-bus, orchestrator                                                                                       |
| Zero `selection_decision` / `allocation_decision` / `execution_*` over the window                                                     | STUCK    | 6–8   | operational-modes, edge-detection, capital-sizing                                                             |
| `*_heartbeat` with `events_emitted=0` AND `notifications_received>>0`                                                                 | BAD      | 0     | dex-scanning, rpc-management                                                                                  |
| `*_heartbeat` with rising `dto_nil_skip` / `process_errors` / `rate_limit_skip`                                                       | DEGRADED | 0     | dex-scanning, rpc-management                                                                                  |
| `failed_tx / notifications_received > 0.20`                                                                                           | DEGRADED | 0     | rpc-management                                                                                                |
| `telegram_destructive_command_executed` without prior confirmation log                                                                | BAD      | meta  | telegram-dispatcher                                                                                           |
| `telegram_command_received` with no matching response event within 5s                                                                 | DEGRADED | meta  | telegram-dispatcher                                                                                           |
| Any direct Telegram API log line outside the dispatcher                                                                               | BAD      | meta  | telegram-dispatcher (bus-only rule)                                                                           |
| Stage X `stage_completed` count >> Stage X+1 input count (sustained ≥3 windows)                                                       | STUCK    | any   | event-bus (consumer_offsets lag)                                                                              |
| Same `event_id` appears as input to a worker_group **>1×**                                                                            | BAD      | any   | idempotency, event-bus                                                                                        |
| Missing `version_id` on any event                                                                                                     | BAD      | any   | strategy-versioning, traceability                                                                             |
| `rescan_worker_started` absent for ≥10 min while `cfg.rescan.enabled=true`                                                            | STUCK    | 0.5   | rescan-orchestration, event-bus                                                                               |
| `rescan_band_completed.candidates=0` for **all** bands sustained ≥30 min                                                              | DEGRADED | 0.5   | rescan-orchestration, data-quality-engine (eligibility too tight)                                             |
| `rescan_emit_failed` rate > 5% of candidates per band                                                                                 | DEGRADED | 0.5   | rescan-orchestration, event-bus                                                                               |
| `rescan_worker_started` present BUT `transport="rescan_*"` events = 0 for the full window (rescan enabled, worker alive, zero output) | STUCK    | 0.5   | rescan-orchestration, data-quality-engine (no tokens passing eligibility gate OR tick loop silently erroring) |
| Same `(token_address, band)` re-emitted within one bucket window                                                                      | BAD      | 0.5   | idempotency, rescan-orchestration                                                                             |
| `transport="rescan_*"` market_data_event with no downstream `dq_decision` within 60 s                                                 | STUCK    | 0.5→1 | event-bus, data-quality-engine                                                                                |
| `MOMENTUM_EDGE` count from rescanned traces >> `NEW_LAUNCH_EDGE` count from fresh traces (sustained 24 h)                             | DEGRADED | 3     | edge-detection (NEW_LAUNCH window may be mis-tuned)                                                           |

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
   MUST call the **`vscode_askQuestions`** tool with a structured question that
   presents the confirmation gate as an interactive VS Code dialog — NOT as a
   free-text chat message. This gives the operator an in-editor prompt with
   clearly labelled option buttons rather than requiring them to type a reply.

2. The `vscode_askQuestions` call MUST include at minimum these options:
   - `yes` — execute the full plan in dependency order
   - `no` — stop; do not invoke any agent or modify any code
   - `modify` — user will specify which finding ids to execute, skip, or reorder
   - `dry-run` — print the agent invocations that would run, without executing
   - `other` — user provides a free-text instruction; the skill interprets it
     and re-emits the Confirmation Gate with the adjusted plan before acting

3. The question text MUST summarise the plan scope:

   ```
   header: "Execute remediation plan?"
   question: "The above plan contains <N> delegation items across <M> agents
              and will modify: <comma-separated module paths>.
              Do you want to execute this plan now?"
   options:
     - label: "yes"        description: "Execute full plan in dependency order"
     - label: "no"         description: "Stop — do not modify any code"
     - label: "modify"     description: "I will specify which finding ids to change"
     - label: "dry-run"    description: "Print agent invocations only, no execution"
     - label: "other"      description: "Describe a custom instruction (free text)"
   allowFreeformInput: true
   ```

   When the user selects `other` OR submits freeform text alongside any option,
   the skill MUST:
   a. Read the freeform input as a plain-language instruction.
   b. Apply it to the plan (add/remove/reorder findings, change agents, etc.).
   c. Re-emit the revised plan (Section 3) and call `vscode_askQuestions` again
   with the same gate — this loop repeats until `yes` or `no` is received.

4. The skill MUST NOT auto-execute, auto-delegate to subagents, or modify any
   file until the `vscode_askQuestions` call returns `yes` (or the user types
   an equivalent affirmative such as `proceed`, `go`, `execute`, `approved`).
   **The one permitted exception is pre-authorization:** if the operator has
   explicitly set `log_reviewer.auto_execute: true` in config (default `false`),
   the Confirmation Gate is still shown and `vscode_askQuestions` is still
   called — the operator retains the ability to cancel. Auto-execute means the
   plan proceeds automatically _only if the operator does not cancel_ within the
   configured timeout. It does NOT mean the gate is skipped.
5. If the user selects `modify`, the skill MUST re-emit the Confirmation Gate
   (another `vscode_askQuestions` call) with the revised plan and wait for
   `yes` again.
6. The Confirmation Gate is REQUIRED even when the verdict is `OK` and the
   plan is empty — in that case the question becomes "No findings — confirm
   you accept the OK verdict and want no further action?" with options
   `["yes — accept OK", "re-scan"]`.
7. Operators MAY pre-authorize via a documented config flag
   (`log_reviewer.auto_execute: true`) — in that case the gate still emits the
   plan and a notice that auto-execute is enabled, but `vscode_askQuestions`
   is still called so the operator can cancel. Default MUST be `false`.

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
prs:       <0-100>   # Production Readiness Score — see § Production Readiness Score
prs_tier:  BLOCKED | CAUTION | LAUNCH_ALLOWED | SUSTAINABLE | OPTIMIZED
open_critical: <count of unresolved CRITICAL/HIGH findings>
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
  doc_anchor:    docs/reference/architecture.md § <n>  | docs/<spec>.md § <n>
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
# Implemented via vscode_askQuestions tool (NOT free-text chat output).
# Call parameters:
questions:
  - header: "Execute remediation plan?"
    question: |
      The above plan contains <N> delegation items across <M> agents and will
      modify code in: <comma-separated module paths>.
      Do you want to execute this plan now?
    options:
      - label: "yes"      description: "Execute full plan in dependency order"
      - label: "no"       description: "Stop — do not modify any code"
      - label: "modify"   description: "I will specify which finding ids to change"
      - label: "dry-run"  description: "Print agent invocations only, no execution"
      - label: "other"    description: "Describe a custom instruction (free text)"
    allowFreeformInput: true
```

The skill MUST NOT proceed to execution until `vscode_askQuestions` returns
`yes` (or an equivalent affirmative). Selecting `other` or submitting freeform
text causes the skill to interpret the instruction, revise the plan, and
re-present the Confirmation Gate — the loop repeats until `yes` or `no`.

---

## Production Readiness Score (PRS)

The PRS is a **deterministic score from 0–100** computed from 10 equally-weighted
dimensions (10 pts each). It is emitted in every review verdict alongside `status`.
It is the single answer to "are we ready to trade?" — no subjective judgment,
no moving target.

### Scoring Rubric

| #   | Dimension                                                      | 10 pts                                                                                         | 5 pts                                   | 0 pts                                                      |
| --- | -------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | --------------------------------------- | ---------------------------------------------------------- |
| 1   | **Pipeline completeness** — all stages L0–L10 observed ≥1×     | ≥1 end-to-end trace (L0→L10) AND (rescan disabled OR ≥1 rescanned trace reaches edge_decision) | L0–L5 observed, L6–L10 absent           | L0–L5 incomplete                                           |
| 2   | **Data Quality** — DQ detectors not constant-zero              | `risk_score` varies AND ≥1 reject in window                                                    | detectors present, all pass             | R5 STUBBED on DQ (`risk_score=0` on ≥95% tokens)           |
| 3   | **Feature signals** — no constant features                     | All 8 feature fields vary across trace_ids                                                     | ≥5 vary                                 | R5 STUBBED on features (identical values across trace_ids) |
| 4   | **Probability model** — real model, not fixed prior            | `probability_used` varies AND ≤1% prob_join_timeout                                            | varies but mismatch < 5%                | constant 0.35 OR `prob_join_timeout` on ≥5%                |
| 5   | **Slippage model** — p50/p95 not constant                      | p50 and p95 both vary across trace_ids                                                         | one varies                              | both constant (R5 STUBBED)                                 |
| 6   | **Capital safety** — kill-switch, DLQ, drawdown wired          | Zero CRITICAL capital/execution findings                                                       | Only LOW capital findings               | Any CRITICAL finding touching capital/execution            |
| 7   | **Execution engine** — ≥1 confirmed execution observed         | `execution_confirmed` ≥1 in window                                                             | `execution_submitted` ≥1 but no confirm | Zero execution events while wallet is funded               |
| 8   | **Learning / adaptation** — ≥30 real trade outcomes            | `learning_record_emitted` count ≥30 in DB history                                              | 1–29 records                            | Zero records (cold start)                                  |
| 9   | **Probe coverage** — at least one live `*Known=true` flag      | ≥1 `*Known` flag non-zero (live RPC probe running)                                             | Probes deployed but returning zero      | No probes wired, all flags false                           |
| 10  | **Live P&L evidence** — positive rolling EV over ≥30 positions | `mean(ev_bps) > 0` over last 30 closed positions                                               | Insufficient data (< 30 closed)         | Negative rolling EV over ≥30 closed positions              |

### PRS Tier Thresholds

```
0  – 49   BLOCKED        — Do not trade. Significant code defects present.
           At least one CRITICAL or HIGH finding exists in the current window.
           All must be resolved before any threshold can advance.

50 – 64   CAUTION        — Code is structurally sound. Pipeline confirmed L0–L5.
           Safe for paper-trading or micro-size ($1–$5) observation only.
           No CRITICAL findings; HIGH findings may be present.

65 – 79   LAUNCH_ALLOWED — All CRITICAL/HIGH code defects resolved.
           Safe to execute real trades at minimum size ($5–$10).
           THIS IS THE PROFITABLE-AND-READY-TO-LAUNCH THRESHOLD.
           Operational calibration (dimensions 8, 9, 10) still in progress —
           this is expected and does not block launch.

80 – 89   SUSTAINABLE    — Model calibrated on real outcomes (N≥30).
           Rolling EV positive. Slippage α self-calibrated. Normal operating mode.

90 – 100  OPTIMIZED      — All infrastructure deployed, full probe coverage,
           learning engine active with verified positive expectancy.
           Continue operating; tune parameters only.
```

### PRS Is Final at LAUNCH_ALLOWED

When PRS ≥ 65 AND `open_critical = 0`, the log-reviewer skill MUST declare:

```
VERDICT: PROFITABLE_AND_READY_TO_LAUNCH
prs: <65–100>
prs_tier: LAUNCH_ALLOWED | SUSTAINABLE | OPTIMIZED
message: "All CRITICAL/HIGH code defects resolved. Pipeline confirmed L0–L5.
          Remaining items (dimensions 8/9/10) are operational calibration —
          they self-improve as trades execute. No further code fixes required
          to begin trading. Fund wallets and start."
```

This declaration is **terminal** — once emitted, the log-reviewer skill shifts
role from "find and fix" to "monitor and alert". It will still run on every log
window and flag new CRITICAL/HIGH regressions, but it will not produce fix plans
for the operational calibration items (dimensions 8, 9, 10) because those are
not code defects.

---

## Tolerable vs. Non-Tolerable in Production

These rules define the hard boundary between "acceptable operational state" and
"stop trading immediately." They apply once PRS ≥ 65 (LAUNCH_ALLOWED).

### Tolerable (do NOT produce a fix plan — monitor only)

| Condition                                                | Why tolerable                                                                                                          | Action                                            |
| -------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------- |
| `*Known` flags = false (probes not yet deployed)         | DQ still runs static checks + mode profile. Mitigated by STRICT mode.                                                  | Monitor; deploy probes in own session when ready. |
| Slippage α=1.0 (not yet calibrated from fills)           | Conservative overestimate — rejects borderline trades, never accepts bad ones. Auto-calibrates via `alpha_aggregator`. | Monitor; no code change needed.                   |
| Learning cold-start (< N=30 closed trades)               | Prior priors are safe defaults. Model does not degrade, just does not improve yet.                                     | Monitor; self-resolves as trades close.           |
| `max_open_positions=1`                                   | Limiting but safe. Prevents runaway exposure during cold-start.                                                        | Increase config gradually as P&L confirms.        |
| Single transient WARN line                               | Isolated LOW finding.                                                                                                  | Log and move on.                                  |
| PRS between 65–79                                        | LAUNCH_ALLOWED tier — expected during early live operation.                                                            | Normal; improves automatically.                   |
| DQ `risk_score` = 0 on all tokens in a < 20-token window | Below R5 stub-detection threshold (N=20). Not enough samples.                                                          | Wait for N=20 window; re-evaluate.                |
| `ev_bps` near threshold (100–300 bps) but positive       | Thin edge — valid. Real model is active.                                                                               | Monitor slippage calibration.                     |

### Non-Tolerable (MUST produce a fix plan — stop or gate trading)

| Condition                                                                                                                                | Class    | Severity | Required action                                                                                                                                                                                                                                                                     |
| ---------------------------------------------------------------------------------------------------------------------------------------- | -------- | -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `probability_used=0.35` (fallback) on ANY token when bot is live                                                                         | BAD      | CRITICAL | Stop trading. Fix regression in probability join or confidence gate.                                                                                                                                                                                                                |
| `ev_bps < 0` on majority (>50%) of ACCEPT decisions                                                                                      | BAD      | CRITICAL | Stop trading. EV gate broken or prior config corrupted.                                                                                                                                                                                                                             |
| `slippage_estimated` p50/p95 constant across all tokens (R5 STUBBED)                                                                     | STUBBED  | HIGH     | Pause new entries. CPMM formula broken or reserve data missing.                                                                                                                                                                                                                     |
| Any `level=PANIC` or `level=FATAL`                                                                                                       | BAD      | CRITICAL | Stop trading immediately. Run `doctor` agent.                                                                                                                                                                                                                                       |
| `validation_decision: REJECT` for >95% of tokens over 3+ windows                                                                         | STUCK    | HIGH     | Pipeline starved. Investigate mode transitions + thresholds.                                                                                                                                                                                                                        |
| `execution_confirmed=0` after >10 ACCEPT decisions with funded wallet                                                                    | STUCK    | HIGH     | Wallet config broken or RPC endpoint down.                                                                                                                                                                                                                                          |
| `risk_score=0` on ≥95% of tokens over N≥20 window (R5 STUBBED)                                                                           | STUBBED  | HIGH     | DQ detectors regressed to stub state.                                                                                                                                                                                                                                               |
| `probability_scored` constant across ≥20 distinct trace_ids (R5 STUBBED)                                                                 | STUBBED  | HIGH     | Probability model regressed to stub.                                                                                                                                                                                                                                                |
| Negative rolling EV over ≥30 closed positions (dimension 10 = 0 pts)                                                                     | BAD      | HIGH     | Strategy is losing. Pause, run `learning-engine` + `loss-pattern-analyzer`.                                                                                                                                                                                                         |
| Any duplicate `event_id` consumed by same worker group >1×                                                                               | BAD      | CRITICAL | Idempotency broken. Stop trading.                                                                                                                                                                                                                                                   |
| `drawdown > daily_loss_limit` (kill-switch not firing)                                                                                   | BAD      | CRITICAL | Kill switch malfunction. Stop manually. Fix `drawdown-protection`.                                                                                                                                                                                                                  |
| Missing `version_id` on any event                                                                                                        | BAD      | HIGH     | Strategy attribution broken. Fix before adding capital.                                                                                                                                                                                                                             |
| Same `(token_address, band)` re-emitted by rescan within one window                                                                      | BAD      | CRITICAL | Rescan idempotency broken. Fix `rescan-orchestration` + `idempotency`.                                                                                                                                                                                                              |
| `rescan_emit_failed` rate > 5% of candidates in any band                                                                                 | DEGRADED | HIGH     | Rescan→pipeline handoff failing. Fix `event-bus` + `rescan-orchestration`.                                                                                                                                                                                                          |
| Rescan enabled (`cfg.rescan.enabled=true`) AND `rescan_worker_started` seen BUT zero `transport="rescan_*"` events over a ≥10 min window | STUCK    | HIGH     | Rescan worker alive but producing no output. Inspect eligibility thresholds (`min_age_seconds`, `max_age_seconds`, band windows) in `config/pipeline.yaml`; check `rescan_tick_error` rate; verify `dex_pool_detected` / `solana_ingestion_emitted` are populating the token table. |

---

## Termination Contract

**The fixing loop has a hard end.** This contract defines it.

### The Loop Ends When

```
PRS ≥ 65
AND open_critical = 0        # zero CRITICAL/HIGH findings in current window
AND all_code_stubs_resolved  # R5 finds no STUBBED fields above threshold
```

When these three conditions are simultaneously true, the skill emits
`VERDICT: PROFITABLE_AND_READY_TO_LAUNCH` and transitions to monitor-only mode.

### What Remains After the Loop Ends (NOT code fixes)

| Item                                        | Nature                                               | How it resolves                                          |
| ------------------------------------------- | ---------------------------------------------------- | -------------------------------------------------------- |
| Probe deployment (PRS dimension 9)          | Infrastructure — one-time deployer task              | Wire RPC client + probe interface once per chain; done   |
| Slippage α calibration (PRS dimension 5→10) | Operational — auto-calibrates via `alpha_aggregator` | Self-resolves as realized fills accumulate               |
| Learning cold-start (PRS dimension 8)       | Operational — self-improving by design               | Self-resolves after N≥30 trades close                    |
| Live P&L evidence (PRS dimension 10)        | Capital decision — operator chooses when to fund     | Cannot be resolved with code; requires real money + time |

None of these four items produce a fix plan entry. They are tracked via PRS score
movement only. The skill reports their current state in the verdict but does NOT
route them to any agent.

### Prohibited Loop Patterns

The following behaviors are **explicitly forbidden** to prevent an endless loop:

1. **Re-opening closed findings**: A finding marked as resolved (its `exit_criteria`
   log signal was observed) MUST NOT be re-raised in the same class/severity unless
   a regression is confirmed. Confirm regression with ≥3 consecutive windows before
   re-flagging.

2. **Cascading micro-fixes**: The skill MUST NOT produce a new plan containing only
   LOW findings when PRS ≥ 65 and `open_critical = 0`. LOW findings in LAUNCH_ALLOWED
   tier are noted in the verdict but do not generate plan entries.

3. **Blocking launch on operational items**: The skill MUST NOT produce a plan entry
   for PRS dimensions 8, 9, or 10 (learning cold-start, probe deployment, live P&L).
   These are NOT code defects and MUST NOT block trading.

4. **Moving the launch threshold**: The LAUNCH_ALLOWED threshold is fixed at PRS=65.
   It MUST NOT be silently raised by any future plan or skill update without an explicit
   operator decision recorded in `docs/ops/PROGRESS_REPORT.md`.

### Current PRS (as of 2026-05-01)

```
prs: 58
prs_tier: CAUTION
delta_to_launch: 7 points needed

Breakdown:
  D1 pipeline completeness:  8/10  (L0–L5 confirmed; L6–L10 await first real execution)
  D2 data quality:           6/10  (detectors live; *Known flags false — probes not deployed)
  D3 feature signals:        8/10  (z-score normalizer + on-chain signals live; cold-start conf=0.4)
  D4 probability model:      6/10  (real logistic model active; not yet calibrated on real outcomes)
  D5 slippage model:         6/10  (CPMM formula live; α=1.0 stub — auto-calibrates from fills)
  D6 capital safety:         9/10  (CRIT-1+CRIT-2 fixed; kill-switch, DLQ, drawdown wired)
  D7 execution engine:       8/10  (wallet sharding, nonce, circuit breaker wired; no live exec yet)
  D8 learning / adaptation:  5/10  (all code present; zero real trade outcomes yet)
  D9 probe coverage:         2/10  (HoneypotSimProbe = no-op template; probes deployer task)
  D10 live P&L evidence:     0/10  (no real trades executed yet)

Path to LAUNCH_ALLOWED (PRS=65):
  Deploy ≥1 Solana on-chain probe (getAccountInfo creator authority check) → D9: 2→5  (+3)
  Observe first 5 live executions → D7: 8→10                                         (+2)
  Observe first execution_confirmed → D1: 8→9                                         (+1)
  Total delta: +6 → PRS=64  (one more point from any source → 65 = LAUNCH_ALLOWED)

open_critical: 0   # all F-1 through F-9 findings from prior sessions resolved
```

This snapshot is informational. The authoritative PRS is always computed live
from the log window being reviewed, using the rubric above.

---

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
    doc_anchor: docs/reference/architecture.md § 3.4 (Probability/Slippage/Latency Models)

  - id: F-2
    pattern: features_extracted identical (1, 0.7, 0.9) across distinct trace_ids
    class: STUBBED
    severity: HIGH
    layer: 2
    doc_anchor: docs/reference/architecture.md § 3.2

  - id: F-3
    pattern: dq_decision PASS with risk_score=0 for 100% of tokens
    class: STUBBED
    severity: HIGH
    layer: 1
    doc_anchor: docs/reference/architecture.md § 3.1

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
    doc_anchor: docs/reference/orchestrator_spec.md (event join / dependency wait);
      docs/reference/architecture.md § 2.2 (event bus consumer_offsets)

  - id: F-6
    pattern: solana_ingestion_heartbeat raydium-v4 events_emitted=0 with
      notifications_received>>0 and high dto_nil_skip
    class: BAD
    severity: HIGH
    layer: 0
    doc_anchor: docs/archive/architecture-context/* ingestion notes

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
- [ ] **PRS computed** from all 10 dimensions and emitted in the verdict.
- [ ] **PRS tier declared** (BLOCKED / CAUTION / LAUNCH_ALLOWED / SUSTAINABLE / OPTIMIZED).
- [ ] **`open_critical` count** emitted alongside PRS.
- [ ] If PRS ≥ 65 AND `open_critical = 0`: emit `VERDICT: PROFITABLE_AND_READY_TO_LAUNCH`.
- [ ] Tolerable conditions (per § Tolerable vs. Non-Tolerable) NOT added to the fix plan.
- [ ] PRS dimensions 8, 9, 10 (operational calibration) NOT added to the fix plan.
- [ ] No previously-resolved finding re-raised without ≥3 consecutive window confirmation.
- [ ] No plan entries generated for LOW findings when PRS ≥ 65 AND `open_critical = 0`.

---

## Cross-references

- `observability` — the structured-logging contract this skill consumes.
- `traceability` — defines `trace_id` / `correlation_id` / `causation_id` / `version_id`.
- `event-bus` — explains worker join semantics and `consumer_offsets` lag.
- `operational-modes` — STRICT/BALANCED/EXPLORATION gating affects severity.
- `drawdown-protection` — kill-switch trigger evaluation on CRITICAL findings.
- `telegram-dispatcher` — bus-only Telegram contract.
- `docs/reference/architecture.md` — canonical layer and pipeline definitions.
- `docs/reference/orchestrator_spec.md` — failure handling, join logic, retries.
- `docs/ops/PROGRESS_REPORT.md` — session history; PRS snapshot is updated here after each session.
- § Production Readiness Score — deterministic 0–100 rubric; LAUNCH_ALLOWED at PRS=65.
- § Tolerable vs. Non-Tolerable — hard boundary between monitor-only and stop-trading.
- § Termination Contract — the explicit conditions under which the fix loop ends.
