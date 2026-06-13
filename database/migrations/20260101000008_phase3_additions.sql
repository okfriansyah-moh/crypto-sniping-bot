-- Phase 3 additions: shadow_trades table (false-negative candidates),
-- quarantine_tokens table (quarantined token audit log), and
-- evaluation_details table for per-position evaluation data.
-- All SQL is portable: ON CONFLICT DO NOTHING, CURRENT_TIMESTAMP, parameterized queries.
-- See docs/reference/db_adapter_spec.md and docs/reference/implementation_roadmap.md § Phase 3.

BEGIN;

-- ── Quarantine tokens audit log ───────────────────────────────────────────────
-- Records tokens that have been quarantined after exceeding violation threshold.
CREATE TABLE IF NOT EXISTS quarantine_tokens (
    quarantine_id      TEXT        PRIMARY KEY,
    token_address      TEXT        NOT NULL,
    lifecycle_id       TEXT        NOT NULL DEFAULT '',
    reason             TEXT        NOT NULL DEFAULT '',
    violation_count    INTEGER     NOT NULL DEFAULT 0,
    quarantined_at     TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at         TIMESTAMP
);

-- Partial index covering only non-expired permanent quarantines.
-- CURRENT_TIMESTAMP is not IMMUTABLE so cannot be used in index predicates;
-- time-limited quarantine expiry is enforced at query time, not here.
CREATE UNIQUE INDEX IF NOT EXISTS idx_quarantine_token_address
    ON quarantine_tokens (token_address)
    WHERE expires_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_quarantine_lifecycle
    ON quarantine_tokens (lifecycle_id);

-- ── EVALUATED state: add to unique index exclusion ────────────────────────────
-- The existing idx_lifecycle_token partial unique index (in 20260101000003)
-- excludes REJECTED, POSITION_CLOSED, FAILED. We must also exclude EVALUATED
-- to allow new lifecycles for the same token after evaluation completes.
-- We cannot modify the existing index; drop and recreate with additive exclusion.
DROP INDEX IF EXISTS idx_lifecycle_token;

CREATE UNIQUE INDEX IF NOT EXISTS idx_lifecycle_token
    ON token_lifecycle (token_address)
    WHERE current_state NOT IN ('REJECTED', 'POSITION_CLOSED', 'FAILED', 'EVALUATED');

COMMIT;
