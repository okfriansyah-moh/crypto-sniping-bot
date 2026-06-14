-- Phase 10 — rescan layer support indexes.
-- Pure additive: CREATE INDEX IF NOT EXISTS only. No table or column changes.
-- See docs/plans/2026-06-10-profit-restoration-plan.md § Task 2.

BEGIN;

-- Composite index covering (chain, ingested_at) for age-band scans.
CREATE INDEX IF NOT EXISTS idx_market_data_chain_ingested_at
    ON market_data (chain, ingested_at DESC);

-- Latest data_quality row per token (rescan eligibility join).
CREATE INDEX IF NOT EXISTS idx_data_quality_token_evaluated
    ON data_quality (token_address, evaluated_at DESC);

-- Lifecycle current_state lookup for skip_open_positions filter.
CREATE INDEX IF NOT EXISTS idx_lifecycle_token_state
    ON token_lifecycle (token_address, current_state);

COMMIT;
