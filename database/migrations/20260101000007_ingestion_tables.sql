-- Phase 1 ingestion tables: market_data projection, ingestion watermark, RPC endpoint health.
-- All SQL is portable: ON CONFLICT DO NOTHING, CURRENT_TIMESTAMP, parameterized queries.
-- See docs/reference/db_adapter_spec.md and docs/reference/implementation_roadmap.md § Phase 1.

BEGIN;

-- ── Market Data projection (MarketDataDTO) ────────────────────────────────────
CREATE TABLE IF NOT EXISTS market_data (
    event_id           TEXT    PRIMARY KEY,
    trace_id           TEXT    NOT NULL,
    correlation_id     TEXT    NOT NULL,
    causation_id       TEXT,
    version_id         TEXT    NOT NULL,
    chain              TEXT    NOT NULL,
    market             TEXT    NOT NULL,
    block_number       BIGINT  NOT NULL,
    block_hash         TEXT    NOT NULL DEFAULT '',
    tx_hash            TEXT    NOT NULL,
    log_index          INTEGER NOT NULL,
    event_topic        TEXT    NOT NULL DEFAULT '',
    pool_address       TEXT    NOT NULL DEFAULT '',
    token_address      TEXT    NOT NULL DEFAULT '',
    base_address       TEXT    NOT NULL DEFAULT '',
    token0_address     TEXT    NOT NULL DEFAULT '',
    token1_address     TEXT    NOT NULL DEFAULT '',
    amount0_raw        TEXT    NOT NULL DEFAULT '0',
    amount1_raw        TEXT    NOT NULL DEFAULT '0',
    reserve_base_raw   TEXT    NOT NULL DEFAULT '0',
    reserve_token_raw  TEXT    NOT NULL DEFAULT '0',
    block_timestamp    TEXT    NOT NULL DEFAULT '',
    ingested_at        TEXT    NOT NULL DEFAULT '',
    rpc_endpoint       TEXT    NOT NULL DEFAULT '',
    transport          TEXT    NOT NULL DEFAULT 'websocket',
    confirmation_depth INTEGER NOT NULL DEFAULT 0,
    reorged            BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at         TEXT,
    priority           INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_market_data_chain
    ON market_data (chain, block_number DESC);
CREATE INDEX IF NOT EXISTS idx_market_data_token
    ON market_data (token_address, chain);

-- ── Ingestion watermark (last processed block per chain) ─────────────────────
CREATE TABLE IF NOT EXISTS ingestion_state (
    chain                TEXT      PRIMARY KEY,
    last_processed_block BIGINT    NOT NULL DEFAULT 0,
    updated_at           TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- ── RPC endpoint health (circuit breaker state) ───────────────────────────────
CREATE TABLE IF NOT EXISTS rpc_endpoint_state (
    endpoint_url       TEXT      PRIMARY KEY,
    chain              TEXT      NOT NULL,
    healthy            BOOLEAN   NOT NULL DEFAULT TRUE,
    consecutive_errors INTEGER   NOT NULL DEFAULT 0,
    last_check_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    circuit_open       BOOLEAN   NOT NULL DEFAULT FALSE
);

COMMIT;
