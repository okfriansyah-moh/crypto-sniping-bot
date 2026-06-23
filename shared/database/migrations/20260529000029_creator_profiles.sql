-- Migration: 20260529000029_creator_profiles.sql
-- Creates the creator_profiles table that stores per-wallet aggregated launch
-- history (Phase 2 — Real Creator Attribution, P1-A Goal 2).
--
-- This table is a materialised view over the event bus: it is rebuilt from
-- the `events` log and `execution_results` table by the creator-profile
-- aggregator worker (Task 8 — internal/workers/creator_profile_aggregator.go).
-- Content-addressable EventIDs in the event bus prevent double-counting during
-- replay, so the table is safe to truncate and re-materialise at any time.
--
-- Consumers:
--   Layer 1 (DQ):    serial_launcher check — per-wallet token count instead of
--                    factory-program count (Task 9, Phase 3 mode-aware gate)
--   Telegram:        /devstats operator command (Task 10) — rug_pull_pct,
--                    migrated_pct, golden_gem_pct, win_rate derived at query time
--   Layer 10 (Learning): creator-cohort analysis (future)
--
-- Update semantics: idempotent CAS upserts via ON CONFLICT DO UPDATE with
-- monotonically incrementing counters — see §7.6 of the implementation plan.
-- An insert that conflicts on (chain, creator_address) adds to existing counters
-- rather than replacing them, preserving replay-safe append-only semantics.
--
-- Indexes:
--   PRIMARY KEY (chain, creator_address)  — O(1) per-creator lookup
--   idx_creator_profiles_total            — top-N by total launches per chain
--   idx_creator_profiles_last_seen        — recency scan / monitoring freshness

BEGIN;

CREATE TABLE IF NOT EXISTS creator_profiles (
    chain              TEXT        NOT NULL,
    creator_address    TEXT        NOT NULL,
    total_tokens       BIGINT      NOT NULL DEFAULT 0,
    rug_tokens         BIGINT      NOT NULL DEFAULT 0,
    migrated_tokens    BIGINT      NOT NULL DEFAULT 0,
    golden_gem_tokens  BIGINT      NOT NULL DEFAULT 0,
    win_tokens         BIGINT      NOT NULL DEFAULT 0,
    loss_tokens        BIGINT      NOT NULL DEFAULT 0,
    first_seen_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (chain, creator_address)
);

CREATE INDEX IF NOT EXISTS idx_creator_profiles_total
    ON creator_profiles (chain, total_tokens DESC);

CREATE INDEX IF NOT EXISTS idx_creator_profiles_last_seen
    ON creator_profiles (chain, last_seen_at DESC);

COMMIT;
