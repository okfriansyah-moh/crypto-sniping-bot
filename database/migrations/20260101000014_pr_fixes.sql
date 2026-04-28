-- Migration: 20260101000014_pr_fixes.sql
-- PR review remediation — additive schema additions only.
-- Safe for rolling deployment: ADD COLUMN IF NOT EXISTS with safe defaults.
-- No DROP, no ALTER COLUMN TYPE, no data loss.

BEGIN;

-- ── positions.amount_raw ──────────────────────────────────────────────────────
-- Stores the last-known on-chain token balance as a decimal string.
-- AdjustPositionAmount writes here instead of current_price (which is a price
-- decimal used by exit evaluation logic). ReconciliationPosition.AmountRaw is
-- now populated by ListOpenPositionsForReconciliation from this column.
ALTER TABLE positions
    ADD COLUMN IF NOT EXISTS amount_raw TEXT NOT NULL DEFAULT '';

-- ── events.block_number ───────────────────────────────────────────────────────
-- On-chain block number in which this event was observed.
-- Zero means the block is unknown (legacy events or off-chain synthetic events).
-- Used by InvalidateBlockRange to apply precise block-range filtering during
-- chain reorganisations without invalidating unrelated events.
ALTER TABLE events
    ADD COLUMN IF NOT EXISTS block_number BIGINT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_events_block_number
    ON events (chain, block_number)
    WHERE block_number > 0;

COMMIT;
