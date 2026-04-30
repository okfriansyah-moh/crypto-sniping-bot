package postgres

// Phase 11 (Reference-Repo Improvements R2 — LEARN/EDGE) — creator-rug
// blacklist persistence. Implements Adapter.UpsertCreatorRugObservation
// and Adapter.GetCreatorBlacklistEntry against the creator_blacklist
// table introduced in migration 20260101000017_phase11_creator_blacklist.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"crypto-sniping-bot/database"
)

// UpsertCreatorRugObservation atomically increments rug_count for the given
// creator on the given chain.
//
// NOT idempotent for repeated calls representing the same logical
// observation: the ON CONFLICT branch increments rug_count and refreshes
// last_seen_at on every call. CreatorRugObservation carries no
// observation_id / learning_record_id, so the adapter cannot de-duplicate.
// Callers (Learning Engine) MUST guarantee at-most-once invocation per
// confirmed rug LearningRecord — see Adapter.UpsertCreatorRugObservation.
func (d *DB) UpsertCreatorRugObservation(ctx context.Context, obs database.CreatorRugObservation) error {
	if obs.CreatorAddress == "" || obs.Chain == "" {
		return fmt.Errorf("upsert creator rug: empty creator_address or chain")
	}
	const q = `
INSERT INTO creator_blacklist (
    creator_address, chain, rug_count, first_seen_at, last_seen_at,
    last_token_address, strategy_version_id
)
VALUES ($1, $2, 1, NOW(), NOW(), $3, $4)
ON CONFLICT (creator_address, chain) DO UPDATE SET
    rug_count           = creator_blacklist.rug_count + 1,
    last_seen_at        = NOW(),
    last_token_address  = EXCLUDED.last_token_address,
    strategy_version_id = EXCLUDED.strategy_version_id`
	if _, err := d.pool.ExecContext(ctx, q,
		obs.CreatorAddress, obs.Chain, obs.TokenAddress, obs.StrategyVersionID,
	); err != nil {
		return fmt.Errorf("upsert creator rug: %w", err)
	}
	return nil
}

// GetCreatorBlacklistEntry returns the blacklist row for (creatorAddress,
// chain). Returns (nil, nil) when absent.
func (d *DB) GetCreatorBlacklistEntry(ctx context.Context, creatorAddress string, chain string) (*database.CreatorBlacklistEntry, error) {
	const q = `
SELECT creator_address, chain, rug_count, first_seen_at, last_seen_at,
       last_token_address, strategy_version_id
FROM creator_blacklist
WHERE creator_address = $1 AND chain = $2`
	row := d.pool.QueryRowContext(ctx, q, creatorAddress, chain)
	var e database.CreatorBlacklistEntry
	if err := row.Scan(
		&e.CreatorAddress, &e.Chain, &e.RugCount,
		&e.FirstSeenAt, &e.LastSeenAt, &e.LastTokenAddress, &e.StrategyVersionID,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get creator blacklist: %w", err)
	}
	return &e, nil
}
