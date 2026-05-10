-- Phase 12 — add creator_address to market_data.
-- Stores the deployer / mint authority wallet so the Layer 1 dev reputation
-- detector can count how many prior tokens the same creator has launched.
-- Column is nullable (TEXT) — older rows and EVM rows without a known
-- deployer will remain NULL; the DQ worker treats NULL as "unknown".

BEGIN;

ALTER TABLE market_data
    ADD COLUMN IF NOT EXISTS creator_address TEXT;

CREATE INDEX IF NOT EXISTS idx_market_data_creator_address
    ON market_data (creator_address)
    WHERE creator_address IS NOT NULL;

COMMIT;
