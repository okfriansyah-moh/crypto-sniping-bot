---
name: migration-management
type: skill
description: "Database migration best practices. Use when creating, reviewing, or managing database schema migrations. Ensures portable, reversible, safe migrations."
---

## Purpose

Enforce safe database migration practices. Ensure all schema changes are versioned, reversible, portable across database engines, and applied through the proper migration system.

---

## Rules

### Migration Files

1. **Naming convention** — `YYYYMMDD000NNN_description.sql` (e.g., `20240115000001_create_users.sql`)
2. **Append-only** — never modify existing migration files after they're committed
3. **Each migration is atomic** — one logical schema change per file
4. **Up and down** — every migration must have a reversible down migration
5. **Migrations live in `database/migrations/`** — no ad-hoc ALTER TABLE elsewhere

### SQL Portability

| Portable (Use)            | Engine-Specific (Avoid)                |
| ------------------------- | -------------------------------------- |
| `TEXT`                    | `VARCHAR(n)` for variable text         |
| `INTEGER`                 | `SERIAL`, `BIGSERIAL`                  |
| `REAL`                    | `DOUBLE PRECISION`                     |
| `CURRENT_TIMESTAMP`       | `NOW()`, `strftime()`                  |
| `ON CONFLICT DO NOTHING`  | `INSERT OR IGNORE`, `ON DUPLICATE KEY` |
| Parameterized `?` or `$1` | String interpolation                   |

### Safety Rules

1. **No DROP TABLE in production migrations** — use soft deletes or archive patterns
2. **No ALTER COLUMN that loses data** — always create new column, migrate data, then drop old
3. **Add columns as nullable first** — then backfill, then add NOT NULL constraint
4. **Indexes on large tables** — create concurrently when possible
5. **Test migrations on a copy** — never run untested migrations on production

### Migration Workflow

```
1. Create migration file: database/migrations/YYYYMMDD000NNN_description.sql
2. Write UP migration (create table, add column, etc.)
3. Write DOWN migration (reverse the UP — drop table, remove column)
4. Test: apply UP, verify schema, apply DOWN, verify clean state
5. Commit migration file
6. Apply in CI/staging before production
```

---

## Checklist

```
[ ] Migration file follows naming convention
[ ] UP and DOWN migrations both present
[ ] SQL uses portable syntax (no engine-specific functions)
[ ] No modification of existing migration files
[ ] New columns added as nullable or with defaults
[ ] No data-destructive operations without review
[ ] Parameterized queries only (no string interpolation)
[ ] Migration tested on clean database
[ ] Index creation doesn't lock tables for extended periods
[ ] Migration is idempotent (safe to re-run)
```

---

## Anti-Patterns

| Pattern                               | Problem                  | Fix                                 |
| ------------------------------------- | ------------------------ | ----------------------------------- |
| `ALTER TABLE users DROP COLUMN email` | Data loss                | Archive data first, use soft delete |
| Modifying committed migration         | State inconsistency      | Create new migration                |
| `INSERT OR IGNORE`                    | Engine-specific          | Use `ON CONFLICT DO NOTHING`        |
| Ad-hoc SQL in application code        | Untracked schema changes | Always use migration files          |
| Migration with no down path           | Can't rollback           | Always provide DOWN migration       |
