-- Migration 000016 — Phase 10 / Reference-Repo Improvements
-- Tasks A + E: Position trailing stop, partial TP1 scaling, peak-price
-- tracking and volume-staleness time exit.
--
-- Strictly additive: new columns, new defaults preserve historical
-- replay determinism (every existing row reads zero/empty).
--
-- Source skill: position-management (peak-price tracking, trailing
-- after TP1) + monitoring-loop-engine (8-priority exit incl. trailing).
-- Source architecture: docs/reference/architecture.md §4279 (Trailing Protection
-- after TP1).

ALTER TABLE positions
    ADD COLUMN IF NOT EXISTS peak_price            TEXT     NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS peak_observed_at      TEXT     NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS trailing_stop_bps     INTEGER  NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS tp1_filled_pct_bps    INTEGER  NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_volume_usd       NUMERIC  NOT NULL DEFAULT 0.0,
    ADD COLUMN IF NOT EXISTS last_volume_check_at  TEXT     NOT NULL DEFAULT '';

-- No new indexes required: trailing/peak lookups are always scoped to a
-- specific position_id which is already covered by idx_positions_open
-- and idx_positions_latest.
