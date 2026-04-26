-- Phase 6: Event bus partitioning, events_archive table, system_state table,
-- and exposure aggregates.
-- Migration: 20260101000011_phase6_hardening.sql
-- Append-only — never modify this file after it is committed.

BEGIN;

-- ── System State ────────────────────────────────────────────────────────────
-- Singleton row tracking the current system operational mode.
-- Managed by the risk controller worker; never written by pipeline modules.

CREATE TABLE IF NOT EXISTS system_state (
    id              INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),   -- singleton
    mode            VARCHAR(32) NOT NULL DEFAULT 'BALANCED',    -- BALANCED|STRICT|EXPLORATION|DEGRADED|HALTED
    drawdown_pct    DECIMAL(10,6) NOT NULL DEFAULT 0,
    drawdown_window_hours INT NOT NULL DEFAULT 24,
    open_positions  INT NOT NULL DEFAULT 0,
    total_exposure_usd DECIMAL(18,6) NOT NULL DEFAULT 0,
    active_strategy_id VARCHAR(32) NOT NULL DEFAULT '',
    shadow_strategy_id VARCHAR(32) NOT NULL DEFAULT '',
    last_transition_reason VARCHAR(256) NOT NULL DEFAULT '',
    updated_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    version_id      VARCHAR(32) NOT NULL DEFAULT '',
    state_version   BIGINT NOT NULL DEFAULT 0
);

-- Seed the singleton row if it does not exist.
INSERT INTO system_state (id) VALUES (1) ON CONFLICT DO NOTHING;

-- ── Events Archive ───────────────────────────────────────────────────────────
-- Hot/warm/cold data tiering:
--   events         — hot (last 7 days) + warm (processed, last 30 days)
--   events_archive — cold (older than warm_days, processed=TRUE only)
-- The archive worker uses adapter.ArchiveEvents to move rows here.
-- Detach this table to dump cold data without disrupting the live system.

CREATE TABLE IF NOT EXISTS events_archive (
    event_id        VARCHAR(32) PRIMARY KEY,
    event_type      VARCHAR(64) NOT NULL,
    payload         JSONB       NOT NULL DEFAULT '{}',
    trace_id        VARCHAR(32) NOT NULL DEFAULT '',
    correlation_id  VARCHAR(32) NOT NULL DEFAULT '',
    causation_id    VARCHAR(32),
    version_id      VARCHAR(32) NOT NULL DEFAULT '',
    created_at      TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    processed       BOOLEAN     NOT NULL DEFAULT FALSE,
    claimed_at      TIMESTAMP,
    priority        INT         NOT NULL DEFAULT 0,
    expires_at      TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_events_archive_trace
    ON events_archive (trace_id, created_at ASC);

CREATE INDEX IF NOT EXISTS idx_events_archive_created
    ON events_archive (created_at ASC);

-- ── Exposure Aggregates ──────────────────────────────────────────────────────
-- Maintained by the capital worker on every INSERT/UPDATE to positions.
-- These tables are updated via triggers or explicit adapter calls — never raw SQL in modules.

CREATE TABLE IF NOT EXISTS exposure_aggregates (
    chain           VARCHAR(32)  NOT NULL,
    token_address   VARCHAR(64)  NOT NULL,
    cohort_id       VARCHAR(64)  NOT NULL DEFAULT '',
    total_size_usd  DECIMAL(18,6) NOT NULL DEFAULT 0,
    open_count      INT          NOT NULL DEFAULT 0,
    updated_at      TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (chain, token_address, cohort_id)
);

-- ── Wallet Gas Tracking ──────────────────────────────────────────────────────
-- Daily gas spend per wallet address.  Reset daily by the archive/budget worker.

CREATE TABLE IF NOT EXISTS wallet_gas_daily (
    wallet_address  VARCHAR(64)  NOT NULL,
    date_utc        DATE         NOT NULL,   -- current UTC date at insert time
    spent_gwei      BIGINT       NOT NULL DEFAULT 0,
    PRIMARY KEY (wallet_address, date_utc)
);

COMMIT;
