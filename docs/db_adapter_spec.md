# Database Adapter Specification

> Generated using `.github/prompts/db_adapter.prompt.md`.
> Defines the database abstraction layer, adapter interface, and migration strategy.

---

## 1. Design Principles

- **Single entry point:** `database/adapter.*` is the ONLY database interface
- **DTO-based I/O:** Adapter accepts and returns immutable DTOs — no raw rows, no dicts
- **Engine-agnostic:** Switching database engines requires changes only in `database/`
- **Portable SQL:** All queries use portable syntax compatible with all supported engines
- **Modules are DB-free:** No module under `app/modules/` may import database drivers or contain SQL

---

## 2. Adapter Interface

```python
from contracts.pipeline import PipelineRunDTO
from contracts.entity import EntityDTO


class DatabaseAdapter:
    """Single entry point for all database operations."""

    def initialize(self, config: dict) -> None:
        """Connect to database and run pending migrations."""
        ...

    def close(self) -> None:
        """Close database connection."""
        ...

    # ── Pipeline Runs ─────────────────────────────────────────────

    def create_run(self, run: PipelineRunDTO) -> None:
        """Insert a new pipeline run. Idempotent (ON CONFLICT DO NOTHING)."""
        ...

    def get_run(self, run_id: str) -> PipelineRunDTO | None:
        """Retrieve a pipeline run by ID."""
        ...

    def get_run_by_entity(self, entity_id: str) -> PipelineRunDTO | None:
        """Retrieve the latest run for an entity."""
        ...

    def update_run_stage(self, run_id: str, stage: str) -> None:
        """Checkpoint: update last_completed_stage."""
        ...

    def update_run_status(self, run_id: str, status: str) -> None:
        """Update pipeline run status."""
        ...

    # ── Entities ──────────────────────────────────────────────────

    def insert_entity(self, entity: EntityDTO) -> None:
        """Insert an entity. Idempotent (ON CONFLICT DO NOTHING)."""
        ...

    def get_entity(self, entity_id: str) -> EntityDTO | None:
        """Retrieve an entity by ID."""
        ...

    def update_entity_status(self, entity_id: str, status: str) -> None:
        """Update entity status."""
        ...

    def get_entities_by_status(self, status: str) -> list[EntityDTO]:
        """Retrieve all entities with the given status."""
        ...

    # ── Add project-specific methods below ────────────────────────
```

---

## 3. SQL Compatibility Rules

All SQL in the adapter MUST use portable syntax compatible with all supported engines:

### Allowed

| Pattern                  | Example                                                 |
| ------------------------ | ------------------------------------------------------- |
| `ON CONFLICT DO NOTHING` | `INSERT INTO t (id) VALUES (?) ON CONFLICT DO NOTHING`  |
| `ON CONFLICT DO UPDATE`  | `... ON CONFLICT (id) DO UPDATE SET col = ?`            |
| Parameterized queries    | `cursor.execute("SELECT * FROM t WHERE id = ?", (id,))` |
| `CURRENT_TIMESTAMP`      | `DEFAULT CURRENT_TIMESTAMP`                             |
| Standard types           | `TEXT`, `INTEGER`, `REAL`, `BLOB`                       |

### Forbidden

| Pattern                     | Why                                      |
| --------------------------- | ---------------------------------------- |
| `INSERT OR IGNORE`          | Engine-specific syntax — not portable    |
| `INSERT OR REPLACE`         | Engine-specific syntax — not portable    |
| String interpolation in SQL | SQL injection risk                       |
| `AUTOINCREMENT`             | Use content-addressable TEXT PKs instead |
| Engine-specific functions   | Not portable across engines              |
| Engine-specific date/time   | Use `CURRENT_TIMESTAMP` instead          |

---

## 4. Engine Implementations

### Directory Structure

```
database/
├── adapter.py                    # Public interface (engine-agnostic)
├── engines/
│   ├── __init__.py
│   └── <engine>_engine.py        # One implementation per supported engine
└── migrations/
    ├── 20240101000001_initial_schema.sql
    └── ...
```

### Engine Selection

```yaml
# config/pipeline.yaml
database:
  engine: <engine_name> # Project-specific — choose the appropriate engine
  # Engine-specific connection settings go here
  # Example:
  #   path: "data/pipeline.db"    # File-based engines
  #   host: "localhost"            # Server-based engines
  #   port: 5432                   # Server-based engines
```

The adapter reads `database.engine` from config and instantiates the appropriate engine.
Engine-specific settings (connection modes, pooling, etc.) belong in `database/engines/` only.

---

## 5. Migration Strategy

### Naming Convention

```
YYYYMMDD000NNN_description.sql
```

Examples:

- `20240101000001_initial_schema.sql`
- `20240115000002_add_items_table.sql`

### Rules

1. Migrations are **append-only** — never modify existing migration files
2. All schema changes go through migrations — no ad-hoc `ALTER TABLE`
3. Migrations run automatically on `adapter.initialize()`
4. Migration state tracked in a `_migrations` table
5. All SQL in migrations uses portable syntax

### Migration Runner

```python
def run_migrations(self) -> None:
    """Run all pending migrations in order."""
    applied = self._get_applied_migrations()
    pending = [m for m in self._discover_migrations() if m not in applied]
    for migration in sorted(pending):
        self._execute_migration(migration)
        self._record_migration(migration)
```

---

## 6. Transaction Model

- **Write operations** are wrapped in transactions
- **Checkpoint writes** are single-statement transactions (atomic)
- **Batch inserts** use a single transaction for all items
- **Read operations** use consistent snapshots (engine-appropriate isolation level)

```python
# Checkpoint write (atomic)
def update_run_stage(self, run_id: str, stage: str) -> None:
    with self._transaction():
        self._execute(
            "UPDATE pipeline_runs SET last_completed_stage = ?, updated_at = CURRENT_TIMESTAMP WHERE run_id = ?",
            (stage, run_id)
        )
```

---

## 7. Connection Management

Engine-specific connection management is implemented in `database/engines/`.
Each engine module handles its own:

- Connection lifecycle (created on `initialize()`, closed on `close()`)
- Concurrency model (single connection, connection pooling, etc.)
- Engine-specific optimizations (journal modes, foreign key enforcement, etc.)
- Transaction isolation levels

The adapter delegates all engine-specific behavior to the active engine module.
See `docs/db_adapter_spec.md` § Engine Implementations for the directory structure.

---

## 8. Idempotency Enforcement

The adapter enforces idempotency at the database level:

1. **All INSERTs** use `ON CONFLICT DO NOTHING` or `ON CONFLICT DO UPDATE`
2. **Primary keys** are content-addressable (SHA-256 derived)
3. **Duplicate inserts** are silently ignored (no errors)
4. **Status updates** use conditional logic to prevent invalid transitions

```sql
-- Idempotent insert
INSERT INTO entities (entity_id, name, status)
VALUES (?, ?, ?)
ON CONFLICT (entity_id) DO NOTHING;

-- Conditional status update (prevents backward transitions)
UPDATE entities
SET status = ?, updated_at = CURRENT_TIMESTAMP
WHERE entity_id = ? AND status != 'completed';
```
