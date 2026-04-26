-- Phase 6: Exposure aggregates, wallet gas tracking, and events_archive indexes.
-- Migration: 20260101000011_phase6_hardening.sql
-- Append-only — never modify this file after it is committed.
--
-- NOTE: system_state and events_archive were already created in
-- 20260101000006_production_gaps.sql.  This migration adds only the tables
-- and indexes that are genuinely new in Phase 6.

BEGIN;

-- ── Events Archive — additional indexes ─────────────────────────────────────
-- events_archive table exists from 20260101000006_production_gaps.sql.
-- These indexes were not included in the original migration.

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
