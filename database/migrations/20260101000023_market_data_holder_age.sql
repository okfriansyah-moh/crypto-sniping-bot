-- Migration 000023: Add holder distribution and pool age columns to market_data.
--
-- These fields are needed so that per-token holder and age signals computed
-- at ingestion time are preserved through the pipeline and available when the
-- features worker builds the MarketSnapshot.  Without them
-- rawHolderDistribution / rawTokenAge / rawPrice all return (0, false) for
-- pump.fun tokens, driving minFeatureConfidence to 0.1 and forcing the
-- validation worker onto the 0.35 prior indefinitely.

ALTER TABLE market_data
    ADD COLUMN IF NOT EXISTS holder_dist_known BOOLEAN     NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS holder_count      INTEGER     NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS top5_holder_pct   DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    ADD COLUMN IF NOT EXISTS pool_age_seconds  INTEGER     NOT NULL DEFAULT 0;
