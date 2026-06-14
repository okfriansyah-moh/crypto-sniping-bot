-- Phase 0 initial schema: event bus, consumer offsets, strategy versions, pipeline runs, migration log.
-- All tables use portable SQL syntax (ON CONFLICT DO NOTHING, CURRENT_TIMESTAMP).
-- See docs/reference/db_adapter_spec.md § 6.1, § 6.8.

BEGIN;

-- ── Migration tracking ────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS _migrations (
    migration_id   TEXT        PRIMARY KEY,    -- filename without extension
    applied_at     TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- ── Event Bus (append-only) ───────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS events (
    event_id       TEXT        PRIMARY KEY,                   -- SHA256(payload_signature)[:16]
    event_type     TEXT        NOT NULL,
    payload        JSONB       NOT NULL,
    trace_id       TEXT        NOT NULL,
    correlation_id TEXT        NOT NULL,
    causation_id   TEXT,                                      -- NULL only for Layer 0 root events
    version_id     TEXT        NOT NULL,
    created_at     TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    processed      BOOLEAN     NOT NULL DEFAULT FALSE,
    CONSTRAINT fk_causation FOREIGN KEY (causation_id) REFERENCES events(event_id)
);

CREATE INDEX IF NOT EXISTS idx_events_unprocessed
    ON events (processed, created_at) WHERE processed = FALSE;
CREATE INDEX IF NOT EXISTS idx_events_trace
    ON events (trace_id);
CREATE INDEX IF NOT EXISTS idx_events_correlation
    ON events (correlation_id);
CREATE INDEX IF NOT EXISTS idx_events_causation
    ON events (causation_id);
CREATE INDEX IF NOT EXISTS idx_events_type
    ON events (event_type, processed, created_at);

-- ── Consumer Offsets (per-group progress tracking) ────────────────────────────
CREATE TABLE IF NOT EXISTS consumer_offsets (
    consumer_group TEXT        PRIMARY KEY,
    last_event_id  TEXT        NOT NULL,
    updated_at     TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- ── Strategy Versions (immutable config snapshots) ────────────────────────────
CREATE TABLE IF NOT EXISTS strategy_versions (
    strategy_version_id TEXT        PRIMARY KEY,  -- SHA256(config_snapshot)[:16]
    config_snapshot     JSONB       NOT NULL,
    created_at          TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    activated_at        TIMESTAMP,
    deactivated_at      TIMESTAMP
);

-- ── Pipeline Runs (per-market execution tracking) ────────────────────────────
CREATE TABLE IF NOT EXISTS pipeline_runs (
    run_id               TEXT        PRIMARY KEY,
    trace_id             TEXT        NOT NULL,
    status               TEXT        NOT NULL DEFAULT 'started',  -- started|processing|completed|partial|failed
    last_completed_stage TEXT,
    strategy_version_id  TEXT        NOT NULL REFERENCES strategy_versions(strategy_version_id),
    created_at           TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_runs_status
    ON pipeline_runs (status, created_at);

COMMIT;
