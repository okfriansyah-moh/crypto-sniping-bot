-- Residual risk #1 (rolling-window baseline persistence)
--
-- Persists the in-memory ring-buffer baselines used by the features
-- (Layer 2) and edge (Layer 3) workers so the rolling-window history
-- (z-score normalization, adaptive momentum quantile) survives worker
-- restarts. Without persistence every restart cold-starts the
-- normalizer / adaptive threshold for several minutes.
--
-- The hot path (per-event Append / AppendBatch) MUST stay in-memory.
-- The flush goroutine writes dirty (module, market, signal) rows on a
-- debounced cadence and is best-effort: failures are logged and
-- skipped, never propagated to the worker.
--
-- Idempotent: CREATE TABLE/INDEX IF NOT EXISTS so the migration is
-- re-runnable. Backward-compatible: when the table is empty (fresh
-- deploy or first run after migration) workers start with empty
-- baselines exactly as today.

BEGIN;

CREATE TABLE IF NOT EXISTS baselines (
    module     TEXT        NOT NULL,                       -- 'features' | 'edge'
    market     TEXT        NOT NULL,
    signal     TEXT        NOT NULL,
    values     JSONB       NOT NULL,                       -- []float64 ring buffer (oldest first)
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (module, market, signal)
);

CREATE INDEX IF NOT EXISTS idx_baselines_updated_at
    ON baselines (updated_at);

COMMIT;
