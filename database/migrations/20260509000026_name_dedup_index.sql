-- Migration: add functional index for pre-probe token name deduplication.
--
-- The MarketProbesWorker pre-probe guard queries market_data by
-- lower(trim(name)) + chain before running any RPC probe, to skip
-- expensive Helius credits on duplicate-name token launches.
--
-- This partial index covers only rows where name is populated (Solana
-- pump.fun tokens) and is therefore much smaller than a full-table index.
-- The WHERE clause also prevents the planner from selecting it for EVM
-- rows where name is always empty, keeping the index compact.
--
-- Compatible with PostgreSQL 13+ (lower() expression on text column).
-- ON CONFLICT DO NOTHING semantics: index creation is idempotent.

CREATE INDEX IF NOT EXISTS idx_market_data_name_chain
    ON market_data (lower(trim(name)), chain)
    WHERE name IS NOT NULL AND name <> '';
