package postgres

import (
	"context"
	"encoding/json"
	"fmt"
)

// SaveBaseline upserts the rolling-window ring buffer for a single
// (module, market, signal) tuple. Stored oldest-first as a JSONB array.
//
// Portable SQL: ON CONFLICT (...) DO UPDATE SET. updated_at is refreshed
// to NOW() on every write so the idx_baselines_updated_at index can drive
// staleness queries / TTL sweeps if those land later.
//
// Residual-risk #1 fix — see migration 20260101000019.
func (d *DB) SaveBaseline(ctx context.Context, module, market, signal string, values []float64) error {
	if module == "" || market == "" || signal == "" {
		return fmt.Errorf("save baseline: module/market/signal must be non-empty")
	}
	// Marshal explicitly so the SQL driver doesn't have to introspect.
	// nil values must serialise as `[]`, not `null`, to satisfy NOT NULL.
	if values == nil {
		values = []float64{}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return fmt.Errorf("save baseline: marshal values: %w", err)
	}

	const q = `
INSERT INTO baselines (module, market, signal, values, updated_at)
VALUES ($1, $2, $3, $4::jsonb, NOW())
ON CONFLICT (module, market, signal) DO UPDATE
   SET values = EXCLUDED.values,
       updated_at = NOW()`

	if _, err := d.pool.ExecContext(ctx, q, module, market, signal, string(raw)); err != nil {
		return fmt.Errorf("save baseline: %w", err)
	}
	return nil
}

// LoadBaselines returns every persisted (market, signal) → values entry
// for `module`. Returns a non-nil empty map when no rows exist so callers
// can iterate without nil checks.
func (d *DB) LoadBaselines(ctx context.Context, module string) (map[string]map[string][]float64, error) {
	out := make(map[string]map[string][]float64)
	if module == "" {
		return out, nil
	}
	const q = `SELECT market, signal, values FROM baselines WHERE module = $1`
	rows, err := d.pool.QueryContext(ctx, q, module)
	if err != nil {
		return nil, fmt.Errorf("load baselines: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var market, signal string
		var raw []byte
		if err := rows.Scan(&market, &signal, &raw); err != nil {
			return nil, fmt.Errorf("load baselines: scan: %w", err)
		}
		var values []float64
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &values); err != nil {
				return nil, fmt.Errorf("load baselines: unmarshal (%s/%s): %w", market, signal, err)
			}
		}
		bySignal, ok := out[market]
		if !ok {
			bySignal = make(map[string][]float64)
			out[market] = bySignal
		}
		bySignal[signal] = values
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load baselines: rows: %w", err)
	}
	return out, nil
}
