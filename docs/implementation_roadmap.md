# Implementation Roadmap

> Generated using `.github/prompts/roadmap.prompt.md`.
> Defines the phase-by-phase implementation plan with exit criteria and parallel development strategy.

---

## System Priority Layers

| Priority | Phases      | Description                                          |
| -------- | ----------- | ---------------------------------------------------- |
| **P0**   | Phase 0     | Execution blockers — infrastructure must exist first |
| **P1**   | Phase 1–N   | Core pipeline — main processing stages               |
| **P1.5** | Enhancement | Quality improvements, optimization stages            |
| **P2**   | Future      | Optional features, monitoring, extensions            |

---

## Phase 0 — Core Infrastructure

**Priority:** P0 (Execution Blocker)
**Owns:** `database/`, `config/`, `app/main.py`, `app/orchestrator/`

### Objective

Set up the foundational infrastructure: database schema, adapter, configuration loader, logging, and entry point.

### Tasks

1. [ ] Create database schema migration (`database/migrations/20240101000001_initial_schema.sql`)
2. [ ] Implement database adapter (`database/adapter.*`)
3. [ ] Implement database engine (`database/engines/<engine>_engine.py`)
4. [ ] Create configuration loader and default `config/pipeline.yaml`
5. [ ] Set up structured logging
6. [ ] Create entry point `app/main.py`
7. [ ] Create orchestrator skeleton `app/orchestrator/pipeline.py`
8. [ ] Write unit tests for adapter, config loader, and orchestrator

### Database Migration

```sql
CREATE TABLE IF NOT EXISTS pipeline_runs (
    run_id               TEXT PRIMARY KEY,
    entity_id            TEXT NOT NULL,
    status               TEXT NOT NULL DEFAULT 'started',
    last_completed_stage TEXT,
    created_at           TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS entities (
    entity_id  TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'created',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Add project-specific tables here
```

### Exit Criteria

- [ ] Database creates all tables on first run
- [ ] Adapter CRUD operations work (insert, query, update)
- [ ] `ON CONFLICT DO NOTHING` prevents duplicates
- [ ] Config loads from YAML with defaults
- [ ] Logging outputs structured JSON
- [ ] Entry point runs without errors
- [ ] All tests pass

---

## Phase 1..N — Pipeline Stages

<!-- Copy this template for each phase -->

### Phase X — [Stage Name]

**Priority:** P1
**Owns:** `app/modules/stage_name/`, `contracts/stage_name.py`

#### Objective

Implement the [stage_name] module that [brief description of what it does].

#### Tasks

1. [ ] Define DTOs in `contracts/stage_name.py`
2. [ ] Create module package `app/modules/stage_name/`
3. [ ] Implement `__init__.py` with public `process()` function
4. [ ] Implement core logic in `stage_name.py`
5. [ ] Add database migration (if new tables needed)
6. [ ] Wire into orchestrator
7. [ ] Write unit tests with fixture data
8. [ ] Verify exit criteria

#### Input/Output DTOs

- **Input:** `PreviousStageResult` from `contracts/previous_stage.py`
- **Output:** `StageNameResult` in `contracts/stage_name.py`

#### Exit Criteria

- [ ] Module accepts input DTO, returns output DTO
- [ ] `@dataclass(frozen=True)` on all DTOs
- [ ] No cross-module imports (only `contracts/`)
- [ ] No database access in module
- [ ] All tests pass without GPU, network, or real data
- [ ] Idempotent: re-run produces no duplicates
- [ ] Deterministic: same input = same output

---

## Parallel Development Strategy

### File Ownership Matrix

| Phase   | Owned Directories                     | Owned Contracts        |
| ------- | ------------------------------------- | ---------------------- |
| Phase 0 | `database/`, `config/`, `app/main.py` | —                      |
| Phase 1 | `app/modules/stage_1/`                | `contracts/stage_1.py` |
| Phase 2 | `app/modules/stage_2/`                | `contracts/stage_2.py` |
| ...     | ...                                   | ...                    |

### Safe Parallel Combinations

Phases can run in parallel when they own **different files**:

```
✅ Phase 1 ‖ Phase 3  — different modules, different contracts
✅ Phase 2 ‖ Phase 4  — no shared files
```

### Unsafe Combinations

```
❌ Phase 0 ‖ anything — Phase 0 creates shared infrastructure
❌ Phase N ‖ Phase N+1 — if N+1 depends on N's output DTO
```

### Recommended Groupings

```
Group A: Phase 0 → Phase 1              (infrastructure → first stage)
Group B: Phase 2 → Phase 3              (independent stages)
Group C: Phase 4 → Phase 5              (later stages)
```
