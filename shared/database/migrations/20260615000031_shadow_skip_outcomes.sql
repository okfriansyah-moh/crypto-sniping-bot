-- Shadow SKIP false-negative tracking uses shadow_trades.stage = 'dq_skip'.
-- Index supports Layer 10 calibration queries on pending_skip_fn observations.

CREATE INDEX IF NOT EXISTS idx_shadow_trades_dq_skip
    ON shadow_trades (rejected_at DESC)
    WHERE stage = 'dq_skip';
