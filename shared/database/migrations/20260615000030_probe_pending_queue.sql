-- probe_pending_queue: tokens deferred when Helius probe budget is exhausted.
-- pending_id = SHA256(source_event_id || '|' || due_hour_unix)[:16] — idempotent enqueue.
-- Drain order: status='pending' AND due_at <= NOW() ORDER BY priority, enqueued_at.

CREATE TABLE IF NOT EXISTS probe_pending_queue (
    pending_id       TEXT        PRIMARY KEY,
    source_event_id  TEXT        NOT NULL,
    token_address    TEXT        NOT NULL,
    chain            TEXT        NOT NULL,
    market           TEXT        NOT NULL DEFAULT '',
    priority         INT         NOT NULL DEFAULT 0,
    payload          JSONB       NOT NULL,
    enqueued_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    due_at           TIMESTAMPTZ NOT NULL,
    status           TEXT        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'claimed', 'completed', 'expired')),
    attempt_count    INT         NOT NULL DEFAULT 0,
    last_error       TEXT        NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_probe_pending_drain
    ON probe_pending_queue (status, due_at, priority, enqueued_at)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_probe_pending_token
    ON probe_pending_queue (chain, token_address, status);
