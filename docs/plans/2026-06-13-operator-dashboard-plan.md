# PLAN.md — Operator Dashboard Monorepo Split & Seamless Integration

> **Version:** 1.0
> **Date:** 2026-06-13
> **Author:** Plan Management (from `docs/specs/2026-06-10-operator-dashboard-design.md` + mockup `docs/mockups/operator-dashboard.html`)
> **Status:** Completed (2026-06-14)
> **Source of Truth:** `docs/specs/2026-06-10-operator-dashboard-design.md` · UI artifact: `docs/mockups/operator-dashboard.html`
> **Pipeline Layers Affected:** Platform (operator UX, HTTP API, deploy topology) — **no L0–L10 pipeline logic changes in Phases 1–3**
> **Profit Factors Affected:** None directly (infra/operator UX) — indirect: faster incident response preserves Execution + AdaptationQuality

---

## 1. Goal

Split the repository into three deployable units — `frontend-dashboard/`, `backend-dashboard/`, and `sniper-bot/` — while delivering an operator dashboard that matches `docs/mockups/operator-dashboard.html`. The refactor must be **seamless**: the existing `crypto-sniping-bot serve` pipeline, Telegram integration, event bus, and database semantics continue working unchanged until each phase is explicitly cut over. Phase 1 ships a **read-only dashboard** (Approach A from the design spec); control-plane writes (mode, kill, resume) follow only after read-path parity with Telegram is proven.

**Phased delivery:**

| Phase           | Scope                                                               | Sniper impact                         |
| --------------- | ------------------------------------------------------------------- | ------------------------------------- |
| **1 — Extract** | Shared `internal/operator/` query layer; Telegram rewired to use it | Zero behavior change                  |
| **2 — API**     | `backend-dashboard` process: read-only REST on port 8090            | Sniper still serves `/health` on 8080 |
| **3 — UI**      | `frontend-dashboard` SPA polling backend API                        | None                                  |
| **4 — Split**   | Physical folder move to `sniper-bot/`; Docker Compose 3 services    | `serve` entry moves path only         |
| **5 — Control** | Operator commands via event bus (mode/kill/resume)                  | New consumer worker in sniper         |

**Why:** Operators today depend on Telegram (`/status`, `/pnl`, `/pipeline`, `/dq`) and shell scripts (`gate_review_collect.sh`). A unified dashboard reduces operational blind spots without coupling UI to the hot trading path.

**Profit factor(s) affected:** None directly — this is operator infrastructure. Indirect benefit: quicker detection of pipeline stalls (DataQuality / AdaptationQuality diagnostics) reduces downtime.

### 1.1 Seamless Refactor Principles (non-negotiable)

1. **Strangler fig** — add `backend-dashboard` and `frontend-dashboard` alongside the monolith; do not rewrite the pipeline.
2. **Extract before move** — create `internal/operator/` and prove Telegram parity **before** moving files into `sniper-bot/`.
3. **Read-only first** — no dashboard write path until Tasks 1–21 are green and operators sign off on read parity.
4. **Single database** — both processes use the same PostgreSQL DSN; migrations remain centralized in `shared/database/migrations/`.
5. **Sniper owns hot path** — private keys, RPC, execution, ingestion stay in `sniper-bot` only.
6. **Commands via event bus** — dashboard writes emit `operator_command_event`; sniper consumes — same audit model as Telegram.
7. **Rollback = stop dashboard** — disabling `backend-dashboard` and `frontend-dashboard` containers leaves trading unaffected.
8. **No pipeline stage reordering** — DETECT→…→ADJUST sequence untouched.

### 1.2 Target Repository Layout (end state)

```
crypto-sniping-bot/                    # monorepo root; go.mod stays here (v1)
├── sniper-bot/                        # deploy unit — pipeline + workers + telegram
├── backend-dashboard/                 # deploy unit — operator REST API :8090
├── frontend-dashboard/                # deploy unit — operator SPA
├── shared/
│   ├── contracts/                     # immutable DTOs
│   ├── database/                      # adapter + migrations
│   └── config/                        # YAML — sniper source of truth
├── internal/                          # cross-app Go (operator, bootstrap, health)
├── docs/
├── scripts/
├── docker-compose.yml
└── go.mod
```

---

## 2. Architecture Impact

### Affected Pipeline Layers

| Layer / Area | Path                                                       | Change type                                                    |
| ------------ | ---------------------------------------------------------- | -------------------------------------------------------------- |
| Platform     | `internal/operator/`                                       | **Create** — shared read-query layer                           |
| Platform     | `backend-dashboard/`                                       | **Create** — new HTTP API process                              |
| Platform     | `frontend-dashboard/`                                      | **Create** — new SPA                                           |
| Platform     | `sniper-bot/cmd/serve.go`                                  | **Move** from `cmd/server.go` — no logic change in Phase 4     |
| Platform     | `cmd/telegram.go`                                          | **Modify** — delegate to `internal/operator/`                  |
| Contracts    | `shared/contracts/operator_api.go`                                | **Create** — additive API response DTOs                        |
| Contracts    | `shared/contracts/operator_command.go`                            | **Create** (Phase 5) — command event payload                   |
| DB           | `shared/database/migrations/20260613000001_operator_commands.sql` | **Create** (Phase 5) — optional command audit table            |
| Config       | `shared/config/dashboard.yaml`                                    | **Create** — API port, CORS, poll hints, auth                  |
| Config       | `internal/app/config/dashboard_config.go`                  | **Create** — Go struct for dashboard.yaml                      |
| L0–L10       | `internal/modules/*`                                       | **No change** in Phases 1–3                                    |
| Event bus    | `events` table                                             | **Consume** (Phase 5) — `operator_command_event`               |
| Telegram     | `internal/telegram/`                                       | **Unchanged** through Phase 3; optional deprecation in Phase 5 |

### DTO Flow (before → after)

```
Before (operator reads):
  Operator → Telegram poller → cmd/telegram.go inline SQL/adapter calls → text reply

After Phase 2 (read path):
  Operator → frontend-dashboard → backend-dashboard REST → internal/operator queries → database.Adapter → JSON

After Phase 5 (write path):
  Operator → frontend-dashboard → POST /api/v1/commands → backend-dashboard
      → INSERT operator_command_event (events table)
      → sniper-bot command_consumer worker → existing mode/kill/resume logic (same as Telegram)
      → mode_transition_event / system_state update
```

### Key Decisions

| Decision                                                              | Rationale                                                                                                                              |
| --------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| **Monorepo, single `go.mod`**                                         | Avoids import-path churn during migration; `sniper-bot/` and `backend-dashboard/` are deploy boundaries, not separate Go modules (v1). |
| **`internal/operator/` shared package**                               | One query implementation for Telegram + REST — prevents drift; lives outside `internal/modules/` (not a pipeline stage).               |
| **Separate `backend-dashboard` process**                              | Isolates API load, auth surface, and restart cycles from the trading hot path.                                                         |
| **Read-only v1 (Approach A)**                                         | Matches approved design spec; no new write paths until read parity proven.                                                             |
| **Commands via event bus (Phase 5)**                                  | Preserves audit trail, bounded mode transitions, and fail-closed destructive-action rules from Telegram.                               |
| **Poll every 30s (no WebSocket v1)**                                  | Design spec YAGNI; sufficient for operator monitoring.                                                                                 |
| **Sniper keeps `/health` on :8080**                                   | Existing Docker healthcheck (`Dockerfile` line 57–58) unchanged until explicitly migrated.                                             |
| **Gate review: read latest `gate_evidence_*.json` + live DB metrics** | Mirrors `scripts/gate_review_collect.sh` output; script remains source for batch collection.                                           |
| **Physical folder move last (Phase 4)**                               | Minimizes simultaneous breakage — extract + API + UI work in current paths first.                                                      |

### Mockup View → API Mapping

| Mockup `data-view` | REST endpoint (v1)                                             | Operator query / adapter source                                      |
| ------------------ | -------------------------------------------------------------- | -------------------------------------------------------------------- |
| `overview`         | `GET /api/v1/overview?chain=&market=`                          | `GetSystemState`, shadow gate, PnL summary, exposure                 |
| `pipeline`         | `GET /api/v1/pipeline?window_hours=24&chain=`                  | `GetPipelineStats`, rescan stats (type-assert)                       |
| `positions`        | `GET /api/v1/positions?chain=`                                 | `GetOpenPositions`                                                   |
| `activity`         | `GET /api/v1/activity?limit=50&chain=`                         | New: `ListRecentEvents` adapter method                               |
| `dq`               | `GET /api/v1/dq?window_hours=24&chain=`                        | DQ breakdown from lifecycle + decision logs                          |
| `gate`             | `GET /api/v1/gate/evidence`                                    | Latest `output/logs/gate_evidence_*.json` + live throughput counters |
| `mode`             | `GET /api/v1/mode` (read); `POST /api/v1/commands` (Phase 5)   | `GetSystemState`                                                     |
| `safety`           | `GET /api/v1/safety` (read); `POST /api/v1/commands` (Phase 5) | `IsSystemHalted`, kill/resume via command bus                        |
| `configs`          | `GET /api/v1/configs`                                          | Read-only manifest of `shared/config/*.yaml` (secrets redacted)             |

---

## 3. Invariants Preserved

This plan maintains the following architecture invariants:

- [x] **Profit invariant**: `Profit = Edge × Probability × Execution × Capital × DataQuality × AdaptationQuality` — pipeline modules untouched in Phases 1–3
- [x] **Determinism**: dashboard is read-mostly display; no randomness introduced
- [x] **Idempotency**: command events use content-addressable `EventID = SHA256(content)[:16]`; `ON CONFLICT DO NOTHING`
- [x] **Module isolation**: no new cross-module imports in `internal/modules/`; `internal/operator/` is platform code, not a pipeline stage
- [x] **No direct DB access from modules**: `backend-dashboard` uses `database.Adapter` only (same as orchestrator/Telegram today)
- [x] **DTO additive-only**: new files `shared/contracts/operator_api.go`, `shared/contracts/operator_command.go` — no existing DTO field changes
- [x] **Config-driven**: dashboard port, CORS origins, poll interval in `shared/config/dashboard.yaml`
- [x] **Event bus backbone**: Phase 5 commands flow through `events` table
- [x] **Security invariants**: API key via `DASHBOARD_API_KEY` env; HTTPS in prod; bounded response bodies; no keys in YAML
- [x] **Layer-1 hard rejects intact**: dashboard never bypasses DQ — read-only display only
- [x] **Telegram via event bus**: outbound Telegram unchanged; inbound commands remain until Phase 5 parity
- [ ] **Migrations append-only**: Phase 5 adds one migration for operator command audit (optional; events table may suffice)

**Factors NOT affected by this plan (Phases 1–4):** Edge, Probability, Execution, Capital, DataQuality, AdaptationQuality — no pipeline algorithm changes.

---

## 4. Implementation Tasks

### 4.1 Monorepo layout & process boundary → task map

> **End-state layout** (§1.2): one git repo, **three deployable units**, **four shared roots** (`shared/contracts/`, `shared/database/`, `shared/config/`, `internal/operator/`). v1 uses a **single root `go.mod`** (`crypto-sniping-bot`); `go.work` is deferred until a future multi-module split.

| Layer / process                 | Boundary rule                                                         | Refactored in task(s)                                                                                 | What appears on disk                                     |
| ------------------------------- | --------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- | -------------------------------------------------------- |
| **SHARED** `shared/contracts/`         | Immutable DTOs; no process imports modules                            | **1** (API DTOs), **21** (command DTO)                                                                | Stays at repo root — never moved                         |
| **SHARED** `shared/database/`          | Adapter + migrations; both processes read                             | **2** (read queries), **21** (command migration)                                                      | Stays at repo root                                       |
| **SHARED** `internal/operator/` | Read queries + formatters; not a pipeline stage                       | **3–5** (queries), **6** (Telegram parity)                                                            | Stays at repo root                                       |
| **SHARED** `shared/config/`            | Sniper = source of truth; dashboard gets own YAML                     | **7** (`dashboard.yaml`)                                                                              | `shared/config/dashboard.yaml` additive                         |
| **`backend-dashboard`**         | REST `:8090`; adapter reads; **no keys, no workers, no orchestrator** | **8** (process skeleton), **9** (auth/CORS), **10–12** (read API), **22** (POST commands → event bus) | **Task 8** creates `backend-dashboard/`                  |
| **`frontend-dashboard`**        | SPA; poll API only; **no DB, no secrets, no sniper URL**              | **13** (scaffold), **14** (layout), **15–17** (read views), **24** (control UI)                       | **Task 13** creates `frontend-dashboard/`                |
| **`sniper-bot`**                | Hot path only: orchestrator, L0–L10, keys, RPC, Telegram              | **18** (physical move), **20** (`/health` only on `:8080`), **23** (command consumer)                 | **Task 18** creates `sniper-bot/` and moves trading code |
| **Deploy topology**             | Three containers + shared Postgres                                    | **19** (`docker-compose.yml`)                                                                         | Runtime boundary enforcement                             |
| **Command path**                | Dashboard writes **never** touch sniper state directly                | **21** (DTO + event type), **22** (backend inserts event), **23** (sniper consumes)                   | Event bus audit trail                                    |

**When each top-level folder is created** (do not move sniper code earlier):

| Milestone                              | Task   | New / moved path                                                                                                        |
| -------------------------------------- | ------ | ----------------------------------------------------------------------------------------------------------------------- |
| Shared extraction (no new deploy dirs) | 1–6    | `internal/operator/` at repo root                                                                                       |
| Backend deploy unit                    | **8**  | `backend-dashboard/cmd/serve.go` + `internal/`                                                                          |
| Frontend deploy unit                   | **13** | `frontend-dashboard/` (Vite + React)                                                                                    |
| Sniper deploy unit (physical split)    | **18** | `sniper-bot/cmd/`, `sniper-bot/internal/` ← move from `cmd/`, `internal/{modules,workers,orchestrator,rpc,telegram,ai}` |
| Three-service runtime                  | **19** | `docker-compose.yml`                                                                                                    |

**Process boundary diagram** (matches §1.2 and `docs/specs/2026-06-10-operator-dashboard-design.md`):

```
frontend-dashboard  ◄──REST/poll──►  backend-dashboard (:8090)
                                              │ read (adapter)
                                              │ write (Phase 5: operator_command_event)
                                              ▼
                                        PostgreSQL
                                              ▲
                                              │ write (hot path)
                                        sniper-bot (:8080 /health)
```

**Command path (Phase 5 — Tasks 21–24):** `POST /api/v1/commands` → `operator_command_event` on event bus → sniper-bot worker applies mode/kill/resume (same safety model as Telegram). Backend-dashboard **must not** call adapter write methods for operator control.

---

### Dependency Graph

```
── Phase 1: Extract shared layers (zero deploy change; no new top-level dirs) ──

Task 1 ✅ (Contracts: operator API response DTOs)          [SHARED: contracts/]
    │
    ▼
Task 2 ✅ (Adapter: ListRecentEvents + DQ breakdown query)  [SHARED: database/]
    │
    ▼
Task 3 ✅ (internal/operator: overview + pnl + status)      [SHARED: internal/operator/]
    │
    ▼
Task 4 ✅ (internal/operator: pipeline + positions + dq)      [SHARED: internal/operator/]
    │
    ▼
Task 5 ✅ (internal/operator: gate + activity + configs)    [SHARED: internal/operator/]
    │
    ▼
Task 6 ✅ (cmd/telegram.go → internal/operator parity)      [SHARED: parity gate]

── Phase 2: backend-dashboard deploy unit (new process, read-only) ──

Task 7 ✅ (shared/config/dashboard.yaml + DashboardConfig)           [SHARED: config/]
    │
    ▼
Task 8 ✅ (backend-dashboard/cmd/serve.go skeleton)           [BOUNDARY: create backend-dashboard/]
    │
    ▼
Task 9 ✅ (Auth middleware: API key + CORS)                 [BOUNDARY: backend-dashboard/]
    │
    ▼
Task 10 ✅ (GET /api/v1/overview + /api/v1/health)          [BOUNDARY: backend-dashboard/]
    │
    ▼
Task 11 ✅ (GET /api/v1/pipeline + /positions + /pnl)       [BOUNDARY: backend-dashboard/]
    │
    ▼
Task 12 ✅ (GET /api/v1/dq + /activity + /gate + /configs)  [BOUNDARY: backend-dashboard/]

── Phase 3: frontend-dashboard deploy unit (read-only UI) ──

Task 13 ✅ (frontend-dashboard: Vite + React + TS)          [BOUNDARY: create frontend-dashboard/]
    │
    ▼
Task 14 ✅ (Layout shell from mockup)                       [BOUNDARY: frontend-dashboard/]
    │
    ▼
Task 15 ✅ (Views: overview + pipeline + positions)         [BOUNDARY: frontend-dashboard/]
    │
    ▼
Task 16 ✅ (Views: activity + dq + gate)                    [BOUNDARY: frontend-dashboard/]
    │
    ▼
Task 17 ✅ (Views: mode + safety + configs — read-only)     [BOUNDARY: frontend-dashboard/]

── Phase 4: sniper-bot deploy unit + runtime topology ──

Task 18 ✅ (Move trading code → sniper-bot/)                [BOUNDARY: create sniper-bot/ — physical split]
    │
    ▼
Task 19 (docker-compose.yml: 3 services + Postgres) ✅      [BOUNDARY: deploy topology]
    │
    ▼
Task 20 (Slim sniper HTTP: /health only on :8080) ✅        [BOUNDARY: sniper-bot/]

── Phase 5: Control plane via event bus (after read path stable) ──

Task 21 (OperatorCommandDTO + migration) ✅                 [SHARED: contracts/ + database/]
    │
    ▼
Task 22 (backend POST /api/v1/commands) ✅                    [BOUNDARY: backend-dashboard/ writes event only]
    │
    ▼
Task 23 (sniper-bot command consumer worker) ✅             [BOUNDARY: sniper-bot/ consumes event]
    │
    ▼
Task 24 (frontend: mode/safety write UI) ✅                   [BOUNDARY: frontend-dashboard/]

── Final validation ──

Task 25 (Integration tests + E2E smoke + docs) ✅        [ALL boundaries]
```

### Task Completion Protocol (required for every task)

A task is **not complete** until all validation commands pass **and** `docs/ops/PROGRESS_REPORT.md` is updated in the same session.

| Item            | Convention                                                                                                                |
| --------------- | ------------------------------------------------------------------------------------------------------------------------- |
| Commit prefix   | `OPERATOR_DASHBOARD Task N — {task name}`                                                                                 |
| Progress report | Append row to `docs/ops/PROGRESS_REPORT.md` using existing table format                                                   |
| Rollback check  | After Tasks 8+, confirm `crypto-sniping-bot serve` still passes `go test ./...` with zero dashboard dependencies required |

---

### Task 1 — Contracts: Operator API Response DTOs ✅

**Status:** ✅ Done (2026-06-13)

**Goal:** Define additive JSON-serializable response shapes for dashboard REST endpoints — decoupled from Telegram HTML formatting.

**Layer(s) affected:** Contracts

**Files to create/modify:**

- `shared/contracts/operator_api.go` (create) — response DTOs:
  - `OverviewResponseDTO` — mode, shadow/live, drawdown, exposure, open positions, PnL today, win rate 7d, shadow gate block, chain status strip
  - `PipelineStatsResponseDTO` — wraps funnel counts + per-layer heartbeat status
  - `PositionRowDTO` — open position summary for table view
  - `ActivityEventDTO` — recent event bus tail entry
  - `DQBreakdownResponseDTO` — reject reason counts + pass rate
  - `GateEvidenceResponseDTO` — throughput verdict + §1.1 criteria from `docs/plans/2026-06-10-profit-restoration-plan.md`
  - `ConfigManifestEntryDTO` — filename, sha256 prefix, last modified (no secret values)
- `docs/reference/dto_contracts.md` (modify) — append registry entries for new DTOs (additive doc only)

**Invariant check:**

- [x] DTO changes additive-only (new file, no existing struct modifications)
- [x] All structs have `json` tags; timestamps ISO 8601 strings
- [x] No secrets fields (wallet keys, API tokens)

**Validation:**

- `go build ./contracts/...`: zero errors
- `go vet ./contracts/...`: zero issues

**Prompt context needed:** §7.1 Operator API DTOs

---

### Task 2 — Adapter: ListRecentEvents + DQ Breakdown Queries ✅

**Status:** ✅ Done (2026-06-13)

**Goal:** Add read-only adapter methods needed by the dashboard but not yet exposed via Telegram text commands.

**Layer(s) affected:** DB / Platform

**Files to create/modify:**

- `shared/database/adapter.go` (modify) — add interface methods:
  - `ListRecentEvents(ctx, chain string, limit int) ([]RecentEventRow, error)`
  - `GetDQBreakdown(ctx, windowHours int, chain string) (*DQBreakdown, error)`
- `shared/database/engines/postgres/events.go` (create or modify) — `ListRecentEvents` implementation
- `shared/database/engines/postgres/lifecycle.go` (modify) — `GetDQBreakdown` aggregating reject reasons
- `shared/database/engines/postgres/events_test.go` (create) — deterministic fixture test

**Invariant check:**

- [x] Read-only SQL (`SELECT` only)
- [x] `ON CONFLICT` not needed (reads only)
- [x] Chain filter optional — empty chain = all chains
- [x] `limit` capped at 200 server-side (bounded response)

**Validation:**

- `go build ./database/...`: zero errors
- `go test ./database/engines/postgres/... -run 'RecentEvent|DQBreakdown'`: green

**Prompt context needed:** §7.5 Adapter query patterns; §7.6 Security (bounded limits)

---

### Task 3 — Operator Queries: Overview, Status, PnL ✅

**Status:** ✅ Done (2026-06-13)

**Goal:** Create `internal/operator/` package with typed queries for overview KPIs — extracted from `cmd/telegram.go` `buildStatusFn` and `buildPnlFn` logic.

**Layer(s) affected:** Platform

**Files to create/modify:**

- `internal/operator/overview.go` (create) — `func BuildOverview(ctx, db, cfg, startTime) (*contracts.OverviewResponseDTO, error)`
- `internal/operator/pnl.go` (create) — `func BuildPnLSummary(ctx, db, lookbackHours) (*contracts.PnLSummaryDTO, error)`
- `internal/operator/shadow_gate.go` (create) — thin wrapper around `health.ShadowGateEvaluator`
- `internal/operator/overview_test.go` (create) — stub adapter tests mirroring telegram test fixtures

**Invariant check:**

- [x] No imports from `internal/modules/` (except `health` evaluator — platform cross-cut, same as `cmd/telegram.go` today)
- [x] No SQL in operator package — adapter calls only
- [x] No hardcoded thresholds — lookback hours from config or function args

**Validation:**

- `go build ./internal/operator/...`: zero errors
- `go test ./internal/operator/... -run Overview`: green

**Prompt context needed:** §7.1, §7.3, §7.9

---

### Task 4 — Operator Queries: Pipeline, Positions, DQ ✅

**Goal:** Extract pipeline funnel, open positions, and DQ breakdown into `internal/operator/`.

**Layer(s) affected:** Platform

**Files to create/modify:**

- `internal/operator/pipeline.go` (create) — `BuildPipelineStats(ctx, db, windowHours, chain)` wrapping `GetPipelineStats` + optional `GetRescanPipelineStats` type-assert
- `internal/operator/positions.go` (create) — `BuildPositionRows(ctx, db, chain)` wrapping `GetOpenPositions` with TP/SL context
- `internal/operator/dq.go` (create) — `BuildDQBreakdown(ctx, db, windowHours, chain)` wrapping Task 2 adapter method
- `internal/operator/pipeline_test.go` (create) — funnel math regression (cumulative counts)

**Invariant check:**

- [x] Cumulative funnel semantics preserved per `database.PipelineStats` comment
- [x] Chain filter applied post-query (or SQL-side if adapter supports it)

**Validation:**

- `go test ./internal/operator/... -run 'Pipeline|Position|DQ'`: green

**Prompt context needed:** §7.4 Pipeline funnel semantics; §7.1

---

### Task 5 — Operator Queries: Gate Evidence, Activity, Config Manifest ✅

**Goal:** Complete the operator query layer for gate review, event activity tail, and read-only config listing.

**Layer(s) affected:** Platform

**Files to create/modify:**

- `internal/operator/gate.go` (create) — `BuildGateEvidence(ctx, db, evidenceDir)` reads latest `gate_evidence_*.json` from `output/logs/` + merges live DB throughput counters
- `internal/operator/activity.go` (create) — `BuildActivityFeed(ctx, db, chain, limit)` wrapping `ListRecentEvents`
- `internal/operator/configs.go` (create) — `BuildConfigManifest(configDir)` lists `shared/config/*.yaml` with redaction (`${ENV_VAR}` placeholders only; never echo env values)
- `internal/operator/gate_test.go` (create) — uses `tests/fixtures/gate_phase2_pass_evidence.json`

**Invariant check:**

- [x] Config manifest never returns secret values — filenames + structural YAML keys only
- [x] Gate evidence file read bounded to 256 KiB (`io.LimitReader`)
- [x] Missing evidence file → graceful empty state, not 500

**Validation:**

- `go test ./internal/operator/... -run 'Gate|Activity|Config'`: green

**Prompt context needed:** §7.7 Gate evidence schema; §7.6

---

### Task 6 — Rewire Telegram to `internal/operator` (Parity Gate) ✅

**Goal:** Replace inline logic in `cmd/telegram.go` with `internal/operator` calls — **zero operator-visible behavior change**. This is the parity gate before any new API work.

**Layer(s) affected:** Platform

**Files to create/modify:**

- `cmd/telegram.go` (modify) — `buildStatusFn`, `buildPnlFn`, `buildPipelineFn`, `buildDqFn`, `buildPositionsFn` delegate to `internal/operator`; keep HTML formatting in telegram layer
- `cmd/telegram_parity_test.go` (create) — golden-file tests: operator text output before/after must match for fixture DB state

**Invariant check:**

- [x] Telegram HTML formatting stays in `cmd/telegram.go` / `internal/telegram/` — operator package returns DTOs only
- [x] All existing telegram tests still pass
- [x] Destructive commands (`/kill`, `/resume`, `/mode`) **not** moved yet — stay in cmd/telegram.go

**Validation:**

- `go test ./cmd/... -run Telegram`: green
- `go test ./internal/telegram/...`: green
- Manual: `/status`, `/pipeline`, `/dq`, `/positions` output unchanged on dev DB

**Prompt context needed:** §7.8 Telegram parity rules

---

### Task 7 — Dashboard Config: `shared/config/dashboard.yaml` ✅

**Goal:** Add configuration for the dashboard API process — ports, CORS, auth, polling hints.

**Layer(s) affected:** Config

**Files to create/modify:**

- `shared/config/dashboard.yaml` (create):
  ```yaml
  dashboard:
    listen_port: 8090
    cors_allowed_origins:
      - "http://localhost:5173" # Vite dev
    poll_interval_seconds: 30
    max_events_per_request: 50
    gate_evidence_dir: "output/logs"
    config_manifest_dir: "config"
  ```
- `internal/app/config/dashboard_config.go` (create) — `DashboardConfig` struct + `applyDashboardDefaults()`
- `internal/app/config/config.go` (modify) — optional `Dashboard` field (loaded only by backend-dashboard cmd)
- `internal/app/config/dashboard_config_test.go` (create)

**Invariant check:**

- [x] No API keys in YAML — `DASHBOARD_API_KEY` env only
- [x] Sensible defaults; disabled-by-default auth reject when key unset in production

**Validation:**

- `go test ./internal/app/config/... -run Dashboard`: green

**Prompt context needed:** §7.2, §7.3

---

### Task 8 — Backend Dashboard: Process Skeleton ✅

**Boundary:** Creates **`backend-dashboard/`** deploy unit (first new top-level folder). No orchestrator, workers, keys, or RPC.

**Goal:** Create `backend-dashboard/cmd/serve.go` — standalone process connecting to PostgreSQL via existing adapter; no routes yet.

**Layer(s) affected:** Platform

**Files to create/modify:**

- `backend-dashboard/cmd/serve.go` (create) — DB init, config load, graceful shutdown, structured logging (mirror `cmd/server.go` lines 55–62 pattern)
- `backend-dashboard/cmd/root.go` (create) — `dashboard serve` entry if using subcommand pattern; OR single `main.go`
- `Makefile` (modify) — `make dashboard-serve` target

**Invariant check:**

- [x] Does NOT start orchestrator, workers, or Telegram
- [x] Does NOT load wallet keys or RPC endpoints
- [x] Uses same `buildDBConfig()` helper (extract to `internal/app/bootstrap/db.go` if needed to avoid duplication)

**Validation:**

- `go build -o bin/dashboard-api ./backend-dashboard/cmd/...`: success
- Process starts, connects to DB, exits cleanly on SIGTERM
- `crypto-sniping-bot serve` still builds and runs unchanged

**Prompt context needed:** §7.10 Process boundary rules

---

### Task 9 — Backend Dashboard: Auth + CORS Middleware ✅

**Status:** ✅ Done (2026-06-13)

**Goal:** Secure the dashboard API with API-key auth and CORS for the frontend origin.

**Layer(s) affected:** Platform

**Files to create/modify:**

- `backend-dashboard/internal/auth/middleware.go` (create) — `X-Dashboard-Key` header or `Authorization: Bearer`; constant-time compare; reject when `DASHBOARD_API_KEY` unset in non-dev
- `backend-dashboard/internal/auth/cors.go` (create) — origins from `shared/config/dashboard.yaml`
- Reuse `internal/app/web/securityHeaders` pattern from existing server (extract to `internal/app/web/headers.go` if needed)

**Invariant check:**

- [x] API key from `os.Getenv("DASHBOARD_API_KEY")` only
- [x] `Cache-Control: no-store` on all API responses
- [x] CSP not needed for JSON API

**Validation:**

- `go test ./backend-dashboard/internal/auth/...`: green (401 without key, 200 with key)

**Prompt context needed:** §7.6

---

### Task 10 — Backend Dashboard: Overview + Health Endpoints ✅

**Status:** ✅ Done (2026-06-13)

**Goal:** Implement `GET /api/v1/overview` and `GET /api/v1/health` — first real API surface.

**Layer(s) affected:** Platform

**Files to create/modify:**

- `backend-dashboard/internal/api/overview/handler.go` (create) — vertical slice: handler → `operator.BuildOverview`
- `backend-dashboard/internal/api/health/handler.go` (create) — wraps existing health check + shadow gate JSON
- `backend-dashboard/internal/api/router.go` (create) — route registration
- `backend-dashboard/internal/api/overview/handler_test.go` (create)

**Invariant check:**

- [x] JSON responses only — no HTML
- [x] Query params: `chain` (optional), `market` (optional)
- [x] Response maps to `contracts.OverviewResponseDTO`

**Validation:**

- `curl -H "X-Dashboard-Key: $DASHBOARD_API_KEY" http://localhost:8090/api/v1/overview` returns 200 JSON
- `go test ./backend-dashboard/internal/api/overview/...`: green

**Prompt context needed:** §7.1, §7.11 API route table

---

### Task 11 — Backend Dashboard: Pipeline, Positions, PnL Endpoints ✅

**Status:** ✅ Done (2026-06-13)

**Goal:** Implement core monitoring endpoints matching mockup Monitor section.

**Layer(s) affected:** Platform

**Files to create/modify:**

- `backend-dashboard/internal/api/pipeline/handler.go` (create)
- `backend-dashboard/internal/api/positions/handler.go` (create)
- `backend-dashboard/internal/api/pnl/handler.go` (create)
- Handler tests for each (create)

**Invariant check:**

- [x] `window_hours` query param defaults to 24; max 168
- [x] Positions response includes `trace_id`, `strategy_version_id` per design spec

**Validation:**

- `go test ./backend-dashboard/internal/api/...`: green
- Response shapes validated against `shared/contracts/operator_api.go`

**Prompt context needed:** §7.1, §7.4

---

### Task 12 — Backend Dashboard: DQ, Activity, Gate, Configs Endpoints ✅

**Status:** ✅ Done (2026-06-13)

**Goal:** Complete read-only API surface for all mockup views.

**Layer(s) affected:** Platform

**Files to create/modify:**

- `backend-dashboard/internal/api/dq/handler.go` (create)
- `backend-dashboard/internal/api/activity/handler.go` (create)
- `backend-dashboard/internal/api/gate/handler.go` (create)
- `backend-dashboard/internal/api/configs/handler.go` (create)
- `backend-dashboard/internal/api/router.go` (modify) — register all routes under `/api/v1/`

**Invariant check:**

- [x] `/api/v1/configs` returns manifest only — never raw secrets
- [x] `/api/v1/gate/evidence` returns `THROUGHPUT_VERDICT` field

**Validation:**

- `go test ./backend-dashboard/...`: all green
- Manual smoke: all 9 mockup views have a corresponding endpoint

**Prompt context needed:** §7.7, §7.11

---

### Task 13 — Frontend Dashboard: Vite + React + TypeScript Scaffold ✅

**Status:** ✅ Done (2026-06-13)

**Boundary:** Creates **`frontend-dashboard/`** deploy unit (second new top-level folder). No DB, no secrets, no sniper URL.

**Goal:** Initialize `frontend-dashboard/` with Vite, React 18, TypeScript, and API client boilerplate.

**Layer(s) affected:** Platform (frontend)

**Files to create/modify:**

- `frontend-dashboard/package.json` (create)
- `frontend-dashboard/vite.config.ts` (create) — proxy `/api` → `localhost:8090` in dev
- `frontend-dashboard/tsconfig.json` (create)
- `frontend-dashboard/src/api/client.ts` (create) — typed fetch with `X-Dashboard-Key` from `VITE_DASHBOARD_API_KEY`
- `frontend-dashboard/src/api/types.ts` (create) — mirror `shared/contracts/operator_api.go` shapes
- `.gitignore` (modify) — `frontend-dashboard/node_modules/`, `dist/`

**Invariant check:**

- [x] API key in env var only (`VITE_DASHBOARD_API_KEY` for dev — never committed)
- [x] No wallet or RPC code in frontend

**Validation:**

- `cd frontend-dashboard && npm install && npm run build`: zero errors
- `npm run dev` serves on :5173

**Prompt context needed:** §7.11

---

### Task 14 — Frontend: Layout Shell from Mockup ✅

**Status:** ✅ Done (2026-06-14)

**Goal:** Port sidebar, chain bar, collapsible nav, and CSS design tokens from `docs/mockups/operator-dashboard.html`.

**Layer(s) affected:** Platform (frontend)

**Files to create/modify:**

- `frontend-dashboard/src/styles/tokens.css` (create) — CSS variables from mockup `:root`
- `frontend-dashboard/src/components/Sidebar.tsx` (create)
- `frontend-dashboard/src/components/ChainBar.tsx` (create)
- `frontend-dashboard/src/App.tsx` (create) — view router via `data-view` state
- `frontend-dashboard/src/hooks/useChainFilter.ts` (create)

**Invariant check:**

- [x] Visual parity with mockup (dark theme, sidebar collapse, chain dots)
- [x] Accessible: `aria-label` on nav, keyboard nav for view switch

**Validation:**

- Visual review against `docs/mockups/operator-dashboard.html`
- `npm run build`: zero errors

**Prompt context needed:** §7.12 Mockup view list

---

### Task 15 ✅ — Frontend Views: Overview, Pipeline, Positions

**Goal:** Wire Monitor section views to live API with 30s polling.

**Layer(s) affected:** Platform (frontend)

**Files to create/modify:**

- `frontend-dashboard/src/views/OverviewView.tsx` (create) — KPI grid, chain status strip, drill-down cards
- `frontend-dashboard/src/views/PipelineView.tsx` (create) — L0–L10 table, heartbeat indicators
- `frontend-dashboard/src/views/PositionsView.tsx` (create) — open positions table
- `frontend-dashboard/src/hooks/usePolling.ts` (create) — 30s interval from config

**Invariant check:**

- [x] Loading and error states for each view
- [x] Chain filter passed as query param to API

**Validation:**

- Manual: all three views render live data from running `backend-dashboard`
- `npm run build`: zero errors

**Prompt context needed:** §7.11, §7.12

---

### Task 16 ✅ — Frontend Views: Activity, DQ, Gate

**Goal:** Wire Quality section views.

**Layer(s) affected:** Platform (frontend)

**Files to create/modify:**

- `frontend-dashboard/src/views/ActivityView.tsx` (create)
- `frontend-dashboard/src/views/DQView.tsx` (create)
- `frontend-dashboard/src/views/GateView.tsx` (create) — criteria grid from §1.1 PLAN.md

**Invariant check:**

- [x] Gate view shows `CODE_DEFECT | MARKET_QUIET | GUARDRAILS_ACTIVE | HEALTHY` verdict
- [x] DQ view shows mandatory hard-reject breakdown

**Validation:**

- Manual end-to-end with live backend
- `npm run build`: zero errors

**Prompt context needed:** §7.7, §7.12

---

### Task 17 ✅ — Frontend Views: Mode, Safety, Configs (Read-Only)

**Goal:** Render Control section views in read-only mode — buttons disabled with tooltip "Use Telegram for now" per Approach A.

**Layer(s) affected:** Platform (frontend)

**Files to create/modify:**

- `frontend-dashboard/src/views/ModeView.tsx` (create) — mode grid, current mode highlighted, buttons disabled
- `frontend-dashboard/src/views/SafetyView.tsx` (create) — kill/resume buttons disabled with explanation
- `frontend-dashboard/src/views/ConfigsView.tsx` (create) — YAML manifest table

**Invariant check:**

- [x] No write API calls in this task
- [x] Clear UX copy pointing to Telegram for destructive actions

**Validation:**

- Manual review: Control section matches mockup visually but is read-only
- `npm run build`: zero errors

**Prompt context needed:** §7.12, §7.8

---

### Task 18 ✅ — Physical Move: `sniper-bot/` Folder Split

**Boundary:** Creates **`sniper-bot/`** deploy unit — **physical refactor of trading hot path** (move only; zero logic change). Run only after Tasks 1–17 are green (§4.1).

**Goal:** Move trading code into `sniper-bot/` without changing runtime behavior — the seamless cutover.

**Layer(s) affected:** Platform

**Files to create/modify:**

- Move `cmd/server.go` → `sniper-bot/cmd/serve.go`
- Move `cmd/migrate.go`, `cmd/hydrate.go`, `cmd/helpers.go`, `cmd/telegram.go` → `sniper-bot/cmd/`
- Move `internal/orchestrator`, `internal/workers`, `internal/modules`, `internal/rpc`, `internal/telegram`, `internal/ai` → `sniper-bot/internal/`
- Root `cmd/` (modify) — thin forwarders OR update `go build` paths in Makefile/Dockerfile
- `Dockerfile` (modify) — `go build ./sniper-bot/cmd/`
- `Makefile` (modify) — `make serve` builds sniper-bot
- All import paths remain `crypto-sniping-bot/...` (single module — **no** `go.mod` path change)

**Invariant check:**

- [x] `internal/operator/` stays at repo root (shared)
- [x] `shared/contracts/`, `shared/database/`, `shared/config/` stay at repo root
- [x] Zero logic changes — move only

**Validation:**

- `go build ./...`: zero errors
- `go test ./...`: all green
- `make serve` starts pipeline identically to pre-move

**Prompt context needed:** §7.10

---

### Task 19 — Docker Compose: Three-Service Topology ✅

**Status:** ✅ Done (2026-06-14)

**Boundary:** Enforces **runtime** separation — sniper `:8080`, dashboard-api `:8090`, dashboard-ui `:3000`; shared Postgres; dashboard containers get no wallet keys.

**Goal:** Add `docker-compose.yml` with sniper, dashboard-api, and dashboard-ui services sharing Postgres.

**Layer(s) affected:** Platform

**Files to create/modify:**

- `docker-compose.yml` (create or modify):
  - `sniper-bot` — existing image, port 8080, env vars for keys
  - `dashboard-api` — port 8090, `DASHBOARD_API_KEY`, same `DATABASE_URL`
  - `dashboard-ui` — nginx serving `frontend-dashboard/dist`, port 3000
- `frontend-dashboard/Dockerfile` (create) — multi-stage build
- `backend-dashboard/Dockerfile` (create) — distroless static binary
- `Makefile` (modify) — `make up`, `make down`, `make dashboard-dev`

**Invariant check:**

- [x] Sniper container does NOT mount dashboard code
- [x] Dashboard-api container does NOT receive wallet private keys
- [x] Healthchecks: sniper → `:8080/health`; dashboard-api → `:8090/api/v1/health`

**Validation:**

- `docker compose up -d` — all three services healthy
- Sniper trades (shadow mode) while dashboard displays live data

**Prompt context needed:** §7.10

---

### Task 20 — Slim Sniper HTTP Surface ✅

**Status:** ✅ Done (2026-06-14)

**Boundary:** **`sniper-bot/`** exposes only `/health` on `:8080`; all dashboard routes live on `backend-dashboard` `:8090`.

**Goal:** Confirm sniper exposes only `/health` on :8080; all dashboard routes live exclusively on backend-dashboard :8090.

**Layer(s) affected:** Platform

**Files to create/modify:**

- `sniper-bot/cmd/serve.go` (modify) — remove any dashboard route registration if added during development
- `internal/app/web/server.go` (modify) — document: sniper web server = health only
- `docs/reference/architecture.md` (modify) — add § "Operator Dashboard Topology" cross-reference (one paragraph + link to this plan)

**Invariant check:**

- [x] Existing Docker healthcheck still passes
- [x] No regression in shadow gate JSON on `/health`

**Validation:**

- `curl localhost:8080/health` → 200
- `curl localhost:8080/api/v1/overview` → 404
- `curl localhost:8090/api/v1/overview` → 200

**Prompt context needed:** §7.10

---

### Task 21 — Migration + OperatorCommandDTO (Phase 5 Foundation) ✅

**Status:** ✅ Done (2026-06-14)

**Goal:** Add event type and DTO for dashboard-originated operator commands.

**Layer(s) affected:** Contracts / DB

**Files to create/modify:**

- `shared/contracts/operator_command.go` (create):
  ```go
  type OperatorCommandDTO struct {
      CommandID   string // SHA256(content)[:16]
      CommandType string // "mode" | "kill" | "resume" | "force_close"
      IssuerID    string // dashboard user id (from auth)
      Args        map[string]string
      ConfirmToken string // required for destructive
      Timestamp   string
  }
  ```
- `shared/database/migrations/20260613000001_operator_command_audit.sql` (create) — optional audit table mirroring events (append-only)
- `docs/reference/dto_contracts.md` (modify) — register `operator_command_event`

**Invariant check:**

- [x] Migration append-only
- [x] `ON CONFLICT DO NOTHING` on event insert
- [x] Content-addressable `CommandID`

**Validation:**

- `go test ./database/...`: green after migration apply
- Migration applies cleanly on fresh and existing DB

**Prompt context needed:** §7.5

---

### Task 22 — Backend: POST `/api/v1/commands` ✅

**Status:** ✅ Done (2026-06-14)

**Boundary:** **`backend-dashboard/`** inserts `operator_command_event` only — **must not** call adapter write methods on sniper state directly.

**Goal:** Implement command submission endpoint with confirmation flow for destructive ops.

**Layer(s) affected:** Platform

**Files to create/modify:**

- `backend-dashboard/internal/api/commands/handler.go` (create) — validates command, emits `operator_command_event` via adapter
- `backend-dashboard/internal/api/commands/confirm.go` (create) — two-step confirm for kill/resume (POST to request token, POST with token to execute)
- `shared/config/dashboard.yaml` (modify) — `destructive_confirm_ttl_seconds: 60`

**Invariant check:**

- [x] Fail-closed when issuer not in allowlist (mirror `SNIPER_TELEGRAM_ALLOWED_USERS` pattern — new env `DASHBOARD_ALLOWED_OPERATORS`)
- [x] Bounded request body (4 KiB)
- [x] Does NOT call `UpsertSystemState` directly — event bus only

**Validation:**

- `go test ./backend-dashboard/internal/api/commands/...`: green
- Integration: POST mode change → event row appears in `events` table

**Prompt context needed:** §7.5, §7.6, §7.8

---

### Task 23 — Sniper: Operator Command Consumer Worker ✅

**Status:** ✅ Done (2026-06-14)

**Boundary:** **`sniper-bot/`** consumes `operator_command_event` and applies mode/kill/resume (same logic as Telegram).

**Goal:** Sniper-bot consumes `operator_command_event` and dispatches to existing mode/kill/resume logic (same code paths as Telegram).

**Layer(s) affected:** Platform

**Files to create/modify:**

- `sniper-bot/internal/workers/run_operator_commands.go` (create) — claims events, dispatches by `CommandType`
- `sniper-bot/internal/operator/commands.go` (create) — shared execution logic extracted from `cmd/telegram.go` kill/mode/resume builders
- `sniper-bot/cmd/serve.go` (modify) — register worker goroutine
- `cmd/telegram.go` (modify) — delegate destructive handlers to `internal/operator/commands.go`

**Invariant check:**

- [x] One command processed per event (idempotent on replay)
- [x] Bounded mode transitions preserved (one per window)
- [x] All commands logged with `issuer_id`

**Validation:**

- `go test ./sniper-bot/internal/workers/... -run OperatorCommand`: green
- E2E: dashboard POST kill → sniper halts → overview shows HALTED

**Prompt context needed:** §7.5, §7.8

---

### Task 24 — Frontend: Mode & Safety Write UI ✅

**Status:** ✅ Done (2026-06-14)

**Goal:** Enable Control section buttons wired to `POST /api/v1/commands` with confirmation modals.

**Layer(s) affected:** Platform (frontend)

**Files to create/modify:**

- `frontend-dashboard/src/views/ModeView.tsx` (modify) — enable mode buttons
- `frontend-dashboard/src/views/SafetyView.tsx` (modify) — kill/resume with typed confirmation
- `frontend-dashboard/src/api/commands.ts` (create)

**Invariant check:**

- [x] Destructive actions require explicit confirmation UI (type "KILL" or similar)
- [x] Error toasts on 401/403/409

**Validation:**

- Manual E2E: mode change via UI → reflected in overview within 30s
- `npm run build`: zero errors

**Prompt context needed:** §7.8

---

### Task 25 — Integration Tests, E2E Smoke, PROGRESS_REPORT, Architecture Cross-Ref ✅

**Status:** ✅ Done (2026-06-14)

**Goal:** Final validation across all three deployable units; record plan completion.

**Layer(s) affected:** Platform

**Files to create/modify:**

- `backend-dashboard/tests/integration/dashboard_api_test.go` (create) — read-only endpoint smoke against test DB fixture
- `backend-dashboard/tests/integration/operator_command_test.go` (create) — command event round-trip
- `docs/ops/PROGRESS_REPORT.md` (modify) — append Tasks 1–25 completion rows
- `docs/plans/2026-06-13-operator-dashboard-plan.md` (modify) — status → Completed
- `docs/reference/architecture.md` (modify) — § operator dashboard topology (if not done in Task 20)

**Invariant check:**

- [x] Full test suite green
- [x] Sniper serves without dashboard running
- [x] Dashboard serves without sniper running (read-only degraded: pipeline heartbeats stale)

**Validation:**

- `go build ./...`: zero errors
- `go vet ./...`: zero issues
- `go test ./...`: all packages green
- `cd frontend-dashboard && npm run build`: zero errors
- `docker compose up` smoke test documented in PROGRESS_REPORT

**Prompt context needed:** All §7 sections

---

## 5. Task Summary

| Task | Name                                     | Boundary / unit                                      | Primary files                                          | Depends on | Est. complexity |
| ---- | ---------------------------------------- | ---------------------------------------------------- | ------------------------------------------------------ | ---------- | --------------- |
| 1 ✅ | Operator API DTOs                        | SHARED `shared/contracts/`                                  | `shared/contracts/operator_api.go`                            | —          | Low             |
| 2 ✅ | Adapter read queries                     | SHARED `shared/database/`                                   | `shared/database/adapter.go`, `postgres/events.go`            | —          | Medium          |
| 3 ✅ | Operator: overview + pnl                 | SHARED `internal/operator/`                          | `internal/operator/overview.go`                        | 1, 2       | Medium          |
| 4 ✅ | Operator: pipeline + positions + dq      | SHARED `internal/operator/`                          | `internal/operator/pipeline.go`                        | 1, 2       | Medium          |
| 5 ✅ | Operator: gate + activity + configs      | SHARED `internal/operator/`                          | `internal/operator/gate.go`                            | 1, 2       | Medium          |
| 6 ✅ | Telegram parity rewire                   | SHARED (parity gate before new processes)            | `cmd/telegram.go`                                      | 3, 4, 5    | Medium          |
| 7 ✅ | Dashboard config YAML                    | SHARED `shared/config/`                                     | `shared/config/dashboard.yaml`                                | —          | Low             |
| 8 ✅ | Backend process skeleton                 | **`backend-dashboard/`** (creates deploy unit)       | `backend-dashboard/cmd/serve.go`                       | 7          | Low             |
| 9 ✅ | Auth + CORS middleware                   | `backend-dashboard/`                                 | `backend-dashboard/internal/auth/`                     | 8          | Medium          |
| 10 ✅ | Overview + health API                    | `backend-dashboard/`                                 | `backend-dashboard/internal/api/overview/`             | 3, 9       | Medium          |
| 11 ✅ | Pipeline + positions + pnl API           | `backend-dashboard/`                                 | `backend-dashboard/internal/api/pipeline/`             | 4, 9       | Medium          |
| 12 ✅ | DQ + activity + gate + configs API       | `backend-dashboard/`                                 | `backend-dashboard/internal/api/dq/`                   | 5, 9       | Medium          |
| 13 ✅ | Frontend scaffold                        | **`frontend-dashboard/`** (creates deploy unit)      | `frontend-dashboard/package.json`                      | —          | Low             |
| 14 ✅ | Layout shell                             | `frontend-dashboard/`                                | `frontend-dashboard/src/components/`                   | 13         | Medium          |
| 15 ✅ | Views: overview, pipeline, positions     | `frontend-dashboard/`                                | `frontend-dashboard/src/views/`                        | 10, 14     | High            |
| 16 ✅ | Views: activity, dq, gate                | `frontend-dashboard/`                                | `frontend-dashboard/src/views/`                        | 12, 14     | High            |
| 17 ✅ | Views: mode, safety, configs (read-only) | `frontend-dashboard/`                                | `frontend-dashboard/src/views/`                        | 12, 14     | Medium          |
| 18 ✅ | Physical sniper-bot move                 | **`sniper-bot/`** (creates deploy unit; move only)   | `sniper-bot/cmd/`, `sniper-bot/internal/`              | 6          | High            |
| 19 ✅ | Docker Compose 3-service                 | Deploy topology (all 3 units + Postgres)             | `docker-compose.yml`                                   | 8, 13, 18  | Medium          |
| 20 ✅ | Slim sniper HTTP                         | `sniper-bot/` (`:8080` health only)                  | `sniper-bot/cmd/serve.go`                              | 19         | Low             |
| 21 ✅ | OperatorCommand migration + DTO          | SHARED `shared/contracts/` + `shared/database/`                    | `shared/contracts/operator_command.go`                        | 20         | Low             |
| 22 ✅ | POST /api/v1/commands                    | `backend-dashboard/` (event bus write, not hot path) | `backend-dashboard/internal/api/commands/`             | 21         | High            |
| 23 ✅ | Sniper command consumer                  | `sniper-bot/` (consumes `operator_command_event`)    | `sniper-bot/internal/workers/run_operator_commands.go` | 21, 22     | High            |
| 24 ✅ | Frontend control UI                      | `frontend-dashboard/`                                | `frontend-dashboard/src/views/ModeView.tsx`            | 22, 23     | Medium          |
| 25 ✅ | Integration tests + completion           | All boundaries                                       | `backend-dashboard/tests/integration/`, `PROGRESS_REPORT.md` | 24         | Medium          |

---

## 6. How to Use This Plan

1. **Start each task in a fresh chat session** — share this `OPERATOR_DASHBOARD_PLAN.md` + the relevant §7 sub-sections listed under "Prompt context needed" for that task.
2. **Validate after each task** — run the task's validation commands before proceeding. For Tasks 1–6, confirm `crypto-sniping-bot serve` behavior is unchanged.
3. **Do not skip Task 6** — Telegram parity is the rollback safety gate before any new processes are deployed.
4. **One task at a time** — do not combine tasks; dependency graph enforces ordering.
5. **Source of truth** — refer to `docs/specs/2026-06-10-operator-dashboard-design.md` for UX decisions and `docs/mockups/operator-dashboard.html` for visual reference. This plan is the implementation breakdown.
6. **Phases 1–3 before Phase 4** — resist moving folders (Task 18) until API + operator extraction are green.
7. **Phase 5 is optional until sign-off** — read-only dashboard (Tasks 1–20) delivers full monitoring value per Approach A.
8. **Update `docs/ops/PROGRESS_REPORT.md`** after every task per §4 Task Completion Protocol.
9. **Invariants are non-negotiable** — if a task appears to require bypassing event-bus command flow or putting keys in the dashboard process, stop and flag for design review.

### Suggested parallel execution (after Task 6)

| Track A                  | Track B                                  |
| ------------------------ | ---------------------------------------- |
| Tasks 7–12 (backend API) | Tasks 13–14 (frontend scaffold + layout) |
| Task 15–17 after Task 12 |                                          |

Tracks merge at Task 15 (frontend needs live API).

---

## 7. Deep Knowledge Reference

This section contains schemas, business rules, and patterns needed by task sessions.
Include the specific §7.N sub-sections listed under "Prompt context needed" for your task.

---

### 7.1 Operator API DTOs (Task 1)

New file `shared/contracts/operator_api.go` — all additive. Key structs:

```go
// OverviewResponseDTO — GET /api/v1/overview
type OverviewResponseDTO struct {
    Mode              string             `json:"mode"`
    ExecutionMode     string             `json:"execution_mode"`      // shadow | live
    DrawdownPct       float64            `json:"drawdown_pct"`
    OpenPositions     int32              `json:"open_positions"`
    TotalExposureUsd  float64            `json:"total_exposure_usd"`
    MaxExposureUsd    float64            `json:"max_exposure_usd"`
    PnLTodayUsd       float64            `json:"pnl_today_usd"`
    WinRate7d         float64            `json:"win_rate_7d"`
    ClosedTrades7d    int32              `json:"closed_trades_7d"`
    ShadowGate        *ShadowGateBlockDTO `json:"shadow_gate,omitempty"`
    ChainStatuses     []ChainStatusDTO   `json:"chain_statuses"`
    AlertBanner       *AlertBannerDTO    `json:"alert_banner,omitempty"`
    StrategyVersionID string             `json:"strategy_version_id"`
    UpdatedAt         string             `json:"updated_at"`
}

// PipelineStatsResponseDTO — GET /api/v1/pipeline
type PipelineStatsResponseDTO struct {
    WindowHours   int                    `json:"window_hours"`
    Funnel        PipelineFunnelDTO      `json:"funnel"`
    LayerHeartbeats []LayerHeartbeatDTO  `json:"layer_heartbeats"`
    ThroughputVerdict string             `json:"throughput_verdict,omitempty"`
}

// PositionRowDTO — GET /api/v1/positions
type PositionRowDTO struct {
    PositionID        string  `json:"position_id"`
    TokenAddress      string  `json:"token_address"`
    Chain             string  `json:"chain"`
    Market            string  `json:"market"`
    EntryPriceUsd     float64 `json:"entry_price_usd"`
    CurrentPriceUsd   float64 `json:"current_price_usd"`
    PnLPct            float64 `json:"pnl_pct"`
    SizeUsd           float64 `json:"size_usd"`
    AgeSeconds        int64   `json:"age_seconds"`
    TraceID           string  `json:"trace_id"`
    StrategyVersionID string  `json:"strategy_version_id"`
}
```

Existing DTO consumed (unchanged): `contracts.SystemStateDTO`, `contracts.PositionStateDTO`.

---

### 7.2 Config Struct Fields (Task 7)

```go
// internal/app/config/dashboard_config.go
type DashboardConfig struct {
    ListenPort           int      `yaml:"listen_port"`
    CorsAllowedOrigins   []string `yaml:"cors_allowed_origins"`
    PollIntervalSeconds  int      `yaml:"poll_interval_seconds"`
    MaxEventsPerRequest  int      `yaml:"max_events_per_request"`
    GateEvidenceDir      string   `yaml:"gate_evidence_dir"`
    ConfigManifestDir    string   `yaml:"config_manifest_dir"`
    DestructiveConfirmTTLSeconds int `yaml:"destructive_confirm_ttl_seconds"`
}
```

Env vars (never in YAML):

| Env var                       | Used by                  | Purpose                                                  |
| ----------------------------- | ------------------------ | -------------------------------------------------------- |
| `DASHBOARD_API_KEY`           | backend-dashboard        | API authentication                                       |
| `DASHBOARD_ALLOWED_OPERATORS` | backend-dashboard        | Comma-separated operator IDs (mirror Telegram allowlist) |
| `VITE_DASHBOARD_API_KEY`      | frontend-dashboard (dev) | Dev-only API key for Vite proxy                          |
| `DATABASE_URL`                | both processes           | Shared PostgreSQL DSN                                    |

---

### 7.3 YAML Config Paths

```yaml
# shared/config/dashboard.yaml (new)
dashboard:
  listen_port: 8090
  cors_allowed_origins:
    - "http://localhost:5173"
    - "http://localhost:3000"
  poll_interval_seconds: 30
  max_events_per_request: 50
  gate_evidence_dir: "output/logs"
  config_manifest_dir: "config"
  destructive_confirm_ttl_seconds: 60
```

Sniper config unchanged — `shared/config/pipeline.yaml`, `shared/config/chains.yaml`, etc. remain loaded only by sniper-bot.

---

### 7.4 Pipeline Funnel Semantics

From `database.PipelineStats` (adapter.go lines 638–684):

- Counts are **cumulative** — `Selected ≤ Validated ≤ DQPassed ≤ Detected`.
- `Rejected` and `Failed` are **non-cumulative** terminal counts.
- `DQ_SKIPPED` tokens are excluded from `DQPassed` and downstream funnel counts.
- Dashboard pipeline view must label columns L0–L10 matching mockup:
  - L0 = Detected (ingestion)
  - L1 = DQPassed
  - L2 = FeatureReady
  - L3 = EdgeDetected
  - L5 = Validated (L4 models are sub-stages — group under "Models" row in UI)
  - L6 = Selected
  - L7 = (capital events — derive from selection → allocation)
  - L8 = Executed
  - L9 = PositionOpen
  - L10 = Evaluated

Layer heartbeat: query latest `stage_completed` log metric per worker from structured logs or `events` table filtered by `event_type`.

---

### 7.5 Event Bus Pattern (Phase 5 commands)

Emit pattern (backend-dashboard):

```go
cmd := contracts.OperatorCommandDTO{
    CommandID:   contentHashID(payload),
    CommandType: "mode",
    IssuerID:    issuerFromAuth(ctx),
    Args:        map[string]string{"mode": "EXPLORATION"},
    Timestamp:   time.Now().UTC().Format(time.RFC3339),
}
// INSERT INTO events (event_id, event_type, payload, ...)
// event_type = "operator_command_event"
// ON CONFLICT DO NOTHING
```

Consume pattern (sniper-bot worker):

```go
// ClaimNextEvents with event_type = "operator_command_event"
// Dispatch to operator.ExecuteCommand(ctx, db, cmd)
// which calls same logic as Telegram buildModeFn / buildKillFn / buildResumeFn
```

Reference: `docs/reference/architecture.md` §2.2–2.3, `docs/reference/orchestrator_spec.md`.

---

### 7.6 Security Rules

| Rule                         | Implementation                                                              |
| ---------------------------- | --------------------------------------------------------------------------- | ------ | ----- | ----------- |
| API key                      | `os.Getenv("DASHBOARD_API_KEY")` — constant-time compare                    |
| No keys in dashboard process | `backend-dashboard` env excludes `SOLANA_*_PRIVATE_KEY`, `JITO_*`           |
| Bounded responses            | Events list max 200; gate evidence file max 256 KiB; request body max 4 KiB |
| CORS                         | Explicit origin list from YAML — no `*` in production                       |
| HTTPS                        | Production reverse proxy (nginx/Caddy) terminates TLS                       |
| Config manifest              | Return filenames + top-level keys only — redact values matching `/key       | secret | token | password/i` |

---

### 7.7 Gate Evidence Schema

From `scripts/gate_review_collect.sh` and `tests/fixtures/gate_phase2_pass_evidence.json`:

Key JSON fields for `GateEvidenceResponseDTO`:

```json
{
  "wsol_token_address_emitted": 0,
  "ingestion_valid_token_ratio": 0.92,
  "market_probes_backlog_ratio": 0.02,
  "dq_pass_or_risky_pass": 3,
  "traces_completed": 1,
  "shadow_observer_failed": 0,
  "throughput_verdict": "HEALTHY"
}
```

`throughput_verdict` enum: `CODE_DEFECT | MARKET_QUIET | GUARDRAILS_ACTIVE | HEALTHY`.

Criteria thresholds from `docs/plans/2026-06-10-profit-restoration-plan.md` §1.1 Phase 2 success criteria (Tasks 13–19).

---

### 7.8 Telegram Parity & Command Rules

From `internal/telegram/commands.go`:

- **Read-only commands** (no allowlist required, but warn if unconfigured): `/status`, `/pnl`, `/positions`, `/pipeline`, `/dq`, `/health`
- **Destructive commands** (require `AllowedUserIDs`): `/kill`, `/resume`, `/mode` (mode change logged)
- Dashboard Phase 5 must mirror: `DASHBOARD_ALLOWED_OPERATORS` env = same semantics
- Destructive confirm: dashboard uses two-step confirm; Telegram uses explicit command + allowlist

Parity test approach (Task 6):

1. Seed in-memory adapter with fixture state
2. Call `internal/operator.BuildOverview()` → serialize key fields
3. Call legacy `buildStatusFn()` → parse HTML for same field values
4. Assert equality

---

### 7.9 SystemState & Shadow Gate (Overview)

`contracts.SystemStateDTO` fields used by overview:

- `Mode` — BALANCED | STRICT | EXPLORATION | VERY_EXPLORATION | DEGRADED | HALTED
- `DrawdownPct`, `OpenPositions`, `TotalExposureUsd`
- `ActiveStrategyID`, `UpdatedAt`

Shadow gate (`internal/modules/health/shadow_gate.go`):

- Evaluates `GetShadowGateStats` — simulated trades, realized PnL bps aggregate
- Pass criteria from `shared/config/pipeline.yaml` → `execution.shadow_gate`
- Exposed on `/health` today as `shadow_gate` JSON block — reuse in overview API

---

### 7.10 Process Boundary Rules

| Concern                                   | Sniper-bot | Backend-dashboard           |
| ----------------------------------------- | ---------- | --------------------------- |
| Orchestrator + workers                    | ✅         | ❌                          |
| Wallet private keys                       | ✅         | ❌                          |
| RPC clients                               | ✅         | ❌                          |
| Telegram poller                           | ✅         | ❌                          |
| Dashboard REST API                        | ❌         | ✅                          |
| `database.Adapter` reads                  | ✅         | ✅                          |
| `database.Adapter` writes (events, state) | ✅         | ✅ (command events only)    |
| Migrations (`migrate` cmd)                | ✅         | ❌ (sniper runs migrations) |
| Port                                      | 8080       | 8090                        |

Rollback procedure:

1. `docker compose stop dashboard-api dashboard-ui`
2. Sniper continues on :8080
3. Operators use Telegram as before

---

### 7.11 API Route Table (complete v1)

| Method | Path                       | Phase | Auth                     |
| ------ | -------------------------- | ----- | ------------------------ |
| GET    | `/api/v1/health`           | 2     | optional                 |
| GET    | `/api/v1/overview`         | 2     | required                 |
| GET    | `/api/v1/pnl`              | 2     | required                 |
| GET    | `/api/v1/pipeline`         | 2     | required                 |
| GET    | `/api/v1/positions`        | 2     | required                 |
| GET    | `/api/v1/activity`         | 2     | required                 |
| GET    | `/api/v1/dq`               | 2     | required                 |
| GET    | `/api/v1/gate/evidence`    | 2     | required                 |
| GET    | `/api/v1/configs`          | 2     | required                 |
| GET    | `/api/v1/mode`             | 2     | required                 |
| GET    | `/api/v1/safety`           | 2     | required                 |
| POST   | `/api/v1/commands`         | 5     | required + allowlist     |
| POST   | `/api/v1/commands/confirm` | 5     | required + confirm token |

Query params (common): `chain` (solana|eth|bsc|all), `market` (market id), `window_hours` (default 24).

---

### 7.12 Mockup View List

From `docs/mockups/operator-dashboard.html` nav (lines 709–724):

| `data-view` | Nav section | Phase 1 behavior                                    |
| ----------- | ----------- | --------------------------------------------------- |
| `overview`  | Monitor     | Live KPIs + drill-down cards                        |
| `pipeline`  | Monitor     | L0–L10 funnel table                                 |
| `positions` | Monitor     | Open positions table                                |
| `activity`  | Monitor     | Event bus tail                                      |
| `dq`        | Quality     | DQ decision breakdown                               |
| `gate`      | Quality     | Gate review criteria grid                           |
| `mode`      | Control     | Read-only mode display (Phase 5: interactive)       |
| `safety`    | Control     | Read-only kill switch status (Phase 5: interactive) |
| `configs`   | Control     | YAML manifest table                                 |

Chain filter UI: global `chain-bar` applies to views marked chain-aware in mockup sidebar tip.

---

### 7.13 Validation SQL (post-deployment)

```sql
-- Recent events feed working
SELECT event_type, COUNT(*) FROM events
WHERE created_at > NOW() - INTERVAL '1 hour'
GROUP BY event_type ORDER BY COUNT(*) DESC LIMIT 10;

-- Overview state readable
SELECT mode, open_positions, total_exposure_usd, updated_at
FROM system_state LIMIT 1;

-- Pipeline funnel (24h)
-- Use adapter GetPipelineStats(24) — verify Detected > 0 during active market

-- Operator command audit (Phase 5)
SELECT event_id, payload->>'command_type', created_at
FROM events WHERE event_type = 'operator_command_event'
ORDER BY created_at DESC LIMIT 5;
```

---

## Related Documents

| Document                                             | Relationship                                                        |
| ---------------------------------------------------- | ------------------------------------------------------------------- |
| `docs/specs/2026-06-10-operator-dashboard-design.md` | Design source (Approach A, IA, out of scope)                        |
| `docs/mockups/operator-dashboard.html`               | Visual source of truth                                              |
| `docs/plans/2026-06-10-profit-restoration-plan.md`   | Pipeline profit plan — orthogonal; gate criteria referenced in §7.7 |
| `docs/plans/2026-05-10-rescan-plan.md`               | Precedent for standalone plan documents                             |
| `docs/reference/architecture.md`                     | Update in Task 20/25 with 3-process topology                        |
| `docs/reference/db_adapter_spec.md`                  | Adapter interface rules for new read methods                        |
| `docs/ops/PROGRESS_REPORT.md`                        | Task completion log                                                 |
