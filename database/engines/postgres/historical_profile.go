package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"crypto-sniping-bot/contracts"
)

// UpsertHistoricalProfile persists a cohort-level statistical profile
// computed by the `hydrate` CLI command. Idempotent: ON CONFLICT (cohort_key)
// DO UPDATE SET replaces all numeric columns and refreshes computed_at.
//
// Portable SQL: no engine-specific syntax; ON CONFLICT is standard SQL:2003.
func (d *DB) UpsertHistoricalProfile(ctx context.Context, dto contracts.HistoricalMarketProfileDTO) error {
	if dto.CohortKey == "" {
		return fmt.Errorf("upsert historical profile: cohort_key must be non-empty")
	}
	const q = `
INSERT INTO historical_market_profiles (
    cohort_key,
    token_count,
    liquidity_usd_p10,     liquidity_usd_p50,     liquidity_usd_p90,
    volume_24h_p10,        volume_24h_p50,        volume_24h_p90,
    tx_velocity_p10,       tx_velocity_p50,       tx_velocity_p90,
    buy_sell_ratio_p10,    buy_sell_ratio_median, buy_sell_ratio_p90,
    ath_multiple_p10,      ath_multiple_p50,      ath_multiple_p90,
    time_to_rug_p10_sec,   time_to_rug_p50_sec,
    liquidity_min_usd,
    prior_probability,
    social_presence_rate,
    profile_version,
    computed_at
) VALUES (
    $1,
    $2,
    $3,  $4,  $5,
    $6,  $7,  $8,
    $9,  $10, $11,
    $12, $13, $14,
    $15, $16, $17,
    $18, $19,
    $20,
    $21,
    $22,
    $23,
    $24
)
ON CONFLICT (cohort_key) DO UPDATE SET
    token_count           = EXCLUDED.token_count,
    liquidity_usd_p10     = EXCLUDED.liquidity_usd_p10,
    liquidity_usd_p50     = EXCLUDED.liquidity_usd_p50,
    liquidity_usd_p90     = EXCLUDED.liquidity_usd_p90,
    volume_24h_p10        = EXCLUDED.volume_24h_p10,
    volume_24h_p50        = EXCLUDED.volume_24h_p50,
    volume_24h_p90        = EXCLUDED.volume_24h_p90,
    tx_velocity_p10       = EXCLUDED.tx_velocity_p10,
    tx_velocity_p50       = EXCLUDED.tx_velocity_p50,
    tx_velocity_p90       = EXCLUDED.tx_velocity_p90,
    buy_sell_ratio_p10    = EXCLUDED.buy_sell_ratio_p10,
    buy_sell_ratio_median = EXCLUDED.buy_sell_ratio_median,
    buy_sell_ratio_p90    = EXCLUDED.buy_sell_ratio_p90,
    ath_multiple_p10      = EXCLUDED.ath_multiple_p10,
    ath_multiple_p50      = EXCLUDED.ath_multiple_p50,
    ath_multiple_p90      = EXCLUDED.ath_multiple_p90,
    time_to_rug_p10_sec   = EXCLUDED.time_to_rug_p10_sec,
    time_to_rug_p50_sec   = EXCLUDED.time_to_rug_p50_sec,
    liquidity_min_usd     = EXCLUDED.liquidity_min_usd,
    prior_probability     = EXCLUDED.prior_probability,
    social_presence_rate  = EXCLUDED.social_presence_rate,
    profile_version       = EXCLUDED.profile_version,
    computed_at           = EXCLUDED.computed_at`

	computedAt := dto.ComputedAt
	if computedAt.IsZero() {
		computedAt = time.Now().UTC()
	}

	_, err := d.pool.ExecContext(ctx, q,
		dto.CohortKey,
		dto.TokenCount,
		dto.LiquidityUsdP10, dto.LiquidityUsdP50, dto.LiquidityUsdP90,
		dto.Volume24hP10, dto.Volume24hP50, dto.Volume24hP90,
		dto.TxVelocityP10, dto.TxVelocityP50, dto.TxVelocityP90,
		dto.BuySellRatioP10, dto.BuySellRatioMedian, dto.BuySellRatioP90,
		dto.ATHMultipleP10, dto.ATHMultipleP50, dto.ATHMultipleP90,
		dto.TimeToRugP10Sec, dto.TimeToRugP50Sec,
		dto.LiquidityMinUsd,
		dto.PriorProbability,
		dto.SocialPresenceRate,
		dto.ProfileVersion,
		computedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert historical profile %q: %w", dto.CohortKey, err)
	}
	return nil
}

// GetHistoricalProfile retrieves the profile for a given cohort key.
// Returns (nil, nil) when no row exists for the key.
func (d *DB) GetHistoricalProfile(ctx context.Context, cohortKey string) (*contracts.HistoricalMarketProfileDTO, error) {
	const q = `
SELECT
    cohort_key,
    token_count,
    liquidity_usd_p10,     liquidity_usd_p50,     liquidity_usd_p90,
    volume_24h_p10,        volume_24h_p50,        volume_24h_p90,
    tx_velocity_p10,       tx_velocity_p50,       tx_velocity_p90,
    buy_sell_ratio_p10,    buy_sell_ratio_median, buy_sell_ratio_p90,
    ath_multiple_p10,      ath_multiple_p50,      ath_multiple_p90,
    time_to_rug_p10_sec,   time_to_rug_p50_sec,
    liquidity_min_usd,
    prior_probability,
    social_presence_rate,
    profile_version,
    computed_at
FROM historical_market_profiles
WHERE cohort_key = $1`

	row := d.pool.QueryRowContext(ctx, q, cohortKey)
	dto, err := scanHistoricalProfile(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get historical profile %q: %w", cohortKey, err)
	}
	return dto, nil
}

// ListHistoricalProfiles returns all persisted cohort profiles ordered by
// cohort_key ASC. Returns an empty (non-nil) slice when no rows exist.
func (d *DB) ListHistoricalProfiles(ctx context.Context) ([]contracts.HistoricalMarketProfileDTO, error) {
	const q = `
SELECT
    cohort_key,
    token_count,
    liquidity_usd_p10,     liquidity_usd_p50,     liquidity_usd_p90,
    volume_24h_p10,        volume_24h_p50,        volume_24h_p90,
    tx_velocity_p10,       tx_velocity_p50,       tx_velocity_p90,
    buy_sell_ratio_p10,    buy_sell_ratio_median, buy_sell_ratio_p90,
    ath_multiple_p10,      ath_multiple_p50,      ath_multiple_p90,
    time_to_rug_p10_sec,   time_to_rug_p50_sec,
    liquidity_min_usd,
    prior_probability,
    social_presence_rate,
    profile_version,
    computed_at
FROM historical_market_profiles
ORDER BY cohort_key ASC`

	rows, err := d.pool.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list historical profiles: %w", err)
	}
	defer rows.Close()

	var out []contracts.HistoricalMarketProfileDTO
	for rows.Next() {
		dto, err := scanHistoricalProfile(rows)
		if err != nil {
			return nil, fmt.Errorf("list historical profiles: scan: %w", err)
		}
		out = append(out, *dto)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list historical profiles: rows: %w", err)
	}
	if out == nil {
		out = []contracts.HistoricalMarketProfileDTO{}
	}
	return out, nil
}

// scanner is a common interface satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanHistoricalProfile(s scanner) (*contracts.HistoricalMarketProfileDTO, error) {
	var dto contracts.HistoricalMarketProfileDTO
	err := s.Scan(
		&dto.CohortKey,
		&dto.TokenCount,
		&dto.LiquidityUsdP10, &dto.LiquidityUsdP50, &dto.LiquidityUsdP90,
		&dto.Volume24hP10, &dto.Volume24hP50, &dto.Volume24hP90,
		&dto.TxVelocityP10, &dto.TxVelocityP50, &dto.TxVelocityP90,
		&dto.BuySellRatioP10, &dto.BuySellRatioMedian, &dto.BuySellRatioP90,
		&dto.ATHMultipleP10, &dto.ATHMultipleP50, &dto.ATHMultipleP90,
		&dto.TimeToRugP10Sec, &dto.TimeToRugP50Sec,
		&dto.LiquidityMinUsd,
		&dto.PriorProbability,
		&dto.SocialPresenceRate,
		&dto.ProfileVersion,
		&dto.ComputedAt,
	)
	if err != nil {
		return nil, err
	}
	return &dto, nil
}
