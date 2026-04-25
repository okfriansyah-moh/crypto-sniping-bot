package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"crypto-sniping-bot/database"
)

// CreateRun creates a new pipeline run record.
// Idempotent: ON CONFLICT DO NOTHING.
func (d *DB) CreateRun(ctx context.Context, run database.PipelineRun) error {
	const q = `
		INSERT INTO pipeline_runs
		    (run_id, trace_id, status, last_completed_stage, strategy_version_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (run_id) DO NOTHING`

	now := time.Now().UTC().Format(time.RFC3339Nano)
	createdAt := run.CreatedAt
	if createdAt == "" {
		createdAt = now
	}
	updatedAt := run.UpdatedAt
	if updatedAt == "" {
		updatedAt = now
	}

	_, err := d.pool.ExecContext(ctx, q,
		run.RunID,
		run.TraceID,
		run.Status,
		run.LastCompletedStage,
		run.StrategyVersionID,
		createdAt,
		updatedAt,
	)
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}
	return nil
}

// UpdateRunStage checkpoints the last completed stage for a run.
func (d *DB) UpdateRunStage(ctx context.Context, runID string, stage string) error {
	const q = `
		UPDATE pipeline_runs
		   SET last_completed_stage = $1,
		       updated_at           = $2
		 WHERE run_id = $3`

	_, err := d.pool.ExecContext(ctx, q,
		stage,
		time.Now().UTC().Format(time.RFC3339Nano),
		runID,
	)
	if err != nil {
		return fmt.Errorf("update run stage: %w", err)
	}
	return nil
}

// UpdateRunStatus sets the terminal status for a run.
func (d *DB) UpdateRunStatus(ctx context.Context, runID string, status string) error {
	const q = `
		UPDATE pipeline_runs
		   SET status     = $1,
		       updated_at = $2
		 WHERE run_id = $3`

	_, err := d.pool.ExecContext(ctx, q,
		status,
		time.Now().UTC().Format(time.RFC3339Nano),
		runID,
	)
	if err != nil {
		return fmt.Errorf("update run status: %w", err)
	}
	return nil
}

// GetRun fetches a pipeline run by ID.
func (d *DB) GetRun(ctx context.Context, runID string) (*database.PipelineRun, error) {
	const q = `
		SELECT run_id, trace_id, status, last_completed_stage, strategy_version_id, created_at, updated_at
		FROM pipeline_runs
		WHERE run_id = $1`

	var run database.PipelineRun
	err := d.pool.QueryRowContext(ctx, q, runID).Scan(
		&run.RunID,
		&run.TraceID,
		&run.Status,
		&run.LastCompletedStage,
		&run.StrategyVersionID,
		&run.CreatedAt,
		&run.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}
	return &run, nil
}
