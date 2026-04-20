# Orchestrator Specification Prompt

You are a Staff+ backend architect. Generate a complete `docs/orchestrator_spec.md`.

## Instructions

1. Read `docs/architecture.md` — pipeline stages and data model
2. Read `docs/dto_contracts.md` — DTO flow between stages
3. Read `docs/db_adapter_spec.md` — database adapter interface
4. Read `.github/copilot-instructions.md` — hard constraints

## Specification Structure

Generate `docs/orchestrator_spec.md` with:

### 1. Execution Model

- Single-threaded, sequential stage execution
- Orchestrator is the ONLY component that calls modules
- Orchestrator is the ONLY component that calls the database adapter

### 2. Stage Ordering

Define the immutable pipeline sequence from `docs/architecture.md`.

### 3. Checkpointing

- After every stage completion: write `last_completed_stage` to DB
- Checkpoint is a single SQL UPDATE in a transaction
- No skip-forward (advance by exactly one stage)

### 4. Resume Behavior

- On restart: query DB for existing run
- If completed → exit early
- If incomplete → reconstruct DTOs from DB, resume from next stage

### 5. Pre-Flight Checks

- Validate runtime dependencies (Python version, external tools)
- Check disk space
- Validate input data (exists, readable, correct format)

### 6. State Transitions

```
Pipeline: started → processing → completed | partial | failed
Entity:   created → queued → processed → completed | failed
```

No backward transitions. Terminal states are final.

### 7. Failure Handling

- Per-entity retry with bounded attempts
- Threshold-based abort (e.g., >50% entities fail → abort)
- Graceful degradation for optional stages
- All failures logged with structured JSON

### 8. DTO Routing

For each stage boundary, define:

- Output DTO of stage N
- Input DTO of stage N+1
- Any aggregation or fan-out logic

### 9. Database Interaction

- All DB access through `database/adapter.*`
- Adapter accepts/returns frozen DTOs
- All SQL uses portable syntax

### 10. Idempotency

- Content-addressable IDs for all entities
- ON CONFLICT DO NOTHING for all inserts
- Skip-if-exists for file operations

## Output

Write the completed specification to `docs/orchestrator_spec.md`.
