-- Phase 4 Signal Quality projections: probability, slippage, latency models.
-- All SQL uses portable syntax: ON CONFLICT DO NOTHING, CURRENT_TIMESTAMP.
-- See docs/reference/implementation_roadmap.md § Phase 4 and docs/reference/db_adapter_spec.md.

BEGIN;

-- ── Probability Estimates (Layer 4 — model output) ────────────────────────────
CREATE TABLE IF NOT EXISTS probability_estimates (
    event_id            TEXT     PRIMARY KEY,
    trace_id            TEXT     NOT NULL,
    correlation_id      TEXT     NOT NULL,
    causation_id        TEXT,
    version_id          TEXT     NOT NULL,
    token_lifecycle_id  TEXT     NOT NULL DEFAULT '',
    probability         NUMERIC  NOT NULL DEFAULT 0.0,
    calibration         NUMERIC  NOT NULL DEFAULT 0.0,
    model_version_id    TEXT     NOT NULL DEFAULT '',
    expires_at          TEXT,
    priority            INTEGER  NOT NULL DEFAULT 0,
    estimated_at        TEXT     NOT NULL DEFAULT '',
    inserted_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_probability_trace
    ON probability_estimates (trace_id);

CREATE INDEX IF NOT EXISTS idx_probability_lifecycle
    ON probability_estimates (token_lifecycle_id);

-- ── Slippage Estimates (Layer 4 — model output) ───────────────────────────────
CREATE TABLE IF NOT EXISTS slippage_estimates (
    event_id            TEXT     PRIMARY KEY,
    trace_id            TEXT     NOT NULL,
    correlation_id      TEXT     NOT NULL,
    causation_id        TEXT,
    version_id          TEXT     NOT NULL,
    token_lifecycle_id  TEXT     NOT NULL DEFAULT '',
    expected_p50_bps    INTEGER  NOT NULL DEFAULT 0,
    expected_p95_bps    INTEGER  NOT NULL DEFAULT 0,
    model_version_id    TEXT     NOT NULL DEFAULT '',
    expires_at          TEXT,
    priority            INTEGER  NOT NULL DEFAULT 0,
    estimated_at        TEXT     NOT NULL DEFAULT '',
    inserted_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_slippage_trace
    ON slippage_estimates (trace_id);

CREATE INDEX IF NOT EXISTS idx_slippage_lifecycle
    ON slippage_estimates (token_lifecycle_id);

-- ── Latency Profiles (Layer 4 — chain-keyed periodic) ─────────────────────────
CREATE TABLE IF NOT EXISTS latency_profiles (
    event_id            TEXT     PRIMARY KEY,
    trace_id            TEXT     NOT NULL,
    correlation_id      TEXT     NOT NULL,
    causation_id        TEXT,
    version_id          TEXT     NOT NULL,
    chain               TEXT     NOT NULL DEFAULT '',
    expected_p50_ms     INTEGER  NOT NULL DEFAULT 0,
    expected_p95_ms     INTEGER  NOT NULL DEFAULT 0,
    window_size_seconds INTEGER  NOT NULL DEFAULT 0,
    expires_at          TEXT,
    priority            INTEGER  NOT NULL DEFAULT 0,
    estimated_at        TEXT     NOT NULL DEFAULT '',
    inserted_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_latency_chain_inserted
    ON latency_profiles (chain, inserted_at DESC);

COMMIT;
