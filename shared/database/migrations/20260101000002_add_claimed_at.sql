-- Phase 0 patch: add claimed_at to events for atomic worker claiming.
-- Fixes a race condition where FOR UPDATE SKIP LOCKED in autocommit mode
-- releases the row lock before MarkEventProcessed is called, allowing two
-- workers to process the same event concurrently.
--
-- The ClaimNextEvent implementation is changed to an atomic
-- UPDATE ... RETURNING that sets claimed_at, preventing duplicate claims.
-- MarkEventProcessed clears claimed_at on completion.
-- Stale claims (worker crash) are recovered after claim_timeout_seconds.

BEGIN;

ALTER TABLE events ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMP;

-- Partial index for the hot claim path: only unprocessed, unclaimed rows.
CREATE INDEX IF NOT EXISTS idx_events_claimable
    ON events (created_at)
    WHERE processed = FALSE AND claimed_at IS NULL;

COMMIT;
