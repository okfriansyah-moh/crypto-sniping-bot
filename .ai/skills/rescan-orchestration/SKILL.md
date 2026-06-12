# Rescan Orchestration Skill

## Purpose

Provide a deterministic, idempotent, profit-first pattern for **re-emitting
already-ingested tokens at fixed age bands** so the existing pipeline can
capture `MOMENTUM_EDGE` opportunities that the `NEW_LAUNCH_EDGE` path missed.

The rescan worker sits at **Layer 0.5** — between Layer 0 (raw ingestion)
and Layer 1 (DQ). It is a **pure DB reader plus event emitter**: no RPC,
no keys, no on-chain calls, no module-to-module imports.

Source plan: `docs/PLAN.md` (Phase 10 — `rescan-layer`).

---

## Rules

### R1 — Re-emit, never invent

The worker MUST re-emit `market_data_event` (the existing Layer 0 event
type). It MUST NOT introduce a new event type, a new DTO, or a new
contract under `contracts/`. Downstream stages (DQ → Features → Edge →
…) remain completely unchanged.

### R2 — Content-addressable EventID

```
EventID = SHA256(chain ‖ token_address ‖ band_name ‖ bucket_ts)[:16]

bucket_ts = floor(now_unix / interval_seconds) * interval_seconds
```

Two ticker iterations within the same bucket compute the same EventID.
The adapter's `InsertEvent` uses `ON CONFLICT (event_id) DO NOTHING` —
duplicates are silently dropped. This is the **only** acceptable
idempotency contract. Random IDs, timestamp-only IDs, or sequence
counters are forbidden (per `idempotency` skill).

### R3 — Eligibility filters are MANDATORY

Every rescan candidate MUST be filtered by the latest `data_quality`
sub-scores at SQL time, not application time:

| Filter                                                              | Default cap              | Reason                                               |
| ------------------------------------------------------------------- | ------------------------ | ---------------------------------------------------- |
| `dq.honeypot_score`                                                 | ≤ 0.5                    | Never re-expose capital to malicious contracts.      |
| `dq.rug_score`                                                      | ≤ 0.65                   | Same.                                                |
| `dq.buy_tax_bps`                                                    | ≤ 3000                   | Reject confiscatory tax structures.                  |
| `decision IN ('REJECT','PASS','RISKY_PASS')` (with `IncludePassed`) | varies                   | Recover both temporal rejects AND unselected passes. |
| `lifecycle.current_state`                                           | NOT IN open-position set | Prevent double-entry on existing positions.          |

These filters MUST be SQL-side (in the WHERE clause) — pulling all rows
into Go and filtering in memory is forbidden (defeats the index, wastes
DB bandwidth, and creates non-deterministic LIMIT behaviour).

### R4 — Mode-adaptive thresholds

The eligibility cap is selected per active operational mode (per
`operational-modes` skill):

| Mode          | Effect on eligibility               |
| ------------- | ----------------------------------- |
| `STRICT`      | Tighter caps (e.g. honeypot ≤ 0.30) |
| `BALANCED`    | Default caps                        |
| `EXPLORATION` | Looser caps (e.g. honeypot ≤ 0.60)  |

The mode is read from `system_state.mode` at the start of every tick. A
mode flip therefore takes effect at the next ticker fire — never
mid-tick.

### R5 — Per-token failure isolation

A failure inserting one token MUST NOT abort the band. A failure in one
band MUST NOT abort the tick. A failure in one tick MUST NOT abort the
worker. Bounded failure per `failure` and `monitoring-loop-engine` skills.
Log each failure as a `rescan_emit_failed` event and continue.

### R6 — No RPC, no keys, no on-chain calls

The rescan worker is a database-only consumer + event emitter. It MUST
NOT:

- Open RPC clients
- Hold or read private keys
- Submit transactions
- Call any other module's internals

This minimises attack surface (the worker has no privileged outbound
network access) and preserves the `modularity` invariant.

### R7 — Bounded ticker rate

`interval_seconds >= 10`. `MaxPerBandPerTick` is a hard cap on rows
returned per band per tick (default 200). These bounds prevent ticker
storms and protect Postgres from runaway queries.

### R8 — Determinism

Rows MUST be ordered by `(token_address ASC, ingested_at DESC)` and
deduplicated to the latest row per token (`SELECT DISTINCT ON
(token_address)`). Same DB state + same config + same wall-clock bucket
= identical emissions. No `RANDOM()`, no `ORDER BY 1`, no missing
`ORDER BY` before `LIMIT`.

### R9 — Trace continuity

Each rescan starts a **new** trace:

- `TraceID` = fresh UUID
- `CorrelationID` = same as `TraceID`
- `CausationID` = `""` (Layer 0 root convention)

Operators correlating original-vs-rescan flows use `token_address`, not
`trace_id`. Per `traceability` skill R2: one trace = one pipeline run.

### R10 — Diagnostic transport tag

Set `MarketDataDTO.Transport = "rescan_<band_name>"` (e.g.
`"rescan_30m"`) so log-reviewer (R4 detectors) and downstream analytics
can distinguish rescan flows from fresh ingestion.

---

## Inputs

- `database.Adapter` — read-only access to `market_data`, `data_quality`,
  `token_lifecycle`; write access via `InsertMarketData`, `InsertEvent`.
- `*config.Config.Rescan` — bands, eligibility, interval, mode overrides.
- `slog.Logger` — structured logger (per `observability` skill).
- Active strategy version (pinned at worker startup via
  `GetActiveStrategyVersion`).

## Outputs

- `market_data_event` rows in the `events` table, one per (token, band,
  bucket) tuple within eligibility.
- Persisted `market_data` rows (idempotent on event_id).
- Structured log events: `rescan_worker_started`,
  `rescan_worker_disabled`, `rescan_band_completed`,
  `rescan_emit_failed`, `rescan_tick_error`, `rescan_worker_heartbeat`.

## Anti-patterns (REJECT IMMEDIATELY)

| Anti-pattern                                      | Why                                                               |
| ------------------------------------------------- | ----------------------------------------------------------------- |
| Introducing a `rescan_event` event type           | Forbidden — would force changes to DQ/Features/Edge wiring.       |
| Introducing a `RescanCandidateDTO`                | Forbidden — re-use `MarketDataDTO` (R1).                          |
| Filtering rejects in Go after SELECT \*           | Defeats indexes; non-deterministic with `LIMIT`.                  |
| Random or sequence-counter EventID                | Breaks idempotency (R2).                                          |
| Calling RPC inside the rescan worker              | Violates R6; expand the probe worker instead.                     |
| Calling another module's internals                | Violates `modularity`; only adapter + contracts are allowed.      |
| `interval_seconds < 10`                           | Ticker storm risk (R7).                                           |
| Missing `Transport` tag on emitted DTO            | Breaks log-reviewer R4 detectors and analytics (R10).             |
| Enabled by default in `pipeline.yaml`             | Operators MUST opt in; safe-by-default rule.                      |
| Editing existing migrations to add rescan columns | `database/migrations/` is immutable; new additive migration only. |

---

## Examples

### Good — content-addressable EventID

```go
h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%d",
    dto.Chain, dto.TokenAddress, band.Name, bucketTs)))
newEventID := hex.EncodeToString(h[:])[:16]
```

### Bad — timestamp-only ID (NOT idempotent)

```go
newEventID := fmt.Sprintf("rescan-%d", time.Now().UnixNano()) // ❌ never reproducible
```

### Good — SQL-side eligibility

```sql
WHERE dq.honeypot_score <= $4 AND dq.rug_score <= $5
  AND (dq.decision = 'REJECT' OR ($7 AND dq.decision IN ('PASS','RISKY_PASS')))
```

### Bad — Go-side filtering after SELECT \*

```go
rows := adapter.GetAllMarketData(ctx)            // ❌ unbounded fetch
for _, r := range rows {
    if r.HoneypotScore > 0.5 { continue }        // ❌ defeats DB indexes
}
```

---

## Checklist (must pass before merge)

1. [ ] EventID is content-addressable (R2) and verified idempotent under
       same-bucket re-runs.
2. [ ] All eligibility filters are in the SQL WHERE clause (R3).
3. [ ] Mode override is read at the **start of each tick**, not at worker
       startup (R4).
4. [ ] Per-token / per-band / per-tick failure isolation in place (R5).
5. [ ] Worker has zero RPC clients, zero key access, zero direct module
       imports outside `contracts/`, `database/`, and `internal/app/config`
       (R6).
6. [ ] `interval_seconds >= 10` and `MaxPerBandPerTick` cap enforced (R7).
7. [ ] SQL has explicit `ORDER BY token_address, ingested_at DESC` and
       `DISTINCT ON (token_address)` (R8).
8. [ ] Each emitted DTO has a fresh trace*id, empty causation_id, and
       `Transport = "rescan*<band>"` (R9, R10).
9. [ ] `cfg.Rescan.Enabled` defaults to `false` in `pipeline.yaml`.
10. [ ] No new event types, no new DTOs, no contract file modifications.
11. [ ] Indexes added in a new migration (post-dated prefix); no existing
        migration modified.
12. [ ] `log-reviewer` PRS Dimension #1 still totals 10 pts when at least
        one rescan trace reaches `edge_decision`.

---

## Related Skills

- `event-bus` — append-only emission, SKIP LOCKED consumption
- `idempotency` — content-addressable IDs, ON CONFLICT DO NOTHING
- `determinism` — no randomness, deterministic SQL ordering
- `failure` — bounded retry, isolation across tokens/bands/ticks
- `monitoring-loop-engine` — periodic ticker patterns, kill-switch first
- `operational-modes` — STRICT / BALANCED / EXPLORATION transitions
- `traceability` — fresh trace per re-emission
- `profit-first` — Edge × AdaptationQuality justification
- `data-quality-engine` — DQ sub-scores consumed by eligibility filter
- `edge-detection` — MOMENTUM_EDGE downstream trigger
- `database-portability` — portable SQL, parameterized queries
- `migration-management` — additive index-only migrations