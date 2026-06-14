-- Phase 2 trading tables: token lifecycle state machine, DTO projections,
-- nonce manager, positions. Referenced by 20260101000006_production_gaps.sql
-- which adds additive columns to execution_results, allocations, learning_records.
-- All SQL uses portable syntax: ON CONFLICT DO NOTHING, CURRENT_TIMESTAMP.
-- See docs/reference/db_adapter_spec.md § 6.3, § 6.5, § 6.6.

BEGIN;

-- ── Token Lifecycle State Machine ─────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS token_lifecycle (
    token_lifecycle_id TEXT        PRIMARY KEY,
    token_address      TEXT        NOT NULL,
    current_state      TEXT        NOT NULL DEFAULT 'DETECTED',
    state_version      BIGINT      NOT NULL DEFAULT 0,
    terminal_reason    TEXT,
    created_at         TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_lifecycle_token
    ON token_lifecycle (token_address)
    WHERE current_state NOT IN ('REJECTED', 'POSITION_CLOSED', 'FAILED');

CREATE INDEX IF NOT EXISTS idx_lifecycle_state
    ON token_lifecycle (current_state);

-- State transition audit log
CREATE TABLE IF NOT EXISTS token_state_transitions (
    id             BIGSERIAL   PRIMARY KEY,
    lifecycle_id   TEXT        NOT NULL REFERENCES token_lifecycle(token_lifecycle_id),
    from_state     TEXT        NOT NULL,
    to_state       TEXT        NOT NULL,
    trace_id       TEXT        NOT NULL DEFAULT '',
    correlation_id TEXT        NOT NULL DEFAULT '',
    reason         TEXT        NOT NULL DEFAULT '',
    actor_worker   TEXT        NOT NULL DEFAULT '',
    transitioned_at TIMESTAMP  NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_transitions_lifecycle
    ON token_state_transitions (lifecycle_id, transitioned_at DESC);

-- CAS violations (concurrent transition attempts)
CREATE TABLE IF NOT EXISTS state_violations (
    id             BIGSERIAL   PRIMARY KEY,
    lifecycle_id   TEXT        NOT NULL,
    from_state     TEXT        NOT NULL,
    to_state       TEXT        NOT NULL,
    reason         TEXT        NOT NULL DEFAULT '',
    recorded_at    TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_violations_lifecycle
    ON state_violations (lifecycle_id);

-- ── Data Quality projection ───────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS data_quality (
    event_id              TEXT     PRIMARY KEY,
    trace_id              TEXT     NOT NULL,
    correlation_id        TEXT     NOT NULL,
    causation_id          TEXT,
    version_id            TEXT     NOT NULL,
    token_lifecycle_id    TEXT     NOT NULL DEFAULT '',
    token_address         TEXT     NOT NULL DEFAULT '',
    chain                 TEXT     NOT NULL DEFAULT '',
    decision              TEXT     NOT NULL DEFAULT 'PASS',
    risk_score            NUMERIC  NOT NULL DEFAULT 0.0,
    is_honeypot           BOOLEAN  NOT NULL DEFAULT FALSE,
    is_fake_liquidity     BOOLEAN  NOT NULL DEFAULT FALSE,
    is_wash_trading       BOOLEAN  NOT NULL DEFAULT FALSE,
    is_rug_risk           BOOLEAN  NOT NULL DEFAULT FALSE,
    is_tax_anomaly        BOOLEAN  NOT NULL DEFAULT FALSE,
    buy_tax_bps           INTEGER  NOT NULL DEFAULT 0,
    sell_tax_bps          INTEGER  NOT NULL DEFAULT 0,
    lp_locked             BOOLEAN  NOT NULL DEFAULT FALSE,
    lp_holder_count       INTEGER  NOT NULL DEFAULT 0,
    contract_verified     BOOLEAN  NOT NULL DEFAULT FALSE,
    reject_reasons        JSONB    NOT NULL DEFAULT '[]',
    expires_at            TEXT,
    priority              INTEGER  NOT NULL DEFAULT 0,
    evaluated_at          TEXT     NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_data_quality_token
    ON data_quality (token_address, evaluated_at DESC);

-- ── Feature projection ────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS features (
    event_id              TEXT     PRIMARY KEY,
    trace_id              TEXT     NOT NULL,
    correlation_id        TEXT     NOT NULL,
    causation_id          TEXT,
    version_id            TEXT     NOT NULL,
    token_lifecycle_id    TEXT     NOT NULL DEFAULT '',
    token_address         TEXT     NOT NULL DEFAULT '',
    liquidity_score       NUMERIC  NOT NULL DEFAULT 0.0,
    tx_velocity_score     NUMERIC  NOT NULL DEFAULT 0.0,
    holder_distribution   NUMERIC  NOT NULL DEFAULT 0.0,
    wallet_entropy        NUMERIC  NOT NULL DEFAULT 0.0,
    contract_safety       NUMERIC  NOT NULL DEFAULT 0.0,
    token_age             NUMERIC  NOT NULL DEFAULT 0.0,
    volume_momentum       NUMERIC  NOT NULL DEFAULT 0.0,
    price_momentum        NUMERIC  NOT NULL DEFAULT 0.0,
    liquidity_usd_raw     NUMERIC  NOT NULL DEFAULT 0.0,
    tx_velocity_30s_raw   NUMERIC  NOT NULL DEFAULT 0.0,
    holder_count_raw      BIGINT   NOT NULL DEFAULT 0,
    token_age_seconds_raw BIGINT   NOT NULL DEFAULT 0,
    expires_at            TEXT,
    priority              INTEGER  NOT NULL DEFAULT 0,
    extracted_at          TEXT     NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_features_token
    ON features (token_address, extracted_at DESC);

-- ── Edge projection ───────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS edges (
    event_id                TEXT     PRIMARY KEY,
    trace_id                TEXT     NOT NULL,
    correlation_id          TEXT     NOT NULL,
    causation_id            TEXT,
    version_id              TEXT     NOT NULL,
    token_lifecycle_id      TEXT     NOT NULL DEFAULT '',
    token_address           TEXT     NOT NULL DEFAULT '',
    edge_type               TEXT     NOT NULL DEFAULT 'NEW_LAUNCH',
    edge_strength           NUMERIC  NOT NULL DEFAULT 0.0,
    edge_confidence         NUMERIC  NOT NULL DEFAULT 0.0,
    momentum_score          NUMERIC  NOT NULL DEFAULT 0.0,
    threshold_applied       NUMERIC  NOT NULL DEFAULT 0.0,
    opportunity_window_ms   INTEGER  NOT NULL DEFAULT 0,
    expires_at              TEXT,
    priority                INTEGER  NOT NULL DEFAULT 0,
    detected_at             TEXT     NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_edges_token
    ON edges (token_address, detected_at DESC);

-- ── Validated Edge projection ─────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS validated_edges (
    event_id                 TEXT     PRIMARY KEY,
    trace_id                 TEXT     NOT NULL,
    correlation_id           TEXT     NOT NULL,
    causation_id             TEXT,
    version_id               TEXT     NOT NULL,
    token_lifecycle_id       TEXT     NOT NULL DEFAULT '',
    token_address            TEXT     NOT NULL DEFAULT '',
    decision                 TEXT     NOT NULL DEFAULT 'ACCEPT',
    expected_value_bps       INTEGER  NOT NULL DEFAULT 0,
    expected_gain_bps        INTEGER  NOT NULL DEFAULT 0,
    expected_loss_bps        INTEGER  NOT NULL DEFAULT 0,
    fixed_costs_bps          INTEGER  NOT NULL DEFAULT 0,
    probability_used         NUMERIC  NOT NULL DEFAULT 0.0,
    slippage_p95_bps_used    INTEGER  NOT NULL DEFAULT 0,
    ev_threshold_applied     INTEGER  NOT NULL DEFAULT 0,
    reject_reason            TEXT     NOT NULL DEFAULT '',
    expected_latency_ms      INTEGER  NOT NULL DEFAULT 0,
    latency_gate_passed      BOOLEAN  NOT NULL DEFAULT TRUE,
    expires_at               TEXT,
    priority                 INTEGER  NOT NULL DEFAULT 0,
    validated_at             TEXT     NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_validated_edges_token
    ON validated_edges (token_address, validated_at DESC);

-- ── Selection projection ──────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS selections (
    event_id          TEXT     PRIMARY KEY,
    trace_id          TEXT     NOT NULL,
    correlation_id    TEXT     NOT NULL,
    causation_id      TEXT,
    version_id        TEXT     NOT NULL,
    token_lifecycle_id TEXT    NOT NULL DEFAULT '',
    token_address     TEXT     NOT NULL DEFAULT '',
    selected          BOOLEAN  NOT NULL DEFAULT FALSE,
    rank              INTEGER  NOT NULL DEFAULT 0,
    combined_score    NUMERIC  NOT NULL DEFAULT 0.0,
    diversity_bucket  TEXT     NOT NULL DEFAULT '',
    is_exploration    BOOLEAN  NOT NULL DEFAULT FALSE,
    reject_reason     TEXT     NOT NULL DEFAULT '',
    expires_at        TEXT,
    priority          INTEGER  NOT NULL DEFAULT 0,
    selected_at       TEXT     NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_selections_token
    ON selections (token_address, selected_at DESC);

-- ── Allocation projection (base; 20260101000006 adds rejected/cohort columns) ──
CREATE TABLE IF NOT EXISTS allocations (
    event_id          TEXT     PRIMARY KEY,
    trace_id          TEXT     NOT NULL,
    correlation_id    TEXT     NOT NULL,
    causation_id      TEXT,
    version_id        TEXT     NOT NULL,
    token_lifecycle_id TEXT    NOT NULL DEFAULT '',
    token_address     TEXT     NOT NULL DEFAULT '',
    chain             TEXT     NOT NULL DEFAULT '',
    execution_id      TEXT     NOT NULL DEFAULT '',
    size_usd          NUMERIC  NOT NULL DEFAULT 0.0,
    size_base_raw     TEXT     NOT NULL DEFAULT '0',
    max_slippage_bps  INTEGER  NOT NULL DEFAULT 0,
    wallet_address    TEXT     NOT NULL DEFAULT '',
    wallet_shard      INTEGER  NOT NULL DEFAULT 0,
    expires_at        TEXT,
    priority          INTEGER  NOT NULL DEFAULT 0,
    allocated_at      TEXT     NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_allocations_execution
    ON allocations (execution_id);

-- ── Execution Results projection (base; 20260101000006 adds mev/path columns) ─
CREATE TABLE IF NOT EXISTS execution_results (
    event_id              TEXT     PRIMARY KEY,
    trace_id              TEXT     NOT NULL,
    correlation_id        TEXT     NOT NULL,
    causation_id          TEXT,
    version_id            TEXT     NOT NULL,
    token_lifecycle_id    TEXT     NOT NULL DEFAULT '',
    execution_id          TEXT     NOT NULL DEFAULT '',
    allocation_id         TEXT     NOT NULL DEFAULT '',
    status                TEXT     NOT NULL DEFAULT 'failed',
    success               BOOLEAN  NOT NULL DEFAULT FALSE,
    tx_hash               TEXT     NOT NULL DEFAULT '',
    block_number          BIGINT   NOT NULL DEFAULT 0,
    attempts              INTEGER  NOT NULL DEFAULT 0,
    replaced              BOOLEAN  NOT NULL DEFAULT FALSE,
    replacement_count     INTEGER  NOT NULL DEFAULT 0,
    mempool_route         TEXT     NOT NULL DEFAULT 'public',
    nonce_used            BIGINT   NOT NULL DEFAULT 0,
    wallet_address        TEXT     NOT NULL DEFAULT '',
    wallet_shard          INTEGER  NOT NULL DEFAULT 0,
    final_gas_used        BIGINT   NOT NULL DEFAULT 0,
    final_max_fee_wei     TEXT     NOT NULL DEFAULT '0',
    final_priority_fee_wei TEXT    NOT NULL DEFAULT '0',
    realized_entry_price  TEXT     NOT NULL DEFAULT '0',
    slippage_realized_bps INTEGER  NOT NULL DEFAULT 0,
    latency_ms            INTEGER  NOT NULL DEFAULT 0,
    error_code            TEXT     NOT NULL DEFAULT '',
    expires_at            TEXT,
    priority              INTEGER  NOT NULL DEFAULT 0,
    completed_at          TEXT     NOT NULL DEFAULT ''
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_executions_execution_id
    ON execution_results (execution_id);

-- ── Positions projection ──────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS positions (
    event_id           TEXT     PRIMARY KEY,
    trace_id           TEXT     NOT NULL,
    correlation_id     TEXT     NOT NULL,
    causation_id       TEXT,
    version_id         TEXT     NOT NULL,
    token_lifecycle_id TEXT     NOT NULL DEFAULT '',
    position_id        TEXT     NOT NULL DEFAULT '',
    execution_id       TEXT     NOT NULL DEFAULT '',
    token_address      TEXT     NOT NULL DEFAULT '',
    chain              TEXT     NOT NULL DEFAULT '',
    status             TEXT     NOT NULL DEFAULT 'open',
    entry_price        TEXT     NOT NULL DEFAULT '0',
    entry_size_usd     NUMERIC  NOT NULL DEFAULT 0.0,
    current_price      TEXT     NOT NULL DEFAULT '',
    exit_price         TEXT     NOT NULL DEFAULT '',
    exit_reason        TEXT     NOT NULL DEFAULT '',
    pnl_usd            NUMERIC  NOT NULL DEFAULT 0.0,
    pnl_pct            NUMERIC  NOT NULL DEFAULT 0.0,
    tp1_bps            INTEGER  NOT NULL DEFAULT 0,
    tp2_bps            INTEGER  NOT NULL DEFAULT 0,
    sl_bps             INTEGER  NOT NULL DEFAULT 0,
    max_hold_seconds   INTEGER  NOT NULL DEFAULT 0,
    expires_at         TEXT,
    priority           INTEGER  NOT NULL DEFAULT 0,
    opened_at          TEXT     NOT NULL DEFAULT '',
    exited_at          TEXT     NOT NULL DEFAULT '',
    snapshot_at        TEXT     NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_positions_open
    ON positions (position_id, status)
    WHERE status = 'open';

CREATE INDEX IF NOT EXISTS idx_positions_token
    ON positions (token_address, opened_at DESC);

-- Latest snapshot per position (for GetOpenPositions)
CREATE INDEX IF NOT EXISTS idx_positions_latest
    ON positions (position_id, snapshot_at DESC);

-- ── Wallet Nonce State ────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS wallet_nonce_state (
    wallet_address TEXT     NOT NULL,
    chain          TEXT     NOT NULL,
    nonce_value    BIGINT   NOT NULL DEFAULT 0,
    updated_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (wallet_address, chain)
);

-- ── Learning Records projection (base; 20260101000006 adds simulated/etc) ─────
CREATE TABLE IF NOT EXISTS learning_records (
    event_id           TEXT     PRIMARY KEY,
    trace_id           TEXT     NOT NULL,
    correlation_id     TEXT     NOT NULL,
    causation_id       TEXT,
    version_id         TEXT     NOT NULL,
    record_id          TEXT     NOT NULL DEFAULT '',
    token_lifecycle_id TEXT     NOT NULL DEFAULT '',
    shadow             BOOLEAN  NOT NULL DEFAULT FALSE,
    outcome            TEXT     NOT NULL DEFAULT '',
    classification     TEXT     NOT NULL DEFAULT '',
    pnl_usd            NUMERIC  NOT NULL DEFAULT 0.0,
    pnl_pct            NUMERIC  NOT NULL DEFAULT 0.0,
    prediction_error   NUMERIC  NOT NULL DEFAULT 0.0,
    cohort             TEXT     NOT NULL DEFAULT '',
    features_snapshot  JSONB    NOT NULL DEFAULT '{}',
    edge_snapshot      JSONB    NOT NULL DEFAULT '{}',
    validated_snapshot JSONB    NOT NULL DEFAULT '{}',
    expires_at         TEXT,
    priority           INTEGER  NOT NULL DEFAULT 0,
    recorded_at        TEXT     NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_learning_records_lifecycle
    ON learning_records (token_lifecycle_id, recorded_at DESC);

-- ── Evaluations projection ────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS evaluations (
    event_id               TEXT     PRIMARY KEY,
    trace_id               TEXT     NOT NULL,
    correlation_id         TEXT     NOT NULL,
    causation_id           TEXT,
    version_id             TEXT     NOT NULL,
    evaluation_id          TEXT     NOT NULL DEFAULT '',
    window_start           TEXT     NOT NULL DEFAULT '',
    window_end             TEXT     NOT NULL DEFAULT '',
    sample_size            INTEGER  NOT NULL DEFAULT 0,
    true_positive_count    INTEGER  NOT NULL DEFAULT 0,
    false_positive_count   INTEGER  NOT NULL DEFAULT 0,
    true_negative_count    INTEGER  NOT NULL DEFAULT 0,
    false_negative_count   INTEGER  NOT NULL DEFAULT 0,
    expectancy             NUMERIC  NOT NULL DEFAULT 0.0,
    max_drawdown_pct       NUMERIC  NOT NULL DEFAULT 0.0,
    brier_score            NUMERIC  NOT NULL DEFAULT 0.0,
    prediction_error_mean  NUMERIC  NOT NULL DEFAULT 0.0,
    expires_at             TEXT,
    priority               INTEGER  NOT NULL DEFAULT 0,
    evaluated_at           TEXT     NOT NULL DEFAULT ''
);

COMMIT;
