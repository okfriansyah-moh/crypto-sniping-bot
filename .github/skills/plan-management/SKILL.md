---
name: plan-management
type: skill
description: >
  Generate or update a PLAN.md implementation plan for the crypto-sniping-bot project.
  Use for: creating a new plan from any context (spec, design doc, PRODUCTION_GATE_ANALYSIS
  finding, problem statement, Confluence link); adding new tasks to an existing PLAN.md;
  reviewing a plan for invariant compliance and completeness. Produces self-contained
  task breakdowns that each fit in one chat session, following the Spec-Driven
  Development format tailored for Go + 10-layer pipeline + PostgreSQL event bus.
  CRITICAL: every plan this skill produces must preserve all 6 profit factors, all
  architecture invariants, and all security rules — a plan that violates any invariant
  is not a valid plan.
argument-hint: "create | update | review"
---

# Plan Management

## When to Use

- **create** — User provides a spec, design document, PRODUCTION_GATE_ANALYSIS finding,
  implementation roadmap phase reference, problem statement, or any context and wants
  a new `PLAN.md`
- **update** — User wants to add a new task to an existing `PLAN.md` (append after the
  last feature task, before the final validation task)
- **review** — User wants to validate that an existing `PLAN.md` covers all required
  details from its source spec, preserves all invariants, and has correct task
  sequencing

## Reference

Before starting any work, read the full format reference:

- [PLAN.md format + Spec-Driven Development reference](./reference/reference.md)

---

## Codebase Context (always keep in mind)

This project is a **deterministic event-driven microstructure sniper system** — NOT a
chatbot, NOT a web service. Key facts:

| Concern            | Decision                                                                         |
| ------------------ | -------------------------------------------------------------------------------- |
| Language & runtime | Go 1.25 — module `crypto-sniping-bot`                                            |
| Architecture       | Modular monolith — single process, single repo, single PostgreSQL database       |
| Pipeline           | 10-layer sequential — NEVER reorder stages at runtime                            |
| Module comms       | Immutable DTOs in `shared/contracts/` only — no cross-module imports                    |
| Event backbone     | PostgreSQL append-only `events` table — `SELECT FOR UPDATE SKIP LOCKED` workers  |
| State authority    | Database is single source of truth — no in-memory-only state                     |
| Database access    | `shared/database/adapter.*` only — modules never import DB drivers                      |
| Orchestrator       | Only component that calls modules and writes to DB                               |
| Configuration      | YAML in `shared/config/` — zero hardcoded thresholds, paths, or magic numbers           |
| Security           | HTTPS-only, API keys via env vars, bounded HTTP bodies, no raw RPC error strings |
| Determinism        | Same input + same config = identical output — no randomness                      |
| Idempotency        | All IDs = `SHA256(content)[:16]` — `ON CONFLICT DO NOTHING` everywhere           |

**Core profit invariant (non-negotiable — every plan must preserve all six factors):**

```
Profit = Edge × Probability × Execution × Capital × DataQuality × AdaptationQuality
```

If any factor → 0, profit → 0.

**Directory layout:**

```
cmd/                         ← entry-point commands (server, migrate, hydrate, …)
contracts/                   ← immutable DTO definitions (additive-only)
config/                      ← YAML configuration (all tunable parameters)
database/
  adapter.go                 ← single DB entry point
  engines/postgres/          ← engine-specific implementation
  migrations/                ← append-only SQL migration files
internal/
  app/config/                ← Go structs mapping YAML → runtime config
  modules/                   ← pipeline stage implementations
    ingestion/               ← Layer 0  — MarketDataDTO
    data_quality/            ← Layer 1  — DataQualityDTO
    features/                ← Layer 2  — FeatureDTO
    edge/                    ← Layer 3  — EdgeDTO
    models/                  ← Layer 4  — ProbabilityEstimateDTO / SlippageEstimateDTO / LatencyProfileDTO
    validation/              ← Layer 5  — ValidatedEdgeDTO
    selection/               ← Layer 6  — SelectionOutput
    capital/                 ← Layer 7  — AllocationDTO
    execution/               ← Layer 8  — ExecutionResultDTO
    position/                ← Layer 9  — PositionStateDTO
    learning/                ← Layer 10 — LearningRecordDTO
    probes/                  ← Cross-cutting: Helius, DEXScreener, Solana metadata
    evaluation/              ← Post-trade evaluation
    state_machine/           ← Token lifecycle CAS transitions
    traceability/            ← Four-field traceability contract
    execution_solana/        ← Solana-specific execution path
    ingestion_solana/        ← Solana-specific ingestion path
    …                        ← other support modules
  orchestrator/              ← Pipeline orchestration + checkpointing
  rpc/                       ← RPC client pools, DEXScreener, price feeds
  telegram/                  ← Telegram dispatcher (event-bus-only)
  workers/                   ← Background worker loops
  ai/                        ← GroqClient AI enrichment (1-shot, fail-open)
docs/                        ← Architecture specs (read-only except PROGRESS_REPORT.md)
docs/specs/                  ← Design specs (output of brainstorming skill)
docs/plans/                  ← Implementation plans (output of plan-management skill)
tests/                       ← Unit + integration tests
scripts/                     ← Automation (run_parallel.sh, hooks, etc.)
```

---

## Use Case 1: Create a Plan from Scratch

### Input

The user provides one or more of:

- A spec file from `docs/specs/`
- A section from `docs/analysis/2026-05-20-production-gate-analysis.md`, `docs/analysis/profitability-gaps.md`, or
  `docs/reference/implementation_roadmap.md`
- A Confluence URL or page content
- A problem statement or feature description

### Procedure

1. **Read the reference** — load `./reference/reference.md` to internalize the format
   rules before producing any output.

2. **Gather all context** — read the source spec or problem statement in full. If the
   user provides a URL, fetch its content. If they reference an existing doc, read it.
   Understand the full scope before decomposing.

3. **Understand the system impact** — identify:
   - Which pipeline layers are affected (L0–L10)?
   - Which modules in `internal/modules/` need changes?
   - Which DTO contracts in `shared/contracts/` are touched? Are changes additive-only?
   - Does this touch the event bus, orchestrator, or database adapter?
   - What security invariants are relevant (HTTPS, env-only keys, bounded responses)?
   - Which of the six profit factors does this change affect?
   - Are there hard rejects in Layer 1 that must remain intact?
   - Does any part of this require a database migration?

4. **Decompose into tasks** — apply these rules:
   - Each task must be completable in a **single focused chat session** (~1–4 source files)
   - Tasks must have clear dependencies — no circular deps
   - **Ordering rules (strict):**
     - Database migrations first (before any code that depends on new schema)
     - DTO contract changes before any module that produces or consumes them
     - Platform/config layer changes before feature module changes
     - Lower pipeline layers (L0→L1→…) before higher layers that depend on them
     - The **last task** is always: tests + build validation + PROGRESS_REPORT.md update
   - Number tasks sequentially: Task 1, Task 2, … Task N

5. **Write Section 8 (Deep Knowledge)** — extract from `docs/reference/architecture.md` and the
   source spec:
   - DTO fields involved in this plan (from `shared/contracts/` and `docs/reference/dto_contracts.md`)
   - Profit invariant rules relevant to this feature
   - Config fields and YAML paths (from `shared/config/`)
   - Event bus emit/consume patterns (if this plan touches the backbone)
   - Hard rejects that must not be bypassed (if plan touches Layer 1)
   - Security rules relevant to this plan
   - Operational mode behavior (if plan is mode-aware)
   - Any algorithm pseudocode (exact, from `docs/reference/architecture.md`)

6. **Produce the PLAN.md** — following the exact structure from
   `./reference/reference.md`:
   - File header (version, date, author, status, source of truth)
   - §1 Goal
   - §2 Architecture Impact (affected layers + decisions table)
   - §3 Invariants Preserved
   - §4 Implementation Tasks (dependency graph + individual task sections)
   - §5 Task Summary table
   - §6 How to Use This Plan
   - §7 Deep Knowledge Reference

7. **Write the file** — save to `docs/plans/YYYY-MM-DD-<topic>-plan.md`.

8. **Verify** — run the quality checklist from `./reference/reference.md §10` before
   confirming done.

### Task Section Template (apply for each task)

```markdown
### Task N — {Task Name}

**Goal:** {One sentence describing what this task produces.}

**Layer(s) affected:** {L0 | L1 | … | L10 | Platform | Config | DB | Contracts}

**Files to create/modify:**

- `path/to/file.go` (create|modify) — {description}
  - {key function, interface, or struct change}
  - {important constraint or invariant to maintain}
  - {cross-reference to §7.X if detail lives there}

**Invariant check:**

- [ ] DTO changes additive-only (no field renames or removals)
- [ ] No direct DB access from module (goes through adapter)
- [ ] No cross-module imports (only `shared/contracts/`)
- [ ] Config values from YAML, not hardcoded
- [ ] No randomness introduced
- [ ] Security rules respected (if applicable)

**Validation:**

- `go build ./...`: zero build errors
- `go vet ./...`: zero vet issues
- `go test ./...`: all packages green
- {any specific test command for this task}

**Prompt context needed:** {docs/ sections — e.g., §7.1 DTO fields, §7.3 Event bus pattern}

---
```

---

## Use Case 2: Update a Plan — Add a New Task

### Input

The user provides:

- The existing `PLAN.md` (file path or content)
- Context for the new task (spec section, problem statement, Confluence URL)

### Procedure

1. **Read the reference** — load `./reference/reference.md`.

2. **Read the existing PLAN.md** — understand:
   - How many tasks exist (current last task number N)?
   - What layers and modules are already covered?
   - What §7 sub-sections already exist (avoid duplicating)?
   - Is there a final validation task? (the new task goes before it)

3. **Understand the new task context** — identify:
   - Which pipeline layer(s) does this task affect?
   - Which modules, DTOs, or config files are involved?
   - Does it introduce a new migration? (must precede code tasks that need it)
   - Does it require new deep knowledge entries in §7?
   - Which existing task does it depend on?

4. **Determine insertion point** — following `./reference/reference.md §9`:
   - New feature task → append after the last feature task, before the final
     validation task
   - New test/validation phase → append after the final validation task
   - New migration → insert before the code task that depends on it

5. **Make all edits atomically:**
   - Append the new `### Task N+1` block after the correct separator
   - Update the dependency graph
   - Update the Task Summary table
   - Add §7.N sub-sections if the task needs knowledge not already in §7

6. **Do NOT renumber existing tasks** — append only.

7. **Verify** — confirm task count in dependency graph matches §4 and Task Summary.

---

## Use Case 3: Review a Plan for Completeness and Safety

### Procedure

1. **Read the reference** — load `./reference/reference.md`.

2. **Read both files:**
   - The PLAN.md under review
   - Its source of truth (spec, `docs/reference/architecture.md` section, or problem statement)

3. **Check every section** against the quality checklist in `./reference/reference.md §10`.

4. **For each task, verify:**
   - All affected modules are covered
   - No task is too large (> 4 source files = likely needs splitting)
   - No task depends on a file not yet created by a prior task
   - Validation steps are concrete and runnable
   - DTO changes come before any module that uses the new fields
   - Migrations come before code that depends on new schema

5. **Invariant audit — check each task for violations:**
   - Does any task add SQL or DB imports directly in a module? → VIOLATION
   - Does any task create cross-module imports (module importing another module)? → VIOLATION
   - Does any task modify existing DTO fields (rename, remove, change type)? → VIOLATION
   - Does any task introduce `math/rand` or other randomness? → VIOLATION
   - Does any task hardcode a threshold, API URL, or magic number in Go code? → VIOLATION
   - Does any task bypass the four Layer-1 hard rejects? → VIOLATION
   - Does any task add an API key or token to a YAML file? → VIOLATION (env vars only)
   - Does any task call Telegram APIs directly from a module? → VIOLATION (event bus only)
   - Does any task use `io.ReadAll` without a `LimitReader`? → VIOLATION

6. **Report findings** — list what is covered, what is missing, what violates invariants,
   and what should be added. Offer to make updates.

---

## Output Conventions

- **File location:** `docs/plans/YYYY-MM-DD-<topic>-plan.md`
- **Section 7 cross-references:** use `§7.N` notation in task descriptions
- **Version:** start at `1.0`; increment minor for task additions, major for full rewrites
- **Status:** set to `Ready for Implementation` when plan is complete and verified
- **Complexity:** Low = config/YAML/boilerplate, Medium = non-trivial logic, High = complex
  algorithms, security-critical, or multi-layer integration
- **Task granularity:** aim for 30–90 minute focused sessions; never put an entire layer
  in one task

## Layer-Specific Notes

### Config / Platform tasks (Go)

- Validate with `go build ./...` + `go vet ./...`
- Struct changes to `internal/app/config/` always precede feature module tasks that use
  the new fields
- New YAML fields go in `shared/config/*.yaml` with sensible defaults — disabled by default
  (e.g., commented out) when the feature needs calibration before enabling

### Module tasks (Go — `internal/modules/`)

- Modules are **pure functions**: accept DTOs, return DTOs, zero side effects on shared state
- No SQL, no DB driver imports, no calls to other modules
- All thresholds from config (injected at construction, not hardcoded)
- Hard rejects in Layer 1 (`data_quality/`) must never be conditionally skipped for
  correctness-critical gates (serial launcher in STRICT/BALANCED, total supply, social links)

### Contracts tasks (Go — `shared/contracts/`)

- Additive-only: new fields with Go zero-value defaults only
- Must precede ALL module tasks that produce or consume the new fields
- DTO struct changes require a comment update in `docs/reference/dto_contracts.md` (add to §7 of plan)

### Database migration tasks

- Migration file naming: `20260101000NNN_description.sql`
- Always precede code tasks that depend on the new schema
- Use `ON CONFLICT DO NOTHING` (not `INSERT OR IGNORE`)
- Never modify existing migration files — append-only

### Event bus tasks

- Workers consume via `SELECT ... FOR UPDATE SKIP LOCKED`
- Each consumer tracks offset in `consumer_offsets`
- Content-addressable `EventID` = `SHA256(content)[:16]` — idempotent via `ON CONFLICT DO NOTHING`
- Use `"replay:"` prefix to isolate replay events from production workers

### Security-sensitive tasks

- HTTPS-only URLs enforced at construction (Jito, Groq, DEXScreener)
- Bounded HTTP responses: Jito 64 KiB, DEXScreener 128 KiB, Groq 4 KiB
- API keys via `os.Getenv` only — never in YAML, never logged
- RPC error messages truncated to 200 chars before surfacing
- gRPC auth tokens from env vars only — never in `shared/config/chains.yaml`
