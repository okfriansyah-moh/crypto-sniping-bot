package orchestrator

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"crypto-sniping-bot/database"
)

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Checkpoint writes the last completed stage for a pipeline run to the database.
// Must be called after every stage completion — never skip.
// Idempotent: calling twice with the same stage is safe.
//
// See docs/reference/orchestrator_spec.md for checkpoint rules.
func Checkpoint(ctx context.Context, adapter database.Adapter, logger *slog.Logger, runID, stage string) error {
	if logger == nil {
		logger = noopLogger()
	}
	if err := adapter.UpdateRunStage(ctx, runID, stage); err != nil {
		return fmt.Errorf("checkpoint stage %s for run %s: %w", stage, runID, err)
	}
	logger.Info("stage_checkpointed",
		"run_id", runID,
		"stage", stage,
		"checkpointed_at", time.Now().UTC().Format(time.RFC3339Nano),
	)
	return nil
}

// FinalizeRun marks a pipeline run with its terminal status.
// Status must be one of: completed, partial, failed.
func FinalizeRun(ctx context.Context, adapter database.Adapter, logger *slog.Logger, runID, status string) error {
	if logger == nil {
		logger = noopLogger()
	}
	if err := adapter.UpdateRunStatus(ctx, runID, status); err != nil {
		return fmt.Errorf("finalize run %s with status %s: %w", runID, status, err)
	}
	logger.Info("run_finalized",
		"run_id", runID,
		"status", status,
		"finalized_at", time.Now().UTC().Format(time.RFC3339Nano),
	)
	return nil
}

// ResumeFromCheckpoint loads the last completed stage for a run.
// Returns "" if the run has no completed stages yet.
func ResumeFromCheckpoint(ctx context.Context, adapter database.Adapter, runID string) (string, error) {
	run, err := adapter.GetRun(ctx, runID)
	if err != nil {
		return "", fmt.Errorf("resume from checkpoint for run %s: %w", runID, err)
	}
	if run.LastCompletedStage == nil {
		return "", nil
	}
	return *run.LastCompletedStage, nil
}
