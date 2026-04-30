-- Phase 11 (Reference-Repo Improvements R2 — LEARN/EDGE)
-- Creator-rug blacklist plumbing. Populated by the Learning Engine when
-- a confirmed-rug LearningRecord is observed; consumed by the Edge
-- module via EdgeConfig.MaxCreatorRugCount.
--
-- ON CONFLICT semantics: portable Postgres (skeleton-parallel uses
-- migration-safe SQL only). Increments rug_count when the same creator
-- is observed again, so the table is an upsert log, not an event log.

CREATE TABLE IF NOT EXISTS creator_blacklist (
    creator_address       TEXT        NOT NULL,
    chain                 TEXT        NOT NULL,
    rug_count             INTEGER     NOT NULL DEFAULT 1,
    first_seen_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_token_address    TEXT        NOT NULL DEFAULT '',
    strategy_version_id   TEXT        NOT NULL DEFAULT '',
    PRIMARY KEY (creator_address, chain)
);

CREATE INDEX IF NOT EXISTS idx_creator_blacklist_chain_count
    ON creator_blacklist (chain, rug_count DESC);

CREATE INDEX IF NOT EXISTS idx_creator_blacklist_last_seen
    ON creator_blacklist (last_seen_at DESC);
