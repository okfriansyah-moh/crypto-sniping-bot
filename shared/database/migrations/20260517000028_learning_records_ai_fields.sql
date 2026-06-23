-- Migration: add AI loss-explanation fields to learning_records.
--
-- These columns store the Groq AI loss-explanation output populated by
-- internal/modules/learning/loss_explainer.go (Layer 10 learning engine).
-- All columns are nullable with safe defaults so existing rows are unaffected.
--
-- Fields:
--   ai_explanation_known — false when explainer did not run or returned error
--   ai_loss_category     — LLM-assigned loss category (e.g. "bad_entry",
--                          "rug", "execution_slippage", "bad_exit")
--   ai_explanation       — short human-readable explanation from LLM

BEGIN;

ALTER TABLE learning_records
    ADD COLUMN IF NOT EXISTS ai_explanation_known BOOLEAN DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS ai_loss_category     TEXT    DEFAULT NULL,
    ADD COLUMN IF NOT EXISTS ai_explanation       TEXT    DEFAULT NULL;

COMMIT;
