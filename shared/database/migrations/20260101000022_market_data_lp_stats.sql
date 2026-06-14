-- Additive: expose the liquidity and wash-trading snapshot fields from
-- MarketDataDTO that were always populated in memory but never persisted.
-- Without these columns GetMarketData returns LpStatsKnown=false and
-- LiquidityUsd=0, which collapses liquidity_conf to 0.1 and forces the
-- validation worker onto the fallback prior (0.35), causing ev_bps=-6700
-- and 100% REJECT for every pump.fun token (P-1 root cause, second layer).
--
-- All columns are additive with safe defaults so existing rows retain
-- their previous behaviour (unknown/zero). No backfill is needed.

BEGIN;

ALTER TABLE market_data
    ADD COLUMN IF NOT EXISTS liquidity_usd           DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    ADD COLUMN IF NOT EXISTS lp_stats_known          BOOLEAN          NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS wash_stats_known        BOOLEAN          NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS tx_count_1m             INTEGER          NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS unique_wallets_1m       INTEGER          NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS wallet_entropy          DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    ADD COLUMN IF NOT EXISTS repeat_ratio_1m         DOUBLE PRECISION NOT NULL DEFAULT 0.0;

COMMIT;
