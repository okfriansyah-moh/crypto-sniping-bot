-- Operator dashboard command audit — append-only mirror of operator_command_event payloads.
-- Authoritative log remains the events table; this table supports indexed operator queries.
-- Populated by backend-dashboard on command submit (Phase 5, Task 22+).
-- Inserts use ON CONFLICT (command_id) DO NOTHING for idempotency.
--
-- Migration: 20260613000001_operator_command_audit.sql

BEGIN;

CREATE TABLE IF NOT EXISTS operator_command_audit (
    command_id     TEXT        PRIMARY KEY,  -- content-addressable SHA256(payload)[:16]
    event_id       TEXT        NOT NULL UNIQUE,
    command_type   TEXT        NOT NULL,
    issuer_id      TEXT        NOT NULL DEFAULT '',
    args           JSONB       NOT NULL DEFAULT '{}',
    confirm_token  TEXT        NOT NULL DEFAULT '',
    payload        JSONB       NOT NULL,
    recorded_at    TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_operator_command_audit_type_time
    ON operator_command_audit (command_type, recorded_at DESC);

CREATE INDEX IF NOT EXISTS idx_operator_command_audit_issuer_time
    ON operator_command_audit (issuer_id, recorded_at DESC);

COMMIT;
