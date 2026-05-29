# Plan Management Reference — crypto-sniping-bot

This reference documents the canonical PLAN.md format for the `crypto-sniping-bot`
project and the Spec-Driven Development process it supports. Every plan generated or
updated by the `plan-management` skill **must** conform to this format exactly.

---

## External References

| Source                        | Path / URL                                                                             | Purpose                                             |
| ----------------------------- | -------------------------------------------------------------------------------------- | --------------------------------------------------- |
| Architecture spec             | `docs/architecture.md`                                                                 | Single source of truth for all design decisions     |
| Implementation roadmap        | `docs/implementation_roadmap.md`                                                       | Phase-based breakdown with exact file paths         |
| DTO contracts                 | `docs/dto_contracts.md`                                                                | Canonical DTO registry with field-level constraints |
| Database adapter spec         | `docs/db_adapter_spec.md`                                                              | Adapter interface + migration strategy              |
| Orchestrator spec             | `docs/orchestrator_spec.md`                                                            | Execution model, checkpointing, resume, failure     |
| Parallel development guide    | `docs/PARALLEL_DEV.md`                                                                 | Multi-agent phase execution and conflict resolution |
| Production gate analysis      | `docs/PRODUCTION_GATE_ANALYSIS.md`                                                     | Health report, open issues, design plans            |
| Profitability gaps            | `docs/PROFITABILITY_GAPS.md`                                                           | Known gaps with remediation priorities              |
| Progress report               | `docs/PROGRESS_REPORT.md`                                                              | Completed phases, agent pipeline results            |
| Spec-Driven Development (SDD) | https://jurnal.atlassian.net/wiki/spaces/DFS/pages/50632556624/Spec-Driven+Development | Mekari engineering process governing task breakdown |

---

## 1. PLAN.md File Naming & Location

| Scenario                            | Path                                      |
| ----------------------------------- | ----------------------------------------- |
| Standard feature/fix plan (default) | `docs/plans/YYYY-MM-DD-<topic>-plan.md`   |
| Phase implementation plan           | `docs/plans/YYYY-MM-DD-phase<N>-plan.md`  |
| Production hardening plan           | `docs/plans/YYYY-MM-DD-hardening-plan.md` |

Always use lowercase kebab-case names. Never use opaque task codes (e.g., `plan-b3.md`).

---

## 2. File Header

Every PLAN.md begins with a metadata block using blockquote syntax:

```markdown
# PLAN.md — <Short Feature/Fix Name>

> **Version:** 1.0
> **Date:** YYYY-MM-DD
> **Author:** {team name or individual}
> **Status:** {Draft | Ready for Implementation | In Progress | Completed}
> **Source of Truth:** `docs/specs/YYYY-MM-DD-<topic>-design.md`
> **Pipeline Layers Affected:** {e.g., L1 (Data Quality), L7 (Capital), Config}
> **Profit Factors Affected:** {e.g., DataQuality, Capital — or "None (infra only)"}
```

**Rules:**

- `Source of Truth` points to the specific spec or doc section that this plan implements
- `Status` must be exactly one of the four values above
- `Pipeline Layers Affected` helps reviewers understand blast radius immediately
- `Profit Factors Affected` forces the author to confirm the profit invariant is preserved
- Increment `Version` each time a significant revision is made (task additions = minor: 1.0 → 1.1)

---

## 3. Required Sections (in order)

| #   | Section                  | Required | Purpose                                                          |
| --- | ------------------------ | -------- | ---------------------------------------------------------------- |
| 1   | Goal                     | ✅       | One paragraph: what is being built, why, and which layers change |
| 2   | Architecture Impact      | ✅       | Affected layers + DTO flow change + key decisions table          |
| 3   | Invariants Preserved     | ✅       | Explicit statement of which invariants this plan maintains       |
| 4   | Implementation Tasks     | ✅       | Dependency graph + individual task sections                      |
| 5   | Task Summary             | ✅       | Table: task, name, files, depends-on, complexity                 |
| 6   | How to Use This Plan     | ✅       | Usage instructions for implementers                              |
| 7   | Deep Knowledge Reference | ✅       | DTOs, algorithms, config paths, security rules for this feature  |

Section 7 is always required — task sessions must be self-contained; the implementer
should never need to re-read the full architecture doc mid-session.

---

## 4. Section 1 — Goal

```markdown
## 1. Goal

{One clear paragraph: what is being built, which pipeline layers are affected, and
what problem it solves. Reference the source spec or PRODUCTION_GATE_ANALYSIS section.}

{Optional: phase breakdown or sub-goals as bullet list}

**Why:** {One sentence explaining the profit or safety motivation.}
**Profit factor(s) affected:** {e.g., DataQuality — better reject rate means fewer bad trades}
```

---

## 5. Section 2 — Architecture Impact

```markdown
## 2. Architecture Impact

### Affected Pipeline Layers

| Layer  | Module path                      | Change type                          |
| ------ | -------------------------------- | ------------------------------------ |
| L1     | `internal/modules/data_quality/` | New mode-aware threshold logic       |
| Config | `config/data_quality.yaml`       | New per-mode fields added            |
| DTO    | `contracts/data_quality.go`      | New `SKIP` decision value (additive) |

### DTO Flow (before → after)
```

Before: MarketDataDTO → ProcessForMode() → DataQualityDTO (PASS|RISKY_PASS|REJECT)
After: MarketDataDTO → ProcessForMode() → DataQualityDTO (PASS|RISKY_PASS|REJECT|SKIP)

```

### Key Decisions

| Decision | Rationale |
|---|---|
| {Decision 1} | {Why this approach, not an alternative} |
| {Decision 2} | {Why this approach, not an alternative} |
```

The decisions table prevents implementers from second-guessing choices already made
in the source spec. Include at minimum one entry per design choice made during planning.

---

## 6. Section 3 — Invariants Preserved

This section is unique to this project. Every plan must explicitly state which invariants
it maintains. Use the checklist below and mark each item:

```markdown
## 3. Invariants Preserved

This plan maintains the following architecture invariants:

- [x] **Profit invariant**: `Profit = Edge × Probability × Execution × Capital × DataQuality × AdaptationQuality` — all 6 factors preserved
- [x] **Determinism**: same input + same config = identical output — no randomness introduced
- [x] **Idempotency**: content-addressable IDs, `ON CONFLICT DO NOTHING`
- [x] **Module isolation**: no cross-module imports; all comms via `contracts/` DTOs
- [x] **No direct DB access from modules**: adapter-only pattern preserved
- [x] **DTO additive-only**: no existing fields renamed, removed, or type-changed
- [x] **Config-driven**: all new thresholds in `config/` YAML, not hardcoded in Go
- [x] **Event bus backbone**: all state transitions flow through PostgreSQL `events` table
- [x] **Security invariants**: HTTPS-only URLs, API keys via `os.Getenv`, bounded HTTP bodies
- [x] **Layer-1 hard rejects intact**: serial launcher (STRICT/BALANCED), no-social-links, high-supply
- [ ] **Migrations append-only**: {mark if this plan adds a migration}

**Factors NOT affected by this plan:** {list which of the 6 profit factors are untouched}
```

Any invariant marked `[ ]` must have an explanation of why it is intentionally not checked.

---

## 7. Section 4 — Implementation Tasks

### Dependency Graph

Always include a dependency graph before the first task. Use ASCII art:

```
Task 1 (Migration: add schema)
    │
    ▼
Task 2 (Contracts: add SKIP to DataQualityDTO)
    │
    ▼
Task 3 (Config: add mode-profile fields to DataQualityModeProfile struct)
    │
    ▼
Task 4 (Config YAML: add per-mode thresholds in data_quality.yaml)
    │
    ▼
Task 5 (Module: replace serial launcher logic in ProcessForMode)
    │
    ▼
Task 6 (Module: update canonicalProfile fallback in decision.go)
    │
    ▼
Task 7 (Tests + build validation + PROGRESS_REPORT.md)
```

**Ordering rules:**

1. Migrations before any code that depends on new schema
2. `contracts/` changes before any module that produces/consumes the new fields
3. `internal/app/config/` struct changes before YAML and module changes
4. YAML changes before module changes that read new config fields
5. Lower pipeline layers (L1 before L2, L2 before L3, …) for multi-layer plans
6. The **last task** is always: `go build ./...` + `go vet ./...` + `go test ./...` +
   `docs/PROGRESS_REPORT.md` update

### Individual Task Section Format

```markdown
### Task N — {Task Name}

**Goal:** {One sentence: what this task produces and why it is needed.}

**Layer(s) affected:** {e.g., L1 (Data Quality) | Config | Contracts | DB Migration}

**Files to create/modify:**

- `path/to/file.go` (create|modify) — short description
  - Key function/struct change: `func ProcessForMode(…)` — replace serial launcher blocks
  - Constraint: must not modify any existing struct fields, only add new ones
  - Cross-reference: §7.2 Config struct fields; §7.4 Operational modes
- `path/to/another.go` (create|modify) — short description

**Invariant check:**

- [ ] No SQL or DB driver imports in this module file
- [ ] No imports from other `internal/modules/` packages
- [ ] All thresholds read from config, not hardcoded
- [ ] No `math/rand` or other randomness
- [ ] DTO changes additive-only (if applicable)
- [ ] Bounded `io.LimitReader` used for any new HTTP response reading

**Validation:**

- `go build ./...`: zero build errors
- `go vet ./...`: zero vet issues
- `go test ./internal/modules/data_quality/...`: all tests green
- `go test ./...`: all 38+ packages green

**Prompt context needed:** {§7.X sub-section numbers — e.g., §7.1 DTO fields, §7.3 Mode profiles}

---
```

**Rules:**

- Each task must be completable in a **single focused chat session** — if it cannot, split it
- Each task must have a **concrete, runnable validation step**
- Tasks must never share files — if two tasks modify the same file, sequence them explicitly
- The last task always covers: `go build + vet + test` + PROGRESS_REPORT.md update
- **`Prompt context needed`** lists exactly which §7 sub-sections to include in the session

---

## 8. Section 5 — Task Summary Table

```markdown
## 5. Task Summary

| Task | Name                     | Files                                                    | Depends On | Est. Complexity |
| ---- | ------------------------ | -------------------------------------------------------- | ---------- | --------------- |
| 1    | DB Migration             | `database/migrations/20260101000NNN_*.sql`               | —          | Low             |
| 2    | DTO Contract Change      | `contracts/data_quality.go`                              | Task 1     | Low             |
| 3    | Config Struct Extension  | `internal/app/config/data_quality_runtime_config.go`     | Task 2     | Low             |
| 4    | YAML Threshold Addition  | `config/data_quality.yaml`                               | Task 3     | Low             |
| 5    | Module Logic Replacement | `internal/modules/data_quality/data_quality.go`          | Task 4     | High            |
| 6    | Fallback Profile Update  | `internal/modules/data_quality/decision.go`              | Task 5     | Medium          |
| 7    | Tests + Validation       | `tests/modules/data_quality/`, `docs/PROGRESS_REPORT.md` | Task 6     | Medium          |
```

**Complexity guide:**

- **Low:** Boilerplate, YAML edits, config struct fields, additive DTO fields, or pure SQL
- **Medium:** Logic with multiple code paths, new worker loop, multi-file coordination
- **High:** Complex algorithms (probability models, capital sizing), security-critical code,
  multi-layer integration, or code that directly affects the profit invariant

---

## 9. Section 6 — How to Use This Plan

Always include exactly this block (update the source of truth path and layer list):

```markdown
## 6. How to Use This Plan

1. **Start each task in a fresh chat session** — share this PLAN.md + the relevant §7
   sub-sections listed under "Prompt context needed" for that task
2. **Validate after each task** — run `go build ./...` + `go vet ./...` + `go test ./...`
   before moving to the next task. Fix any issue before proceeding.
3. **Do not skip tasks** — the dependency graph enforces ordering; a later task failing
   because an earlier one was skipped wastes the entire session
4. **One task at a time** — do not attempt multiple tasks in a single session
5. **Source of truth** — always refer to `{source of truth path}` for exact design
   decisions. This PLAN.md is the breakdown strategy; the spec is the specification.
6. **Invariants are non-negotiable** — if an implementation step seems to require
   violating an invariant, stop and flag it for design review. Do not work around
   invariants silently.
7. **Update PROGRESS_REPORT.md** in the final task — add a row to the Phase Progress
   table and record the outcome
```

---

## 10. Section 7 — Deep Knowledge Reference

Always required. Its purpose is to make every task session **self-contained** — the
implementer should never need to re-read `docs/architecture.md` or `docs/dto_contracts.md`
mid-session.

### What must be in Section 7 for every plan

| §7.N | Content to include                                                               |
| ---- | -------------------------------------------------------------------------------- |
| 7.1  | Relevant DTO fields (from `contracts/*.go` and `docs/dto_contracts.md`)          |
| 7.2  | Config struct fields affected (from `internal/app/config/`)                      |
| 7.3  | YAML config paths and current values (from `config/*.yaml`)                      |
| 7.4  | Operational mode behavior (if plan is mode-aware)                                |
| 7.5  | Event bus emit/consume pattern (if plan touches the backbone)                    |
| 7.6  | Security rules relevant to this plan                                             |
| 7.7  | Algorithm pseudocode or exact logic (if plan implements non-trivial computation) |
| 7.8  | Layer-1 hard rejects summary (if plan touches Layer 1)                           |
| 7.9  | Profit invariant statement + which factor(s) this plan optimizes                 |
| 7.10 | Validation queries (SQL) for verifying behavior post-deployment (if applicable)  |

Include only §7.N entries relevant to this specific plan. You do not need all 10 for
every plan.

### Structure

```markdown
## 7. Deep Knowledge Reference

This section contains complete schemas, business rules, algorithms, and data flows
needed by each task session. Include the specific §7.N sub-sections listed under
"Prompt context needed" for the task you are implementing.

---

### 7.1 Relevant DTO Fields

{Paste the relevant struct definition from `contracts/` verbatim. Mark new fields.}

### 7.2 Config Struct Fields

{Paste the relevant Go struct from `internal/app/config/` verbatim. Mark new fields.}

### 7.3 YAML Config Paths

{Show the relevant YAML block from `config/` verbatim, with current values.}

### 7.4 Operational Mode Behavior

{If plan is mode-aware, show the mode → behavior table.}
{Reference: docs/architecture.md §7 Operational Modes}

### 7.5 Event Bus Pattern

{If plan emits or consumes events, show the emit/consume code pattern.}
{Reference: docs/architecture.md §2.2–2.3}

...
```

---

## 11. Adding a New Task (Update Flow)

When adding a task to an existing PLAN.md:

1. **Append to Section 4** — add the new `### Task N+1` block after the last existing
   feature task (before the final validation task if one exists).
2. **Update the dependency graph** — add the new task node and arrow.
3. **Update Section 5 (Task Summary table)** — add a new row.
4. **Update Section 7 if needed** — add a new `### 7.N` sub-section only if the task
   requires knowledge not already covered.
5. **Do NOT renumber existing tasks** — append only; existing task numbers must never
   change.

### Determining insertion point

| Scenario                                      | Insert after                                       |
| --------------------------------------------- | -------------------------------------------------- |
| New feature extending an existing module      | That module's task; before final validation task   |
| New migration needed by an existing task      | Before the code task that depends on it            |
| New integration test phase                    | After all feature tasks                            |
| New config change supporting an existing task | Before the module task that reads the config field |
| Most cases                                    | Before the final `Tests + Validation` task         |

---

## 12. Quality Checklist Before Finalising a Plan

### Format checks

- [ ] Every task has a single, testable **Validation** step (`go build`, `go test`, etc.)
- [ ] No task modifies the same file as another task (or they are explicitly sequenced)
- [ ] Every task lists **Prompt context needed** (§7.N sub-section numbers)
- [ ] Dependency graph matches the task list (no missing arrows, no phantom tasks)
- [ ] Task Summary table matches the task list (same count, same names)
- [ ] The last task covers: build + vet + test + PROGRESS_REPORT.md update
- [ ] `Source of Truth` header points to the actual spec, not just `docs/architecture.md`
- [ ] `Status` is set correctly
- [ ] Migrations precede any code that depends on new schema in the dependency graph
- [ ] DTO contract tasks precede any module task that uses the new fields

### Invariant checks (fail the plan if any are not addressed)

- [ ] Plan does not add SQL or DB driver imports inside `internal/modules/`
- [ ] Plan does not add cross-module imports between `internal/modules/` packages
- [ ] Plan does not modify (rename, remove, change type of) any existing DTO fields
- [ ] Plan does not hardcode thresholds, URLs, or magic numbers in Go source
- [ ] Plan does not introduce `math/rand` or other randomness
- [ ] Plan does not bypass the three Layer-1 hard rejects in STRICT/BALANCED mode
- [ ] Plan does not add API keys or tokens to any YAML file
- [ ] Plan does not call Telegram APIs directly from a module (event bus only)
- [ ] Plan does not use `io.ReadAll` without `io.LimitReader` for any new HTTP call
- [ ] Plan does not disable or weaken a security invariant

### Deep knowledge checks

- [ ] Section 7 contains all DTO fields the implementer needs (no back-and-forth to contracts/)
- [ ] Section 7 contains all config struct fields and YAML paths for this plan
- [ ] Section 7 contains algorithm pseudocode if the plan implements non-trivial computation
- [ ] Section 7 contains validation SQL queries if behavior is hard to verify from logs alone

---

## 13. Pipeline Layer Quick Reference

For quickly identifying which module owns each layer:

| Layer | Name                         | Module path                      | Input DTO            | Output DTO                    |
| ----- | ---------------------------- | -------------------------------- | -------------------- | ----------------------------- |
| L0    | Ingestion                    | `internal/modules/ingestion/`    | On-chain event       | `MarketDataDTO`               |
| L0.5  | Rescan Worker                | `internal/workers/`              | DB read              | Re-emits `MarketDataDTO`      |
| L1    | Data Quality                 | `internal/modules/data_quality/` | `MarketDataDTO`      | `DataQualityDTO`              |
| L2    | Feature Extraction           | `internal/modules/features/`     | `DataQualityDTO`     | `FeatureDTO`                  |
| L3    | Signal & Edge                | `internal/modules/edge/`         | `FeatureDTO`         | `EdgeDTO`                     |
| L4    | Probability / Slip / Latency | `internal/modules/models/`       | `EdgeDTO`            | `ProbabilityEstimateDTO` etc. |
| L5    | Edge Validation              | `internal/modules/validation/`   | `ValidatedEdgeDTO`   | `ValidatedEdgeDTO` (gate)     |
| L6    | Selection                    | `internal/modules/selection/`    | `ValidatedEdgeDTO`   | `SelectionOutput`             |
| L7    | Capital                      | `internal/modules/capital/`      | `SelectionOutput`    | `AllocationDTO`               |
| L8    | Execution                    | `internal/modules/execution/`    | `AllocationDTO`      | `ExecutionResultDTO`          |
| L9    | Position                     | `internal/modules/position/`     | `ExecutionResultDTO` | `PositionStateDTO`            |
| L10   | Learning                     | `internal/modules/learning/`     | `PositionStateDTO`   | `LearningRecordDTO`           |

---

## 14. Spec-Driven Development Alignment

| SDD Step                      | PLAN.md Section                                                  |
| ----------------------------- | ---------------------------------------------------------------- |
| Define the spec               | Source of Truth header → `docs/specs/` or `docs/architecture.md` |
| Understand the system         | §1 Goal + §2 Architecture Impact                                 |
| Preserve invariants           | §3 Invariants Preserved                                          |
| Break into shippable tasks    | §4 Implementation Tasks                                          |
| Define done criteria per task | §4 Validation steps                                              |
| Track dependencies            | §4 Dependency graph                                              |
| Preserve deep knowledge       | §7 Deep Knowledge Reference                                      |

**Key SDD principle:** the source spec is never modified. The PLAN.md is the working
document. If the spec changes, a new plan version is created (bump Version field).

---

## 15. Mandatory Invariants Summary (memorise these)

These rules apply to every task in every plan. A plan is not valid if any task violates these.

| Rule                                        | Why it matters                                                                      |
| ------------------------------------------- | ----------------------------------------------------------------------------------- |
| Modules never import other modules          | Ensures pure function composition; prevents hidden coupling                         |
| All DB access via `database/adapter.*`      | Single abstraction boundary; switching DB engines requires only `database/` changes |
| DTO changes additive-only                   | Deployed consumers must never break; zero-value defaults = backward compat          |
| All IDs = `SHA256(content)[:16]`            | Determinism and idempotency in the event bus                                        |
| All thresholds from `config/*.yaml`         | Enables hot-reload, A/B testing, and versioned config snapshots                     |
| HTTPS-only for external endpoints           | Prevents credential interception in transit                                         |
| API keys via `os.Getenv` only               | Never leak keys in logs, version control, or config files                           |
| Bounded HTTP responses                      | Prevents OOM from adversarial responses                                             |
| Event bus for Telegram                      | Decouples alert delivery from business logic; prevents direct API coupling          |
| Layer-1 hard rejects never bypassed         | The most critical safety gate; bypassing costs capital                              |
| `ON CONFLICT DO NOTHING` everywhere         | Idempotent event writes; replay safety                                              |
| Orchestrator calls modules (not vice versa) | Enforces single responsibility; modules stay testable as pure functions             |
