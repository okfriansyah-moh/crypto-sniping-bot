-- Phase 12 (DTO transport-only persistence)
--
-- Persist the four transport-only DTO fields that were previously
-- additive on the wire but not stored in the projection tables, so
-- they survive event replay and are queryable for the learning
-- engine, telegram dispatcher, and adaptive controller.
--
-- All ALTERs use ADD COLUMN IF NOT EXISTS so the migration is
-- idempotent and forward-compatible with rows inserted before this
-- migration ran (existing rows take the column DEFAULT).
--
-- Additionally adds `created_at` to data_quality so
-- GetAdaptiveDQStats can compute time-windowed rug-reject rates
-- without parsing the textual `evaluated_at` column.

BEGIN;

-- ── data_quality additions ───────────────────────────────────────────────────
ALTER TABLE data_quality ADD COLUMN IF NOT EXISTS honeypot_score DOUBLE PRECISION DEFAULT 0;
ALTER TABLE data_quality ADD COLUMN IF NOT EXISTS rug_score      DOUBLE PRECISION DEFAULT 0;
ALTER TABLE data_quality ADD COLUMN IF NOT EXISTS wash_score     DOUBLE PRECISION DEFAULT 0;
ALTER TABLE data_quality ADD COLUMN IF NOT EXISTS fake_liq_score DOUBLE PRECISION DEFAULT 0;
ALTER TABLE data_quality ADD COLUMN IF NOT EXISTS tax_score      DOUBLE PRECISION DEFAULT 0;
ALTER TABLE data_quality ADD COLUMN IF NOT EXISTS profile        TEXT             DEFAULT '';
ALTER TABLE data_quality ADD COLUMN IF NOT EXISTS flags          JSONB            DEFAULT '[]'::jsonb;
ALTER TABLE data_quality ADD COLUMN IF NOT EXISTS created_at     TIMESTAMPTZ      DEFAULT CURRENT_TIMESTAMP;

CREATE INDEX IF NOT EXISTS idx_data_quality_created_at
    ON data_quality (created_at DESC);

-- ── edges additions ──────────────────────────────────────────────────────────
ALTER TABLE edges ADD COLUMN IF NOT EXISTS model_version_id TEXT DEFAULT '';

-- ── probability_estimates additions ──────────────────────────────────────────
ALTER TABLE probability_estimates ADD COLUMN IF NOT EXISTS confidence      DOUBLE PRECISION DEFAULT 0;
ALTER TABLE probability_estimates ADD COLUMN IF NOT EXISTS calibration_bin SMALLINT         DEFAULT 0;

-- ── validated_edges additions ────────────────────────────────────────────────
ALTER TABLE validated_edges ADD COLUMN IF NOT EXISTS fallback_reasons JSONB DEFAULT '[]'::jsonb;

COMMIT;
