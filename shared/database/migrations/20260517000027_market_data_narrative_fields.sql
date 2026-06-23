-- Migration: add AI narrative enrichment fields to market_data.
--
-- These columns store the Groq AI narrative probe output populated by
-- internal/modules/probes/ai_narrative_probe.go (Layer 0.5 enrichment).
-- All columns are nullable with safe defaults so existing rows are unaffected.
--
-- Fields:
--   metadata_description    — raw metadata description text used for scoring
--   narrative_known         — false when probe did not run or returned error
--   narrative_score         — 0–10 quality score (0 = no narrative / scam)
--   scam_probability_score  — 0–1 probability from the LLM response
--   is_copy_paste_desc      — true when description appears plagiarised
--   is_impersonation        — true when token impersonates a known project
--   narrative_type          — LLM-assigned category (e.g. "meme", "DePIN")
--   narrative_reason        — short human-readable explanation from LLM

BEGIN;

ALTER TABLE market_data
    ADD COLUMN IF NOT EXISTS metadata_description   TEXT             DEFAULT NULL,
    ADD COLUMN IF NOT EXISTS narrative_known         BOOLEAN          DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS narrative_score         DOUBLE PRECISION DEFAULT 0.0,
    ADD COLUMN IF NOT EXISTS scam_probability_score  DOUBLE PRECISION DEFAULT 0.0,
    ADD COLUMN IF NOT EXISTS is_copy_paste_desc      BOOLEAN          DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS is_impersonation        BOOLEAN          DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS narrative_type          TEXT             DEFAULT NULL,
    ADD COLUMN IF NOT EXISTS narrative_reason        TEXT             DEFAULT NULL;

COMMIT;
