# Parallel Development Guide — `run_parallel.sh`

> Operator guide for running multiple implementation phases simultaneously using
> autonomous AI agents. Supports 3 execution modes that balance speed, cost, and
> merge complexity.

---

## 1. Overview

This framework supports parallel development of pipeline phases. Most phases own isolated
modules under `app/modules/` and communicate only through immutable DTOs in `contracts/`.
This isolation enables parallel development — multiple phases implemented at the same time
by independent AI agents.

However, parallelism has tradeoffs:

| Dimension      | More Parallelism              | Less Parallelism        |
| -------------- | ----------------------------- | ----------------------- |
| **Speed**      | Faster wall-clock time        | Slower (sequential)     |
| **Token cost** | Higher (each agent re-reads)  | Lower (shared context)  |
| **Merge risk** | More conflicts at integration | Fewer conflicts         |
| **Debugging**  | Harder (concurrent sessions)  | Easier (single session) |

Three execution modes let the operator choose the right balance for the situation.

---

## 2. Model Routing Strategy

Each mode uses a different **heavy model** for its most complex phase/group, plus a shared
round-robin **rotation pool** for all other agents.

| Mode                     | Heavy Model         | Used For                               |
| ------------------------ | ------------------- | -------------------------------------- |
| Mode 1 (Full Parallel)   | `claude-opus-4.6`   | Heaviest phase (most complex by score) |
| Mode 2 (Token-Optimized) | `claude-sonnet-4.6` | Single session (all phases)            |
| Mode 3 (Hybrid)          | `claude-sonnet-4.6` | Heaviest group (by complexity score)   |

**Rotation pool** (round-robin, used for all other phases and remediation agents):

```
claude-sonnet-4.6 → claude-sonnet-4.5 → gpt-5.3-codex → gpt-5.4
```

Used for: non-heavy phases, conflict-resolver, post-merge review, docs sync, quality gate
remediation, and integration remediation.

**Environment overrides:**

```bash
MODEL_HEAVY="claude-opus-4.6"         # Override Mode 1 heavy model
MODEL_HEAVY_LITE="claude-sonnet-4.6"   # Override Modes 2 & 3 heavy model
```

---

## 3. Mode Definitions

### Mode 1 — Full Parallel (Maximum Speed)

Each phase runs in a **separate Git worktree** with a **dedicated Copilot CLI agent**.
All phases execute simultaneously. The heaviest phase (highest complexity score) gets
`claude-opus-4.6`; all others rotate through the pool.

**How it works:**

```text
main
 ├─ track/phase-2   ← worktree 1, checkpoint + agent pipeline (bounded retries)
 ├─ track/phase-3   ← worktree 2, checkpoint + agent pipeline (bounded retries)
 └─ track/phase-4   ← worktree 3, checkpoint + agent pipeline (bounded retries)
```

1. Creates a branch per phase from `main`
2. Creates a Git worktree per branch (sibling directories)
3. Generates a `PHASE_TASK.md` instruction file in each worktree
4. Creates **checkpoint** (`git tag checkpoint-phase-N-pre`) in each worktree
5. Runs `scripts/hooks/setup-env.sh` in each worktree (non-fatal if absent)
6. Runs the **agent pipeline** per worktree with **bounded retries**:
   - `scripts/hooks/activate-env.sh` — activates runtime env before agent
   - `phase-builder` — implements the phase (up to 5 retries)
   - `dto-guardian` — validates DTO contracts (up to 5 retries)
   - `integration` — validates module wiring (up to 5 retries)
   - `refactor` — fixes quality gate failures (up to 3 retries)
   - If any stage exceeds retry limit → rollback to checkpoint
7. Tracks per-phase status in `.parallel-dev/phase-status.json`
8. Resource control: max `MAX_PARALLEL_AGENTS` (default 3) concurrent pipelines
9. Waits for all agent pipelines to finish
10. **Auto-merges** all branches into an integration branch (union strategy, bounded retries)
    - Conflicts resolved automatically by `conflict-resolver` agent (up to 5 retries)
11. **Post-merge review** via `merge-reviewer` agent — validates DTO flow, module boundaries, orchestrator authority
12. **Documentation sync** via `merge-reviewer` agent — detects implementation drift from `docs/` specs (advisory)
13. Global validation + orchestrator authority check
14. **Creates PR automatically** via `gh pr create` (pushed to `origin`)

**When to use:**

- Deadline pressure — need maximum throughput
- All phases in the batch are independent (no shared file ownership)

---

### Mode 2 — Token-Optimized (Serial Grouping)

Multiple phases run **sequentially in a single Copilot CLI session**. No worktrees.
Context is shared across phases. Always uses `claude-sonnet-4.6` as the model.

**How it works:**

```text
main
 └─ track/group-2-3-4  ← single branch, checkpoint + agent pipeline (bounded retries)
     Phase 2 → commit → Phase 3 → commit → Phase 4 → commit
     dto-guardian → integration → refactor (bounded retries) → global validation
```

1. Creates a single branch from `main`
2. Generates a single `PHASE_TASK.md` with all phases listed in order
3. Creates **checkpoint** (`git tag checkpoint-group-X-pre`)
4. Runs `scripts/hooks/activate-env.sh` (non-fatal if absent)
5. Runs the **agent pipeline** with **bounded retries**
6. Each phase is committed before starting the next
7. **Post-merge review** via `merge-reviewer` agent — validates DTO flow, module boundaries, orchestrator authority
8. **Documentation sync** via `merge-reviewer` agent (advisory)
9. Global validation + orchestrator authority check
10. **Creates PR automatically** via `gh pr create` (pushed to `origin`)

**When to use:**

- Cost-sensitive development (limited premium requests)
- Phases have sequential dependencies
- Debugging a specific pipeline section end-to-end

**Grouping rules:**

- Maximum **3 phases per session** (beyond this, context window saturates)
- Phases must be in dependency order (earlier phases first)
- DTO-producing phases go before DTO-consuming phases

---

### Mode 3 — Hybrid (Balanced) — DEFAULT

Groups of phases run **in parallel across groups**, but **sequentially within each group**.
Combines the isolation of Mode 1 with the context sharing of Mode 2. The heaviest group
gets `claude-sonnet-4.6`; other groups rotate through the pool.

**How it works:**

```text
main
 ├─ track/group-a  ← worktree 1, checkpoint + agent pipeline (bounded retries)
 └─ track/group-b  ← worktree 2, checkpoint + agent pipeline (bounded retries)
```

1. Groups phases by dependency and file ownership
2. Creates a branch + worktree per group
3. Creates **checkpoint** per group
4. Runs `scripts/hooks/setup-env.sh` in each worktree (non-fatal if absent)
5. Each group runs the **agent pipeline** with **bounded retries**:
   - `scripts/hooks/activate-env.sh` — activates runtime env before agent
6. Tracks per-group status in `.parallel-dev/phase-status.json`
7. Groups execute in parallel (independent worktrees)
8. **Auto-merges** all group branches into integration branch (union strategy, bounded retries)
   - Conflicts resolved automatically by `conflict-resolver` agent (up to 5 retries)
9. **Post-merge review** via `merge-reviewer` agent — validates DTO flow, module boundaries, orchestrator authority
10. **Documentation sync** via `merge-reviewer` agent (advisory)
11. Global validation + orchestrator authority check
12. **Creates PR automatically** via `gh pr create` (pushed to `origin`)

**When to use:**

- Default choice for most development sessions
- Balance between speed and cost
- Phases have natural groupings by pipeline section

---

## 4. Mode Selection Strategy

```text
                        ┌─────────────────────┐
                        │  How many phases?    │
                        └─────────┬───────────┘
                                  │
                    ┌─────────────┼─────────────┐
                    ▼             ▼              ▼
               1 phase      2–3 phases      4+ phases
                    │             │              │
                    ▼             ▼              ▼
              Mode 2         Mode 2          ┌──────┐
           (single session)  (single session)│ Are   │
                                             │ they  │
                                             │ indep?│
                                             └──┬───┘
                                           yes  │  no
                                            ┌───┘───┐
                                            ▼       ▼
                                         Mode 1   Mode 3
                                       (full par) (hybrid)
```

| Scenario                                    | Recommended Mode |
| ------------------------------------------- | ---------------- |
| Single phase implementation                 | Mode 2           |
| 2–3 phases with sequential dependency       | Mode 2           |
| 2–3 fully independent phases                | Mode 1           |
| 4+ phases, mix of dependent and independent | Mode 3           |
| Cost-constrained (limited premium requests) | Mode 2           |
| Deadline pressure, all phases independent   | Mode 1           |
| Default / unsure                            | Mode 3           |

---

## 5. Phase Grouping Rules

### Safe Parallel Combinations

Phases can run simultaneously when they own **different files**:

```text
✅ Phase A ‖ Phase C  — different modules, no shared files
✅ Phase B ‖ Phase D  — independent inputs
```

### Unsafe Combinations

```text
❌ Phase 0 ‖ anything   — Phase 0 creates shared infrastructure
❌ DTO changes ‖ module changes — Module depends on DTO definition
❌ orchestrator ‖ any module  — Concurrent changes conflict
```

### File Ownership

Each phase owns specific directories. Parallel phases MUST NOT share file ownership.
Define your ownership matrix in `docs/implementation_roadmap.md`.

---

## 6. Token Cost Optimization Strategy

### Skill-First Loading

All agents use the skills system from `.github/skills/` instead of re-reading full documentation:

| Full Doc                    | Tokens | Equivalent Skill              | Tokens | Savings |
| --------------------------- | ------ | ----------------------------- | ------ | ------- |
| `docs/architecture.md`      | ~5000  | pipeline + modularity skills  | ~600   | 88%     |
| `docs/dto_contracts.md`     | ~6000  | dto skill                     | ~400   | 93%     |
| `docs/orchestrator_spec.md` | ~4000  | pipeline + idempotency skills | ~500   | 88%     |

### Token Optimization Rules

1. **Reuse context within session** — Never re-read a document already loaded
2. **Skills first, docs second** — Load skills before falling back to raw documentation
3. **Prefer grouped execution** — Use Mode 2 or Mode 3 to share context
4. **No full-doc reads** — Load the relevant skill, then deep-dive into specific doc sections only if needed
5. **Progressive loading** — Skill discovery → skill body → doc section (only if needed)
6. **Skill injection is automatic** — `run_parallel.sh` injects skill references into every Copilot call

---

## 7. Resilience Framework

### Universal Retry Pattern

ALL stages follow the same deterministic execution pattern:

```text
execute → validate → fix → re-validate → bounded retry → success OR rollback
```

Every code path terminates in a defined state — **no infinite loops, no undefined state**.

### Retry Configuration

```bash
MAX_RETRIES_PHASE_BUILDER=5
MAX_RETRIES_DTO=5
MAX_RETRIES_INTEGRATION=5
MAX_RETRIES_MERGE=5
MAX_RETRIES_GLOBAL_VALIDATION=5
MAX_REMEDIATION_RETRIES=3          # quality gate remediation within pipeline
MAX_PARALLEL_AGENTS=3              # resource control
MODEL_HEAVY=claude-opus-4.6        # Mode 1 heavy model
MODEL_HEAVY_LITE=claude-sonnet-4.6 # Modes 2 & 3 heavy model
```

All retry limits are bounded. The system is **guaranteed to terminate**.

### Workspace Confinement

All agent prompts include a `_WORKSPACE_CONSTRAINT` clause that is injected automatically:

```
WORKSPACE CONSTRAINT: NEVER write any files, scripts, summaries, or reports to
/tmp, /var, /private, or any path outside this project directory. Write ALL output
files inside the project — use .parallel-dev/ for temporary artifacts and output/
for generated files.
```

This prevents `Permission denied` errors that occur when agents attempt to create
verification scripts or summary files in `/tmp` outside the allowed workspace.

### Checkpoint & Rollback

Before each phase/group, a Git tag checkpoint is created:

```bash
git tag checkpoint-${phase_label}-pre
```

If any stage exceeds its retry limit:

```bash
git reset --hard checkpoint-${phase_label}-pre
```

On success, the checkpoint is cleaned up:

```bash
git tag -d checkpoint-${phase_label}-pre
```

### Agent Pipeline (per phase/group)

```text
phase-builder (up to 5 retries)
  → dto-guardian (up to 5 retries)
    → integration (up to 5 retries)
      → refactor/quality gates (up to 3 retries)
        → success OR rollback
```

### Stage-Specific Validation

#### Phase Builder

- **Execute:** Copilot `phase-builder` agent implements phase
- **Validate:** Module compiles, no syntax errors, imports valid
- **Fix:** `refactor` agent fixes compilation issues
- **On failure:** Rollback to checkpoint

#### DTO Guardian (STRICT)

- **Execute:** Copilot `dto-guardian` agent validates `contracts/`
- **Validate:** All DTOs immutable, no missing/extra fields, no mutable defaults
- **Fix:** `dto-guardian` agent fixes DTO issues
- **On failure:** Rollback to checkpoint

#### Integration Agent

- **Execute:** Copilot `integration` agent validates cross-module wiring
- **Validate:** No cross-module imports, no DB in modules, deterministic ordering
- **Fix:** `refactor` agent removes violations
- **On failure:** Rollback to checkpoint

#### Global Validation (CRITICAL)

- **Checks:** All quality gates + DTO flow + orchestrator authority
- **Fix:** `refactor` agent (up to 5 remediation attempts)
- **On failure:** System enters `remediation_failed` state — operator intervention required

### Post-Phase Pipeline Stages

After all phase/group agents complete, the following pipeline stages run in sequence.
Each stage is tracked in `.parallel-dev/phase-status.json` with its own state, model,
exit code, and timestamp.

| Stage               | Agent            | Function                                            | Fatal?        |
| ------------------- | ---------------- | --------------------------------------------------- | ------------- |
| `post-merge-review` | `merge-reviewer` | DTO flow, module boundaries, orchestrator authority | Yes           |
| `docs-sync`         | `merge-reviewer` | Implementation drift from `docs/` specs             | No (advisory) |
| `global-validation` | orchestrator     | Quality gates + orchestrator authority check        | Yes           |
| `remediation`       | `refactor`       | Fix global-validation failures (up to 5 retries)    | Yes           |

### Per-Mode Recovery

| Mode   | Failure Scope          | Recovery Action                                                  |
| ------ | ---------------------- | ---------------------------------------------------------------- |
| Mode 1 | Single agent fails     | Rollback that phase. Other agents continue.                      |
| Mode 1 | Merge conflict         | `conflict-resolver` agent with bounded retry (up to 5).          |
| Mode 2 | Agent fails mid-group  | Rollback to checkpoint. Earlier commits preserved.               |
| Mode 2 | Context window full    | Split remaining phases into new session.                         |
| Mode 3 | Single group fails     | Rollback that group. Other groups continue.                      |
| Mode 3 | Merge conflict         | `conflict-resolver` agent with bounded retry (up to 5).          |
| All    | Post-merge review fail | `merge-reviewer` agent retries (up to 5). Then: `review_failed`. |
| All    | Global validation fail | `refactor` agent (up to 5). Then: defined `remediation_failed`.  |

### Quality Gate Checks (All Modes)

Quality gates are fully delegated to `scripts/hooks/quality-gates.sh`. The orchestrator
calls this hook and treats a non-zero exit code as a gate failure. Each project provides
its own gate implementation (Python/Node/Go/etc.)

Recommended checks to implement in the hook:

1. **Compile/import check** — Project compiles/imports successfully
2. **Lint check** — No lint errors in modified files
3. **Test check** — Test suite passes
4. **SQL check** — No database driver imports in `app/modules/`
5. **Cross-module check** — No cross-module imports between `app/modules/` packages
6. **Console check** — No unstructured console output in `app/modules/`
7. **DTO validation** — All DTOs in `contracts/` are immutable
8. **Orchestrator integrity** — No database imports in `app/modules/`
9. **Protected files** — Warns if `contracts/`, `database/`, or `docs/` were modified
10. **Deterministic ordering** — No unordered iteration of collections without explicit sorting

Gates 1–8 are **blocking** (cause failure). Gates 9–10 are **advisory**.

---

## 8. Status Display

Run `./scripts/run_parallel.sh status` at any time to see the live session state.

### Full Status Layout

```
═══ Parallel Development Status ═══

  Mode:               3 (Hybrid)
  Phases:             2 3 4
  Integration branch: integration/parallel-20260324-100000
  Status:             running
  Started:            2026-03-24T10:00:00Z
  Branches:           track/phase-2, track/phase-3, track/phase-4

  Model (heavy):      claude-sonnet-4.6
  Rotation pool:      claude-sonnet-4.6 → claude-sonnet-4.5 → gpt-5.3-codex → gpt-5.4

  Branch Progress:
    phase-2 (ingestion-scene-splitter)           2 commits        — feat(phase-2): implement ingestion
    phase-3 (processing)                         0 commits        — (no commits yet)

  Agent Status:
    Phase/Group                    State            Model                        Exit   Updated
    ────────────────────────────── ──────────────── ──────────────────────────── ────── ────────────────────
    phase-2 (ingestion-scene-spl.) complete         claude-opus-4.6              0      2026-03-24T10:30:00Z
    phase-3 (processing)           running          claude-sonnet-4.5            —      2026-03-24T10:15:00Z
    ──────────── Post-Phase Pipeline ────────────────────────────────────────────────────────
    post-merge-review              complete         claude-sonnet-4.6            0      2026-03-24T10:35:00Z
    docs-sync                      advisory_failed  claude-sonnet-4.5            1      2026-03-24T10:36:00Z
    global-validation              complete         N/A                          0      2026-03-24T10:37:00Z

  Log files:
    phase-2-phase-builder-1.log -> /path/to/log (12,345 bytes)
    phase-2-dto-guardian-1.log  -> /path/to/log (4,210 bytes)
    post-merge-review-1.log     -> /path/to/log (8,901 bytes)
    docs-sync.log               -> /path/to/log (3,102 bytes)
```

### Phase/Group State Values

| State             | Meaning                                              |
| ----------------- | ---------------------------------------------------- |
| `running`         | Agent pipeline actively executing                    |
| `complete`        | All stages passed; exit code 0                       |
| `failed`          | Exceeded retry limit; rolled back to checkpoint      |
| `timed_out`       | Per-phase timeout expired (exit code 124)            |
| `advisory_failed` | Docs-sync advisory check reported issues (non-fatal) |

### State File Location

Phase status is persisted atomically to `.parallel-dev/phase-status.json`.
The session state (mode, branches, status) is persisted to `.parallel-dev/state.json`.

---

## 9. Requirements

- **Bash 4+** — Required for associative arrays. macOS ships with bash 3.2; install via `brew install bash`
- **Git 2.5+** — Worktree support
- **Python 3** — Used by the YAML config parser and `update_phase_status()` (stdlib only, no packages required)
- **Copilot CLI** — For automated agent execution
- **GitHub CLI (`gh`)** — For PR creation. Auto-installed if absent (Homebrew/apt/dnf). Run `gh auth login` once
- **`COPILOT_GITHUB_TOKEN`, `GH_TOKEN`, or `GITHUB_TOKEN`** — Copilot auth token (checked at startup)
- **(Optional)** `timeout` or `gtimeout` — Per-phase timeout enforcement (falls back to shell watchdog)
- **Language-specific tools** — Belong in `scripts/hooks/`, not in `run_parallel.sh` itself

---

## 10. Fully Autonomous Pipeline

The `start` command runs the **entire pipeline** from implementation to PR without human
intervention:

```text
./scripts/run_parallel.sh start [--mode=1|2|3] <phases...>
        │
        ▼
[1] Per phase/group: phase-builder → dto-guardian → integration → refactor
        │  (bounded retries per stage; rollback to checkpoint on exceed)
        ▼
[2] Auto-merge all branches (union strategy)
         └─ conflict-resolver agent resolves conflicts (bounded retries)
        │
        ▼
[3] Post-merge review — merge-reviewer agent
         └─ DTO flow integrity + module boundaries + orchestrator authority
        │
        ▼
[4] Documentation sync — merge-reviewer agent (advisory, non-blocking)
        │
        ▼
[5] Global validation — quality gates + orchestrator authority
         └─ refactor agent remediates failures (bounded retries)
        │
        ▼
[6] git push + gh pr create  ───────────────────────────►  PR ready for review
```

The only step requiring a human is reviewing and merging the PR.

### Opting Out of Auto-Merge

To run agents without auto-proceeding to merge and PR:

```bash
./scripts/run_parallel.sh start --no-auto-merge [--mode=1|2|3] <phases...>
# Agents run, then stop. You can inspect before:
./scripts/run_parallel.sh merge
```

### Partial Failure Handling

If some (but not all) agents fail in Modes 1/3, auto-merge is **skipped**. You see:

```
[WARN] Some agent(s) failed — skipping auto-merge.
[INFO] Fix failures then run: run_parallel.sh merge
```

Failed phases are rolled back to checkpoint; successful phases remain on their branches.

---

## 11. Hook System

All language-specific operations are delegated to hook scripts under `scripts/hooks/`.
The orchestrator (`run_parallel.sh`) **never needs project-specific modification**.

### Required Hook Files

| Hook file                        | When called                       | Purpose                                              |
| -------------------------------- | --------------------------------- | ---------------------------------------------------- |
| `scripts/hooks/setup-env.sh`     | Worktree creation (Modes 1 & 3)   | Install deps (pip install, npm ci, go mod download…) |
| `scripts/hooks/activate-env.sh`  | Before each agent run (all modes) | Activate runtime env (.venv, nvm, …)                 |
| `scripts/hooks/validate.sh`      | After phase-builder + after merge | Syntax/compile/import checks                         |
| `scripts/hooks/quality-gates.sh` | Quality gates check               | Lint + tests + arch checks                           |

### Hook Contract

- **Missing hook** → warning logged, execution continues (non-blocking)
- **Hook exits 0** → success
- **Hook exits non-zero** → failure (triggers retry or rollback per stage)
- Hooks run with `cwd` set to the worktree/project root
- Hooks receive no arguments by default (add project-specific logic inside)

### Session State

The orchestrator persists session-level state to `.parallel-dev/state.json`, including
model routing information for visibility:

```json
{
  "mode": 1,
  "phases": "2 3 4",
  "integration_branch": "integration/parallel-20260324-100000",
  "branches": ["track/phase-2", "track/phase-3", "track/phase-4"],
  "started_at": "2026-03-24T10:00:00Z",
  "status": "running",
  "model_heavy": "claude-opus-4.6",
  "model_rotation_pool": [
    "claude-sonnet-4.6",
    "claude-sonnet-4.5",
    "gpt-5.3-codex",
    "gpt-5.4"
  ]
}
```

### Phase Status Tracking

Each phase/group writes structured status to `.parallel-dev/phase-status.json`,
including which model the agent is using:

```json
{
  "phases": {
    "phase-2": {
      "phase": "phase-2",
      "state": "complete",
      "model": "claude-opus-4.6",
      "started_at": "2026-03-24T10:00:00Z",
      "exit_code": 0,
      "updated_at": "2026-03-24T10:30:00Z"
    },
    "phase-3": {
      "phase": "phase-3",
      "state": "running",
      "model": "claude-sonnet-4.5",
      "started_at": "2026-03-24T10:01:00Z",
      "updated_at": "2026-03-24T10:15:00Z"
    }
  }
}
```

States: `running` → `complete` | `failed` | `timed_out`

### Per-Phase Timeout

All agent subshells are wrapped with `run_with_timeout 1800` (30 minutes default).
A timed-out phase is recorded as `timed_out` in phase-status.json and treated as a
failure (triggers rollback). Override by setting `AGENT_TIMEOUT_SECONDS` before calling
the function directly.
