package postgres

// Task 8 (Production Gate Hardening) — creator profile persistence.
// Implements Adapter.UpsertCreatorProfileOnLaunch, Adapter.IncrementCreatorOutcome,
// and Adapter.GetCreatorProfile against the creator_profiles table introduced in
// migration 20260529000029_creator_profiles.sql.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"crypto-sniping-bot/shared/contracts"
)

// UpsertCreatorProfileOnLaunch increments total_tokens by 1 and refreshes
// last_seen_at for (chain, creator_address). The row is created on first call.
// Idempotent under the event-bus at-most-once contract: the caller MUST process
// each market_data_event exactly once so the counter remains accurate.
func (d *DB) UpsertCreatorProfileOnLaunch(ctx context.Context, chain, creator string) error {
	if chain == "" || creator == "" {
		return nil // silently skip — guard documented in Adapter interface
	}
	const q = `
INSERT INTO creator_profiles (chain, creator_address, total_tokens, last_seen_at)
VALUES ($1, $2, 1, CURRENT_TIMESTAMP)
ON CONFLICT (chain, creator_address) DO UPDATE SET
    total_tokens    = creator_profiles.total_tokens + 1,
    last_seen_at    = CURRENT_TIMESTAMP,
    last_updated_at = CURRENT_TIMESTAMP`
	if _, err := d.pool.ExecContext(ctx, q, chain, creator); err != nil {
		return fmt.Errorf("upsert creator profile on launch: %w", err)
	}
	return nil
}

// IncrementCreatorOutcome increments one named outcome counter for (chain,
// creator_address). Each outcome maps to a dedicated column; no string
// interpolation is used — each case uses a hard-coded const SQL statement.
// Unrecognised outcome strings return nil without updating any row.
func (d *DB) IncrementCreatorOutcome(ctx context.Context, chain, creator, outcome string) error {
	if chain == "" || creator == "" {
		return nil
	}

	// Each outcome has its own const SQL to prevent any possibility of column
	// injection. The switch is the boundary: only known outcomes reach ExecContext.
	const rugSQL = `
UPDATE creator_profiles
SET rug_tokens      = rug_tokens + 1,
    last_updated_at = CURRENT_TIMESTAMP
WHERE chain = $1 AND creator_address = $2`

	const migratedSQL = `
UPDATE creator_profiles
SET migrated_tokens = migrated_tokens + 1,
    last_updated_at = CURRENT_TIMESTAMP
WHERE chain = $1 AND creator_address = $2`

	const goldenSQL = `
UPDATE creator_profiles
SET golden_gem_tokens = golden_gem_tokens + 1,
    last_updated_at   = CURRENT_TIMESTAMP
WHERE chain = $1 AND creator_address = $2`

	const winSQL = `
UPDATE creator_profiles
SET win_tokens      = win_tokens + 1,
    last_updated_at = CURRENT_TIMESTAMP
WHERE chain = $1 AND creator_address = $2`

	const lossSQL = `
UPDATE creator_profiles
SET loss_tokens     = loss_tokens + 1,
    last_updated_at = CURRENT_TIMESTAMP
WHERE chain = $1 AND creator_address = $2`

	var q string
	switch outcome {
	case "rug":
		q = rugSQL
	case "migrated":
		q = migratedSQL
	case "golden":
		q = goldenSQL
	case "win":
		q = winSQL
	case "loss":
		q = lossSQL
	default:
		return nil // unrecognised outcome — silently ignore per interface contract
	}

	if _, err := d.pool.ExecContext(ctx, q, chain, creator); err != nil {
		return fmt.Errorf("increment creator outcome %q: %w", outcome, err)
	}
	return nil
}

// GetCreatorProfile retrieves the full profile DTO for (chain, creator_address).
// Returns (contracts.CreatorProfile{}, false, nil) when no row exists.
func (d *DB) GetCreatorProfile(ctx context.Context, chain, creator string) (contracts.CreatorProfile, bool, error) {
	const q = `
SELECT chain, creator_address,
       total_tokens, rug_tokens, migrated_tokens, golden_gem_tokens,
       win_tokens, loss_tokens,
       first_seen_at, last_seen_at, last_updated_at
FROM creator_profiles
WHERE chain = $1 AND creator_address = $2`

	row := d.pool.QueryRowContext(ctx, q, chain, creator)

	var p contracts.CreatorProfile
	if err := row.Scan(
		&p.Chain, &p.CreatorAddress,
		&p.TotalTokens, &p.RugTokens, &p.MigratedTokens, &p.GoldenGemTokens,
		&p.WinTokens, &p.LossTokens,
		&p.FirstSeenAt, &p.LastSeenAt, &p.LastUpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return contracts.CreatorProfile{}, false, nil
		}
		return contracts.CreatorProfile{}, false, fmt.Errorf("get creator profile: %w", err)
	}
	return p, true, nil
}
