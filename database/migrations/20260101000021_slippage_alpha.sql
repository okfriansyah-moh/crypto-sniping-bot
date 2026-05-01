-- Per-market slippage α calibration (residual risk #3).
-- Populated by the alpha aggregator worker from realized fills
-- (execution_quality.AlphaAggregator). The Layer 4 slippage model
-- reads via Adapter.GetSlippageAlpha(market) — cold-start returns 1.0.
-- All SQL portable: ON CONFLICT semantics, CURRENT_TIMESTAMP, no engine-specific syntax.

BEGIN;

CREATE TABLE IF NOT EXISTS slippage_alpha_calibrations (
    market         TEXT             NOT NULL PRIMARY KEY,
    alpha          DOUBLE PRECISION NOT NULL,
    sample_count   INTEGER          NOT NULL,
    computed_at    TIMESTAMP        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    ewma_predicted DOUBLE PRECISION NOT NULL,
    ewma_realized  DOUBLE PRECISION NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_slippage_alpha_computed_at
    ON slippage_alpha_calibrations (computed_at);

COMMIT;
