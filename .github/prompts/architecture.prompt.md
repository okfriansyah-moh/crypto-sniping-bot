# Architecture Generation Prompt

You are a Staff+ backend architect. Generate a complete `docs/architecture.md` for a new project based on the skeleton-parallel framework.

## Instructions

1. Read `.github/copilot-instructions.md` for hard architectural constraints
2. Read `docs/STARTER_GUIDE.md` for the expected document structure
3. Ask the user for:
   - **System goal** — What does this system do? (one sentence)
   - **Input** — What goes in?
   - **Output** — What comes out?
   - **Pipeline stages** — What are the sequential processing steps?
   - **Domain entities** — What are the core data objects?

## Architecture Document Structure

Generate `docs/architecture.md` with these sections:

### 1. System Goal

One-paragraph description of what the system does.

### 2. Pipeline Stages

Define the strict sequential pipeline:

```
stage_1 → stage_2 → stage_3 → ... → stage_N
```

Each stage gets a name, input DTO, output DTO, and brief description.

### 3. Module Breakdown

One module per pipeline stage under `app/modules/`. Each module:

- Accepts frozen DTOs as input
- Returns frozen DTOs as output
- Has no side effects, no cross-module imports
- Is called only by the orchestrator

### 4. Data Model

Define database tables:

- Primary entities table(s)
- Pipeline runs table (tracks execution state)
- Entity state tracking table(s)
  All with content-addressable IDs.

### 5. DTO Registry

List all DTOs with:

- Name, source file in `contracts/`
- Producer module, consumer module(s)
- Key fields and constraints

### 6. Configuration

Define `config/pipeline.yaml` structure with all tunable parameters.

### 7. State Machine

Define state transitions for:

- Pipeline runs: `started → processing → completed | partial | failed`
- Entities: `created → queued → processed → completed | failed`

### 8. Failure Handling

Define thresholds, retry policies, and degradation strategies.

## Constraints

- Enforce modular monolith — no microservices
- Enforce determinism — same input = same output
- Enforce idempotency — content-addressable IDs, ON CONFLICT DO NOTHING
- Enforce orchestrator authority — only orchestrator calls modules
- Enforce database adapter — all DB access through `database/adapter.*`

## Output

Write the completed architecture to `docs/architecture.md`.
