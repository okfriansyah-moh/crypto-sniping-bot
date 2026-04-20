---
name: idempotency
description: "Idempotency enforcement. Use when implementing database writes, file operations, or pipeline resume logic. Ensures running the pipeline twice on the same input produces no duplicates and no corruption."
---

# Idempotency Skill

## Purpose

Ensure the pipeline can be safely re-run on the same input without creating duplicates or corrupting data. Covers database writes, file operations, and pipeline resume logic.

> **All database operations MUST go through `database/adapter.*`.** Modules under `app/modules/` never touch the database. See `docs/db_adapter_spec.md`.

## Rules

### Core Invariant

> Running the pipeline twice on the same input produces no duplicates, no corruption, and completes quickly on the second run (cache check only).

### Database Patterns

- All INSERTs use `ON CONFLICT DO NOTHING` or `ON CONFLICT DO UPDATE`
- All SQL uses parameterized queries (no string interpolation)
- All SQL uses portable syntax per `docs/db_adapter_spec.md`
- All database access goes through `database/adapter.*`

### File Write Patterns

- Use atomic write-then-rename pattern
- Skip-if-exists for already-generated files

### Resume Rules

- Pipeline-level: query existing run, skip if completed, resume from next stage
- Entity-level: skip already-processed items via content-addressable IDs

## Inputs

- Database operations in `database/adapter.*`
- File write operations in modules
- Pipeline resume logic in `app/orchestrator/`

## Outputs

- Idempotent database operations with no duplicate writes
- Safe file operations with atomic writes

## Examples

### ID Computation (Content-Addressable)

```python
import hashlib

def compute_entity_id(content_signature: str) -> str:
    """Content-addressable ID: SHA-256 → first 16 hex chars."""
    return hashlib.sha256(content_signature.encode()).hexdigest()[:16]
```

### Database — ON CONFLICT DO NOTHING (Portable)

```sql
-- ✅ CORRECT — idempotent insert, portable
INSERT INTO entities (entity_id, name, status)
VALUES (?, ?, ?)
ON CONFLICT (entity_id) DO NOTHING;

-- ❌ FORBIDDEN — engine-specific syntax
INSERT OR IGNORE INTO entities (entity_id, name, status)
VALUES (?, ?, ?);
```

### Atomic File Write

```python
# ✅ CORRECT — atomic, crash-safe
tmp_path = f"{output_path}.tmp"
with open(tmp_path, 'wb') as f:
    f.write(data)
os.rename(tmp_path, output_path)  # Atomic on same filesystem
```

### Pipeline Resume

```python
run = adapter.get_run(entity_id)
if run and run.status == 'completed':
    return  # Idempotent: no work on re-run
if run:
    resume_stage = STAGE_ORDER.index(run.last_completed_stage) + 1
else:
    resume_stage = 0  # Fresh run
```

## Checklist

- [ ] All INSERTs use `ON CONFLICT DO NOTHING` or `ON CONFLICT DO UPDATE`
- [ ] All SQL uses parameterized queries (no string interpolation)
- [ ] All SQL uses portable syntax per `docs/db_adapter_spec.md`
- [ ] All database access goes through `database/adapter.*`
- [ ] All file writes use atomic write-then-rename pattern
- [ ] Pipeline checks for existing completed run before starting
- [ ] Per-entity processing skips already-completed items
- [ ] IDs are content-addressable (SHA-256 based)
- [ ] `os.makedirs(path, exist_ok=True)` for directory creation
