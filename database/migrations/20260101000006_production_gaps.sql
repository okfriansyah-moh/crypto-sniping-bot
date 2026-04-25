-- Migration: 20260101000006_production_gaps.sql
-- Adds §8 production gap extensions:
-- 1. priority and expires_at columns on events
-- 2. system_state singleton table
-- 3. §8.7 strategy version lifecycle columns
-- 4. §8.2 execution result additive columns
-- 5. §8.3 allocation additive columns
-- 6. §8.8 learning record additive columns
-- 7. events_archive table for event archival
-- All changes are additive/backward-compatible.

-- ── 1. Event TTL and priority ──────────────────────────────────────────────────

ALTER TABLE events
    ADD COLUMN IF NOT EXISTS priority   INTEGER   NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMP WITH TIME ZONE;

-- Index for ClaimNextEvent ordering: priority DESC, created_at ASC, with TTL filter.
CREATE INDEX IF NOT EXISTS idx_events_claim_order
    ON events (event_type, processed, priority DESC, created_at ASC)
    WHERE processed = FALSE;

-- ── 2. System state singleton ─────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS system_state (
    id                       INTEGER     NOT NULL DEFAULT 1 CHECK (id = 1),
    mode                     TEXT        NOT NULL DEFAULT 'BALANCED',
    drawdown_pct             NUMERIC     NOT NULL DEFAULT 0.0,
    drawdown_window_hours    INTEGER     NOT NULL DEFAULT 24,
    open_positions           INTEGER     NOT NULL DEFAULT 0,
    total_exposure_usd       NUMERIC     NOT NULL DEFAULT 0.0,
    active_strategy_id       TEXT        NOT NULL DEFAULT '',
    shadow_strategy_id       TEXT        NOT NULL DEFAULT '',
    last_transition_reason   TEXT        NOT NULL DEFAULT '',
    updated_at               TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    version_id               TEXT        NOT NULL DEFAULT '',
    state_version            BIGINT      NOT NULL DEFAULT 0,
    PRIMARY KEY (id)
);

INSERT INTO system_state (id) VALUES (1) ON CONFLICT DO NOTHING;

-- ── 3. Strategy version lifecycle columns (§8.7) ──────────────────────────────

ALTER TABLE strategy_versions
    ADD COLUMN IF NOT EXISTS status            TEXT    NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS shadow_started_at TIMESTAMP WITH TIME ZONE,
    ADD COLUMN IF NOT EXISTS promoted_at       TIMESTAMP WITH TIME ZONE,
    ADD COLUMN IF NOT EXISTS rolled_back_at    TIMESTAMP WITH TIME ZONE,
    ADD COLUMN IF NOT EXISTS parent_version_id TEXT    NOT NULL DEFAULT '';

-- Partial unique index: only one active version allowed at a time.
CREATE UNIQUE INDEX IF NOT EXISTS idx_strategy_versions_one_active
    ON strategy_versions (status)
    WHERE status = 'active';

-- ── 4. Execution result additive columns (§8.2) ───────────────────────────────

ALTER TABLE execution_results
    ADD COLUMN IF NOT EXISTS mev_protected       BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS execution_path      TEXT    NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS slippage_guard_bps  INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS rejection_reason    TEXT    NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS simulated           BOOLEAN NOT NULL DEFAULT FALSE;

-- ── 5. Allocation additive columns (§8.3) ─────────────────────────────────────

ALTER TABLE allocations
    ADD COLUMN IF NOT EXISTS rejected      BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS reject_reason TEXT    NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS cohort_id     TEXT    NOT NULL DEFAULT '';

-- ── 6. Learning record additive columns (§8.8) ────────────────────────────────

ALTER TABLE learning_records
    ADD COLUMN IF NOT EXISTS simulated       BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS expired_source  BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS strategy_status TEXT    NOT NULL DEFAULT '';

-- ── 7. Events archive table ───────────────────────────────────────────────────
-- Explicit column list used instead of LIKE events INCLUDING ALL to ensure
-- portability across database engines and to avoid copying FK constraints
-- (causation_id FK would reference the live events table, not valid for archive).

CREATE TABLE IF NOT EXISTS events_archive (
    event_id       TEXT        PRIMARY KEY,
    event_type     TEXT        NOT NULL,
    payload        JSONB       NOT NULL,
    trace_id       TEXT        NOT NULL,
    correlation_id TEXT        NOT NULL,
    causation_id   TEXT,
    version_id     TEXT        NOT NULL,
    created_at     TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    processed      BOOLEAN     NOT NULL DEFAULT FALSE,
    claimed_at     TIMESTAMP,
    priority       INTEGER     NOT NULL DEFAULT 0,
    expires_at     TIMESTAMP WITH TIME ZONE
);
