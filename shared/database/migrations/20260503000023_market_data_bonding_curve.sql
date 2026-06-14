-- Phase 10 — add bonding_curve_progress_bps to market_data.
-- This column tracks Solana pump.fun / bonk.fun bonding curve progress in bps
-- (0..10000). Populated by ingestion_solana; 0 means "not applicable" (EVM).
-- Used by Layer 1 to reject already-graduated bonding curves at rescan time.

BEGIN;

ALTER TABLE market_data
    ADD COLUMN IF NOT EXISTS bonding_curve_progress_bps INTEGER NOT NULL DEFAULT 0;

COMMIT;
