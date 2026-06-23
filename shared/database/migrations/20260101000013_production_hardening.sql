-- Migration: 20260101000013_production_hardening.sql
-- Phase 8 — Final Production Hardening
-- Enforces determinism + exactly-once + failure-safety (architecture § 4.10-4.11).
-- All changes are additive: ADD COLUMN with safe defaults, CREATE TABLE IF NOT EXISTS.
-- No DROP, no ALTER COLUMN TYPE. Safe for rolling deployment.

BEGIN;

-- ── § 4.10.A  Event ordering columns ─────────────────────────────────────────

-- chain and consumer must be added first — idx_events_dispatch references them.
ALTER TABLE events
    ADD COLUMN IF NOT EXISTS chain              TEXT    NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS consumer           TEXT    NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS logical_order_key  BYTEA   NOT NULL DEFAULT '\x00'::bytea,
    ADD COLUMN IF NOT EXISTS partition_key      INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS retry_count        INTEGER NOT NULL DEFAULT 0;

-- Dispatch index: consumer + partition + ordering in one pass.
CREATE INDEX IF NOT EXISTS idx_events_dispatch
    ON events (chain, consumer, processed, partition_key, logical_order_key)
    WHERE processed = FALSE;

-- Reorg invalidation marker (§ 4.11.D).
ALTER TABLE events
    ADD COLUMN IF NOT EXISTS invalidated_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_events_invalidated
    ON events (invalidated_at)
    WHERE invalidated_at IS NOT NULL;

-- ── § 4.10.C  Dead-letter queue ───────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS dead_letter_events (
    event_id          TEXT        PRIMARY KEY,
    chain             TEXT        NOT NULL DEFAULT '',
    consumer          TEXT        NOT NULL DEFAULT '',
    reason            TEXT        NOT NULL,
    error_message     TEXT,
    retry_count       INTEGER     NOT NULL DEFAULT 0,
    first_failed_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_failed_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    moved_to_dlq_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    payload_snapshot  JSONB,
    trace_id          TEXT        NOT NULL DEFAULT '',
    correlation_id    TEXT        NOT NULL DEFAULT '',
    causation_id      TEXT,
    version_id        TEXT        NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_dlq_consumer_reason
    ON dead_letter_events (consumer, reason, moved_to_dlq_at);

-- ── § 4.10.E  Position consistency ───────────────────────────────────────────

-- source_execution_id enforces the single-position-per-execution invariant.
ALTER TABLE positions
    ADD COLUMN IF NOT EXISTS source_execution_id TEXT,
    ADD COLUMN IF NOT EXISTS entry_execution_id  TEXT;

-- Unique constraint: at most one position row with a given source_execution_id.
-- Use a partial unique index (allows multiple NULL values).
CREATE UNIQUE INDEX IF NOT EXISTS idx_positions_source_execution_id
    ON positions (source_execution_id)
    WHERE source_execution_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_positions_open_for_reconciliation
    ON positions (status)
    WHERE status = 'open';

-- ── § 4.10.F  Latency feedback loop ─────────────────────────────────────────

CREATE TABLE IF NOT EXISTS latency_events (
    id                          BIGSERIAL   PRIMARY KEY,
    execution_id                TEXT        NOT NULL,
    chain                       TEXT        NOT NULL,
    endpoint                    TEXT        NOT NULL,
    version_id                  TEXT        NOT NULL DEFAULT '',
    op_kind                     TEXT        NOT NULL DEFAULT 'execute',
    decision_to_send_ms         INTEGER     NOT NULL DEFAULT 0,
    send_to_first_observe_ms    INTEGER     NOT NULL DEFAULT 0,
    first_observe_to_confirm_ms INTEGER     NOT NULL DEFAULT 0,
    total_ms                    INTEGER     NOT NULL DEFAULT 0,
    outcome                     TEXT        NOT NULL DEFAULT 'confirmed',
    observed_at                 TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_latency_window
    ON latency_events (chain, endpoint, observed_at);

-- ── § 4.10.H  Circuit breaker / global kill switch ───────────────────────────
-- Stored as a dedicated table to avoid conflicting with the system_state
-- operational-mode singleton (id=1) created in 20260101000006_production_gaps.sql.

CREATE TABLE IF NOT EXISTS system_halt (
    id         INTEGER     PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    halted     BOOLEAN     NOT NULL DEFAULT FALSE,
    reason     TEXT        NOT NULL DEFAULT '',
    operator   TEXT        NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO system_halt (id) VALUES (1) ON CONFLICT DO NOTHING;

-- ── § 4.10.E.2  Reconciliation log ──────────────────────────────────────────

CREATE TABLE IF NOT EXISTS reconciliation_events (
    id             BIGSERIAL   PRIMARY KEY,
    position_id    TEXT        NOT NULL,
    db_amount      NUMERIC(78, 0) NOT NULL DEFAULT 0,
    onchain_amount NUMERIC(78, 0) NOT NULL DEFAULT 0,
    action         TEXT        NOT NULL DEFAULT 'noop',
    reason         TEXT        NOT NULL DEFAULT '',
    observed_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- ── § 4.11.B  Partition leases ───────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS partition_leases (
    chain         TEXT        NOT NULL,
    consumer      TEXT        NOT NULL,
    partition_key INTEGER     NOT NULL,
    worker_id     TEXT        NOT NULL,
    leased_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at    TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (chain, consumer, partition_key)
);

CREATE INDEX IF NOT EXISTS idx_partition_leases_worker
    ON partition_leases (chain, consumer, worker_id);

-- ── § 4.11.C  Crash-safe recovery: execution attempts journal ────────────────

CREATE TABLE IF NOT EXISTS execution_attempts (
    id             BIGSERIAL PRIMARY KEY,
    execution_id   TEXT      NOT NULL,
    attempt_number INTEGER   NOT NULL DEFAULT 1,
    tx_hash        TEXT,
    status         TEXT      NOT NULL DEFAULT 'reserved',
    nonce          BIGINT,
    gas_price_wei  NUMERIC(78, 0),
    sent_at        TIMESTAMPTZ,
    observed_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (execution_id, attempt_number)
);

CREATE INDEX IF NOT EXISTS idx_attempts_in_flight
    ON execution_attempts (status)
    WHERE status IN ('reserved', 'sent');

-- ── § 4.11.D  Reorg handling ─────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS reorg_events (
    chain          TEXT        NOT NULL,
    old_block      BIGINT      NOT NULL,
    new_block      BIGINT      NOT NULL,
    detected_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    depth          INTEGER     NOT NULL DEFAULT 0,
    affected_count INTEGER     NOT NULL DEFAULT 0,
    PRIMARY KEY (chain, old_block, detected_at)
);

-- confirmation_status extends the execution lifecycle for reorg support.
ALTER TABLE execution_results
    ADD COLUMN IF NOT EXISTS confirmation_status TEXT NOT NULL DEFAULT 'confirmed';
-- Values: confirmed | reorg_pending | reorged_out | reorg_mutation

-- ── § 4.11.E  Evaluation invariant ──────────────────────────────────────────

CREATE TABLE IF NOT EXISTS evaluation_invariant (
    execution_id   TEXT        PRIMARY KEY,
    has_evaluation BOOLEAN     NOT NULL DEFAULT FALSE,
    deadline_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_eval_missing
    ON evaluation_invariant (deadline_at)
    WHERE has_evaluation = FALSE;

-- ── § 4.11.F  Backpressure / ingestion drops ─────────────────────────────────

CREATE TABLE IF NOT EXISTS ingestion_drops (
    id            BIGSERIAL   PRIMARY KEY,
    chain         TEXT        NOT NULL,
    reason        TEXT        NOT NULL,
    token_address TEXT,
    score         NUMERIC(10, 4),
    dropped_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_drops_window
    ON ingestion_drops (chain, dropped_at);

COMMIT;
