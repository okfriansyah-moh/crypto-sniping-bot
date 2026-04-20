# Database Adapter Specification Prompt

You are a Staff+ backend architect. Generate a complete `docs/db_adapter_spec.md`.

## Instructions

1. Read `docs/architecture.md` — data model, tables, state machines
2. Read `docs/dto_contracts.md` — DTO definitions the adapter must accept/return
3. Read `.github/copilot-instructions.md` — database rules

## Document Structure

Generate `docs/db_adapter_spec.md` with:

### 1. Design Principles

- Single entry point: `database/adapter.*`
- Accepts and returns immutable DTOs — no raw rows, no dicts
- All SQL uses portable syntax compatible with all supported engines
- Engine-agnostic interface — switching engines requires changes only in `database/`

### 2. Adapter Interface

Define the public API:

```python
class DatabaseAdapter:
    def initialize(self, config: dict) -> None: ...
    def close(self) -> None: ...

    # Pipeline runs
    def create_run(self, run: PipelineRunDTO) -> None: ...
    def get_run(self, run_id: str) -> PipelineRunDTO | None: ...
    def update_run_stage(self, run_id: str, stage: str) -> None: ...

    # Entities (project-specific)
    def insert_entity(self, entity: EntityDTO) -> None: ...
    def get_entity(self, entity_id: str) -> EntityDTO | None: ...
    def update_entity_status(self, entity_id: str, status: str) -> None: ...
```

### 3. SQL Compatibility Rules

- `ON CONFLICT DO NOTHING` (not `INSERT OR IGNORE`)
- Parameterized queries only (no string interpolation)
- `TEXT` for strings, `INTEGER` for ints, `REAL` for floats
- `CURRENT_TIMESTAMP` for defaults
- No engine-specific functions

### 4. Engine Implementations

- Each supported engine gets its own module under `database/engines/`
- Engine selection is driven by configuration: `database.engine: <engine_name>`
- Engine implementations handle connection management and engine-specific settings

### 5. Migration Strategy

- Migration files in `database/migrations/`
- Naming: `YYYYMMDD000NNN_description.sql`
- Append-only — never modify existing migrations
- Auto-run on adapter initialization

### 6. Transaction Model

- Write operations wrapped in transactions
- Checkpoint writes are single-statement transactions
- Read operations use consistent snapshots

### 7. Connection Management

- Connection strategy is engine-specific (configured in `database/engines/`)
- Engine-specific optimizations (WAL mode, connection pooling, etc.) belong in the engine module

### 8. Idempotency Enforcement

- All INSERTs use `ON CONFLICT DO NOTHING` or `ON CONFLICT DO UPDATE`
- Content-addressable primary keys prevent duplicates
- Adapter validates DTO types before executing queries

## Output

Write the completed specification to `docs/db_adapter_spec.md`.
