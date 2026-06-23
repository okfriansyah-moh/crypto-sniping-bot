-- 20260101000020_sybil_indicators.sql
-- F-SEC-08 / residual risk #5: persist SybilClusterIndicators on
-- LearningRecordDTO so wash-bypass-via-Sybil-wallets cases become
-- queryable as a distinct loss bucket.
--
-- Additive only. Default NULL preserves backward compatibility for
-- every existing row and every record that does not trip the Sybil
-- heuristic (wins, true-rugs the wash layer caught, etc.).

BEGIN;

ALTER TABLE learning_records
    ADD COLUMN IF NOT EXISTS sybil_indicators JSONB DEFAULT NULL;

COMMIT;
