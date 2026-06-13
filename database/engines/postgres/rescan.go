package postgres

// rescan.go — Postgres implementation of Adapter.GetTokensForRescan.
// Pure read-only query with parameterised SQL.
// See docs/plans/2026-06-10-profit-restoration-plan.md § Task 4 for design rationale.

import (
	"database/sql"
	"errors"
	"fmt"

	"context"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
)

// rescanQuerySQL is the parameterised SQL used by GetTokensForRescan.
// Extracted as a package-level variable so tests can verify its structure
// without a real database connection.
//
// Parameters:
//
//	$1  MinAgeSeconds           (lower bound, inclusive)
//	$2  MaxAgeSeconds           (upper bound, exclusive)
//	$3  chain                   (empty string = all chains)
//	$4  MaxHoneypotScore
//	$5  MaxRugScore
//	$6  MaxBuyTaxBps
//	$7  IncludePassed           (include PASS/RISKY_PASS alongside REJECT)
//	$8  SkipOpenPositions       (exclude tokens currently held as open positions)
//	$9  IncludeSkippedForRetry  (include SKIP'd tokens whose probes timed out)
//	$10 LIMIT
var rescanQuerySQL = `
WITH latest_dq AS (
    SELECT DISTINCT ON (token_address)
        token_address, decision, honeypot_score, rug_score, buy_tax_bps, flags
    FROM data_quality
    ORDER BY token_address, evaluated_at DESC
),
latest_lifecycle AS (
    SELECT DISTINCT ON (token_address)
        token_address, current_state
    FROM token_lifecycle
    ORDER BY token_address, updated_at DESC
)
SELECT DISTINCT ON (md.token_address)
    md.event_id,
    md.trace_id,
    COALESCE(md.correlation_id, ''),
    COALESCE(md.causation_id, ''),
    md.version_id,
    md.chain,
    md.market,
    md.block_number,
    md.block_hash,
    md.tx_hash,
    md.log_index,
    md.event_topic,
    md.pool_address,
    md.token_address,
    md.base_address,
    md.token0_address,
    md.token1_address,
    md.amount0_raw,
    md.amount1_raw,
    md.reserve_base_raw,
    md.reserve_token_raw,
    md.block_timestamp,
    md.ingested_at,
    md.rpc_endpoint,
    md.transport,
    md.confirmation_depth,
    md.reorged,
    COALESCE(md.expires_at, ''),
    md.priority,
    COALESCE(md.liquidity_usd, 0.0),
    COALESCE(md.lp_stats_known, FALSE),
    COALESCE(md.wash_stats_known, FALSE),
    COALESCE(md.tx_count_1m, 0),
    COALESCE(md.unique_wallets_1m, 0),
    COALESCE(md.wallet_entropy, 0.0),
    COALESCE(md.repeat_ratio_1m, 0.0),
    COALESCE(md.holder_dist_known, FALSE),
    COALESCE(md.holder_count, 0),
    COALESCE(md.top5_holder_pct, 0.0),
    COALESCE(md.pool_age_seconds, 0)
FROM market_data md
JOIN latest_dq dq ON dq.token_address = md.token_address
LEFT JOIN latest_lifecycle ll ON ll.token_address = md.token_address
WHERE
    NULLIF(md.ingested_at, '')::timestamptz <= NOW() - ($1 * INTERVAL '1 second')
    AND NULLIF(md.ingested_at, '')::timestamptz >= NOW() - ($2 * INTERVAL '1 second')
    AND ($3 = '' OR md.chain = $3)
    AND COALESCE(dq.honeypot_score, 0) <= $4
    AND COALESCE(dq.rug_score, 0)      <= $5
    AND COALESCE(dq.buy_tax_bps, 0)    <= $6
    AND (
        dq.decision = 'REJECT'
        OR ($7 AND dq.decision IN ('PASS', 'RISKY_PASS'))
        OR ($9 AND dq.decision = 'SKIP'
                AND dq.flags @> '["serial_launcher_skipped"]'::jsonb
                AND COALESCE(md.holder_dist_known, FALSE) = FALSE)
    )
    AND (
        NOT $8
        OR ll.current_state IS NULL
        OR ll.current_state NOT IN (
            'POSITION_OPEN', 'EXECUTION_PENDING', 'CAPITAL_ALLOCATED', 'SELECTED'
        )
    )
ORDER BY md.token_address ASC, md.ingested_at DESC
LIMIT $10
`

// GetTokensForRescan returns up to q.Limit MarketDataDTOs that are eligible
// for a rescan band. All filters are applied server-side.
//
// Results are deterministic: ORDER BY md.token_address ASC, md.ingested_at DESC;
// one row per token (latest market_data row).
//
// Empty result is not an error — returns ([]MarketDataDTO{}, nil).
func (d *DB) GetTokensForRescan(ctx context.Context, q database.RescanQuery) ([]contracts.MarketDataDTO, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}

	// DISTINCT ON (md.token_address) picks the latest row per token via the
	// inner ORDER BY md.token_address, md.ingested_at DESC.
	// Outer ORDER BY enforces deterministic result ordering.
	//
	// Parameterised throughout (no string interpolation) — OWASP A03 safe.
	chain := q.Chain
	rows, err := d.pool.QueryContext(ctx, rescanQuerySQL,
		q.MinAgeSeconds,
		q.MaxAgeSeconds,
		chain,
		q.MaxHoneypotScore,
		q.MaxRugScore,
		int(q.MaxBuyTaxBps),
		q.IncludePassed,
		q.SkipOpenPositions,
		q.IncludeSkippedForRetry,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres.GetTokensForRescan: %w", err)
	}
	defer rows.Close()

	var result []contracts.MarketDataDTO
	for rows.Next() {
		var dto contracts.MarketDataDTO
		if err := rows.Scan(
			&dto.EventID,
			&dto.TraceID,
			&dto.CorrelationID,
			&dto.CausationID,
			&dto.VersionID,
			&dto.Chain,
			&dto.Market,
			&dto.BlockNumber,
			&dto.BlockHash,
			&dto.TxHash,
			&dto.LogIndex,
			&dto.EventTopic,
			&dto.PoolAddress,
			&dto.TokenAddress,
			&dto.BaseAddress,
			&dto.Token0Address,
			&dto.Token1Address,
			&dto.Amount0Raw,
			&dto.Amount1Raw,
			&dto.ReserveBaseRaw,
			&dto.ReserveTokenRaw,
			&dto.BlockTimestamp,
			&dto.IngestedAt,
			&dto.RpcEndpoint,
			&dto.Transport,
			&dto.ConfirmationDepth,
			&dto.Reorged,
			&dto.ExpiresAt,
			&dto.Priority,
			&dto.LiquidityUsd,
			&dto.LpStatsKnown,
			&dto.WashStatsKnown,
			&dto.TxCount1m,
			&dto.UniqueWallets1m,
			&dto.WalletEntropy,
			&dto.RepeatRatio1m,
			&dto.HolderDistKnown,
			&dto.HolderCount,
			&dto.Top5HolderPct,
			&dto.PoolAgeSeconds,
		); err != nil {
			return nil, fmt.Errorf("postgres.GetTokensForRescan: scan: %w", err)
		}
		result = append(result, dto)
	}
	if err := rows.Err(); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []contracts.MarketDataDTO{}, nil
		}
		return nil, fmt.Errorf("postgres.GetTokensForRescan: rows: %w", err)
	}
	if result == nil {
		return []contracts.MarketDataDTO{}, nil
	}
	return result, nil
}
