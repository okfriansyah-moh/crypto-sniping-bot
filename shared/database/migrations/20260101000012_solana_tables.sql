-- Phase 7: Solana Market Extension
-- Adds Solana-specific tables for RPC endpoint circuit breaker state,
-- execution idempotency/confirmation tracking, ingestion watermark,
-- and endpoint health scoring.
--
-- Naming: solana_* prefix to avoid collision with EVM tables.
-- All SQL uses portable syntax (ON CONFLICT DO NOTHING, CURRENT_TIMESTAMP).
-- Never modify this file once committed — add a new migration instead.

BEGIN;

-- solana_rpc_endpoint_state: per-endpoint circuit breaker state.
-- State machine: closed (normal) → open (failing) → half_open (probing).
CREATE TABLE IF NOT EXISTS solana_rpc_endpoint_state (
    endpoint_url          TEXT        NOT NULL PRIMARY KEY,
    state                 TEXT        NOT NULL DEFAULT 'closed', -- closed | open | half_open
    consecutive_failures  INTEGER     NOT NULL DEFAULT 0,
    last_failure_at       TIMESTAMP,
    circuit_opened_at     TIMESTAMP,
    updated_at            TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- solana_signatures: execution idempotency and confirmation tracking.
-- One row per submitted transaction. execution_id ties back to AllocationDTO.
CREATE TABLE IF NOT EXISTS solana_signatures (
    execution_id          TEXT        NOT NULL PRIMARY KEY,
    signature             TEXT        NOT NULL,
    status                TEXT        NOT NULL DEFAULT 'pending', -- pending | confirmed | failed | expired
    slot                  BIGINT,
    err_msg               TEXT,
    created_at            TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at            TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_solana_signatures_signature ON solana_signatures(signature);
CREATE INDEX IF NOT EXISTS idx_solana_signatures_status    ON solana_signatures(status);

-- solana_ingestion_watermark: monotonically increasing slot watermark per program.
-- Reuses the same pattern as the EVM ingestion_watermarks table.
-- market = "solana-raydium-v4" | "solana-pumpfun" | etc.
CREATE TABLE IF NOT EXISTS solana_ingestion_watermark (
    market                TEXT        NOT NULL PRIMARY KEY,
    last_slot             BIGINT      NOT NULL DEFAULT 0,
    updated_at            TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- solana_endpoint_health: rolling health metrics per RPC endpoint.
-- Used by the endpoint scorer for failover decisions.
CREATE TABLE IF NOT EXISTS solana_endpoint_health (
    endpoint_url          TEXT        NOT NULL PRIMARY KEY,
    p95_latency_ms        INTEGER     NOT NULL DEFAULT 0,
    error_rate            REAL        NOT NULL DEFAULT 0,
    success_count         BIGINT      NOT NULL DEFAULT 0,
    failure_count         BIGINT      NOT NULL DEFAULT 0,
    updated_at            TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP
);

COMMIT;
