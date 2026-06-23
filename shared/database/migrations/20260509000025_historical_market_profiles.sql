-- Migration: 20260509000025_historical_market_profiles.sql
-- Creates the historical_market_profiles table which stores per-cohort
-- statistical priors computed by the `hydrate` CLI command from the
-- config/historical_seeds.yaml seed dataset.
--
-- Consumers:
--   Layer 1 (DQ):          liquidity_min_usd — per-cohort liquidity floor
--   Layer 4 (Probability): prior_probability, ath_multiple_p50
--   Layer 9 (Position):    time_to_rug_p10_sec — cohort-aware stop-loss gate

CREATE TABLE IF NOT EXISTS historical_market_profiles (
    cohort_key            TEXT    PRIMARY KEY,
    token_count           INTEGER NOT NULL DEFAULT 0,

    -- Liquidity percentiles (USD)
    liquidity_usd_p10     NUMERIC NOT NULL DEFAULT 0,
    liquidity_usd_p50     NUMERIC NOT NULL DEFAULT 0,
    liquidity_usd_p90     NUMERIC NOT NULL DEFAULT 0,

    -- Volume 24h percentiles (USD)
    volume_24h_p10        NUMERIC NOT NULL DEFAULT 0,
    volume_24h_p50        NUMERIC NOT NULL DEFAULT 0,
    volume_24h_p90        NUMERIC NOT NULL DEFAULT 0,

    -- Tx velocity percentiles (txns per hour)
    tx_velocity_p10       NUMERIC NOT NULL DEFAULT 0,
    tx_velocity_p50       NUMERIC NOT NULL DEFAULT 0,
    tx_velocity_p90       NUMERIC NOT NULL DEFAULT 0,

    -- Buy/sell ratio percentiles (buys / sells; > 1 = buy pressure)
    buy_sell_ratio_p10    NUMERIC NOT NULL DEFAULT 1,
    buy_sell_ratio_median NUMERIC NOT NULL DEFAULT 1,
    buy_sell_ratio_p90    NUMERIC NOT NULL DEFAULT 1,

    -- ATH multiple estimates (price at ATH / launch price)
    ath_multiple_p10      NUMERIC NOT NULL DEFAULT 1,
    ath_multiple_p50      NUMERIC NOT NULL DEFAULT 1,
    ath_multiple_p90      NUMERIC NOT NULL DEFAULT 1,

    -- Time-to-rug estimates (seconds from launch to rug; 0 = N/A)
    time_to_rug_p10_sec   NUMERIC NOT NULL DEFAULT 0,
    time_to_rug_p50_sec   NUMERIC NOT NULL DEFAULT 0,

    -- Calibrated thresholds used by DQ / Probability / Position layers
    liquidity_min_usd     NUMERIC NOT NULL DEFAULT 5000,
    prior_probability     NUMERIC NOT NULL DEFAULT 0.35,
    social_presence_rate  NUMERIC NOT NULL DEFAULT 0,

    -- Provenance
    profile_version       TEXT        NOT NULL DEFAULT 'seed_v0',
    computed_at           TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
