---
name: database-portability
description: "Database engine portability. Use when writing SQL, implementing the database adapter, or reviewing database operations. Ensures all SQL is portable across supported engines and no engine-specific code leaks into modules."
---

# Database Portability Skill

## Purpose

Ensure all database operations are engine-agnostic. The database adapter is the sole abstraction boundary — switching engines requires changes only in `database/`. No module may reference any specific database engine.

## Rules

### Architecture

- **All database access goes through `database/adapter.*`** — the single entry point
- Modules under `app/modules/` **MUST NOT** import any database driver
- Modules **MUST NOT** contain SQL strings or execute queries
- The adapter accepts and returns immutable DTOs — no raw rows, no dicts
- Only the orchestrator calls the adapter — modules never touch the database

### SQL Portability

| Use This (Portable)          | Not This (Engine-Specific)  |
| ---------------------------- | --------------------------- |
| `ON CONFLICT DO NOTHING`     | `INSERT OR IGNORE`          |
| `CURRENT_TIMESTAMP`          | `strftime(...)` in defaults |
| `TEXT`, `INTEGER`, `REAL`    | `VARCHAR(n)`, `SERIAL`      |
| Parameterized queries `(?)`  | String interpolation        |
| `CREATE TABLE IF NOT EXISTS` | Engine-specific DDL         |

### Forbidden in Modules

```python
# ❌ FORBIDDEN in app/modules/
import sqlite3
import psycopg2
import asyncpg
from database.adapter import DatabaseAdapter
from database import anything
```

### Engine Configuration

Engine selection is driven by configuration, not code:

```yaml
# config/pipeline.yaml
database:
  engine: sqlite # or: postgres
  path: "data/pipeline.db" # SQLite
  # host: "localhost"       # PostgreSQL
  # port: 5432              # PostgreSQL
```

## Inputs

- SQL in `database/` directory
- Database-related code in `database/adapter.*` and `database/engines/`
- Module code that might incorrectly access the database

## Outputs

- Portable SQL that works across supported engines
- Clean adapter interface with no engine-specific leaks

## Examples

### Portable Timestamp Defaults

```sql
-- ✅ PORTABLE
created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP

-- ❌ ENGINE-SPECIFIC (SQLite only)
created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
```

### Portable Upsert

```sql
-- ✅ PORTABLE
INSERT INTO entities (entity_id, name, status)
VALUES (?, ?, ?)
ON CONFLICT (entity_id) DO NOTHING;

-- ❌ ENGINE-SPECIFIC
INSERT OR IGNORE INTO entities (entity_id, name, status)
VALUES (?, ?, ?);
```

### Adapter Pattern

```python
# database/adapter.py
class DatabaseAdapter:
    def __init__(self, engine: DatabaseEngine):
        self._engine = engine

    def create_run(self, run: PipelineRunDTO) -> None:
        self._engine.execute(
            "INSERT INTO pipeline_runs (run_id, input_path, status) "
            "VALUES (?, ?, ?) ON CONFLICT (run_id) DO NOTHING",
            (run.run_id, run.input_path, run.status)
        )
```

## Checklist

- [ ] No database driver imports in `app/modules/`
- [ ] No SQL strings in `app/modules/`
- [ ] All SQL uses `ON CONFLICT DO NOTHING` (not `INSERT OR IGNORE`)
- [ ] All SQL uses `CURRENT_TIMESTAMP` (not engine-specific functions)
- [ ] All SQL uses parameterized queries
- [ ] Adapter accepts/returns frozen DTOs only
- [ ] Engine selection driven by config, not hardcoded
- [ ] No engine-specific syntax in migration files
