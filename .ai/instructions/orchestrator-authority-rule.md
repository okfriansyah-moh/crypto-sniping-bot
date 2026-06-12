The orchestrator is the **ONLY** component that:

- Calls modules (modules never call each other)
- Manages execution order (the pipeline stage sequence)
- Performs checkpointing (writes `last_completed_stage` after each stage)
- Writes to the database (via `database/adapter.*`)
- Routes DTOs between modules (passes output of stage N as input to stage N+1)
- Handles failures (decides retry, skip, or abort)

Modules MUST:

- Be **pure functions** — accept DTOs, return DTOs, no side effects on shared state
- **Not call the database** — no imports from `database/`, no SQL, no adapter calls
- **Not call other modules** — no imports from other modules (only `contracts/`)
- **Not manage their own state** — all state lives in the database, managed by the orchestrator
- **Not perform checkpointing** — only the orchestrator decides when to persist progress

---