# Roadmap Generation Prompt

You are a Staff+ backend architect. Generate a complete `docs/implementation_roadmap.md` from the project's architecture.

## Instructions

1. Read `docs/architecture.md` — the system architecture
2. Read `docs/dto_contracts.md` — DTO definitions (if they exist)
3. Read `.github/copilot-instructions.md` — hard constraints

## Roadmap Structure

Generate `docs/implementation_roadmap.md` with:

### Phase 0 — Core Infrastructure

- Database schema + migrations
- Database adapter (`database/adapter.*`)
- Configuration loader
- Logging setup
- Entry point (`app/main.py`)
- **Exit criteria:** DB creates tables, adapter CRUD works, config loads

### Phase 1..N — Pipeline Stages

For each pipeline stage (or group of related stages):

```markdown
## Phase X — [Stage Name(s)]

### Objective

What this phase delivers.

### Tasks

1. [ ] Create module package under `app/modules/`
2. [ ] Define input/output DTOs in `contracts/`
3. [ ] Implement core processing logic
4. [ ] Write database migration (if needed)
5. [ ] Add orchestrator wiring
6. [ ] Write unit tests (no GPU, no network)
7. [ ] Verify exit criteria

### Database Migrations

SQL for any new tables or columns.

### Input/Output DTOs

- Input: `SomeInputDTO` from `contracts/input.py`
- Output: `SomeOutputDTO` in `contracts/output.py`

### Exit Criteria

- [ ] Module accepts input DTO, returns output DTO
- [ ] All tests pass
- [ ] Idempotent: re-run produces no duplicates
- [ ] Deterministic: same input = same output
```

### System Priority Layers

Assign each phase to a priority tier:

- **P0 (Execution Blockers):** Infrastructure + critical path stages
- **P1 (Core Production):** Main processing stages
- **P1.5 (Quality & Optimization):** Enhancement stages
- **P2 (Enhancements):** Optional/future stages

### Parallel Development Strategy

Define which phases can run in parallel:

- File ownership matrix (which phase owns which directories)
- Safe parallel combinations
- Unsafe combinations (sequential dependencies)

## Output

Write the completed roadmap to `docs/implementation_roadmap.md`.
