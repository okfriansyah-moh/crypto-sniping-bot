-- Migration: 20260101000008_learning_tables.sql
-- Phase 5: shadow_trades table for observation tracking.
-- learning_records and evaluations were added in 20260101000003_trading_tables.sql.

BEGIN;

-- ── Shadow Trades (observation window for rejected tokens) ────────────────────

CREATE TABLE IF NOT EXISTS shadow_trades (
    shadow_id              TEXT        PRIMARY KEY,
    token_address          TEXT        NOT NULL DEFAULT '',
    stage                  TEXT        NOT NULL DEFAULT '',    -- data_quality|edge|validated_edge|selection
    rejected_at            TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    observation_complete   BOOLEAN     NOT NULL DEFAULT FALSE,
    observed_return_pct    NUMERIC     NOT NULL DEFAULT 0.0,
    classification         TEXT        NOT NULL DEFAULT 'TN',  -- TN | FN
    learning_record_id     TEXT        NOT NULL DEFAULT '',    -- FK to learning_records.record_id
    version_id             TEXT        NOT NULL DEFAULT '',
    updated_at             TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_shadow_trades_pending
    ON shadow_trades (observation_complete, rejected_at ASC)
    WHERE observation_complete = FALSE;

CREATE INDEX IF NOT EXISTS idx_shadow_trades_version
    ON shadow_trades (version_id, rejected_at DESC);

COMMIT;
