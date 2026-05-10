package orchestrator_test

import (
	"context"
	"errors"
	"testing"

	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/orchestrator"
)

// ── per-test mock adapters ───────────────────────────────────────────────────

// checkpointAdapter is a minimal adapter mock for checkpoint tests.
// It embeds mockAdapter for all stubs and overrides only the methods under test.
type checkpointAdapter struct {
	mockAdapter
	updateStageErr  error
	updateStatusErr error
	getRun          *database.PipelineRun
	getRunErr       error
}

func (c *checkpointAdapter) UpdateRunStage(_ context.Context, runID, stage string) error {
	if c.updateStageErr != nil {
		return c.updateStageErr
	}
	if r, ok := c.mockAdapter.runs[runID]; ok {
		r.LastCompletedStage = &stage
	}
	return nil
}

func (c *checkpointAdapter) UpdateRunStatus(_ context.Context, runID, status string) error {
	if c.updateStatusErr != nil {
		return c.updateStatusErr
	}
	if r, ok := c.mockAdapter.runs[runID]; ok {
		r.Status = status
	}
	return nil
}

func (c *checkpointAdapter) GetRun(_ context.Context, _ string) (*database.PipelineRun, error) {
	if c.getRunErr != nil {
		return nil, c.getRunErr
	}
	return c.getRun, nil
}
func (c *checkpointAdapter) CountTokensByCreator(_ context.Context, _, _ string) (int32, error) {
	return 0, nil
}

// ── Checkpoint ───────────────────────────────────────────────────────────────

func TestCheckpoint_HappyPath(t *testing.T) {
	// Arrange
	ctx := context.Background()
	runID := "run-001"
	adapter := &checkpointAdapter{mockAdapter: *newMock()}
	adapter.runs[runID] = &database.PipelineRun{RunID: runID}

	// Act
	err := orchestrator.Checkpoint(ctx, adapter, nil, runID, "stage-a")

	// Assert
	if err != nil {
		t.Fatalf("Checkpoint returned unexpected error: %v", err)
	}
	if got := adapter.runs[runID].LastCompletedStage; got == nil || *got != "stage-a" {
		t.Errorf("expected LastCompletedStage=stage-a, got %v", got)
	}
}

func TestCheckpoint_AdapterError_PropagatesError(t *testing.T) {
	// Arrange
	ctx := context.Background()
	sentinel := errors.New("db write failure")
	adapter := &checkpointAdapter{
		mockAdapter:    *newMock(),
		updateStageErr: sentinel,
	}

	// Act
	err := orchestrator.Checkpoint(ctx, adapter, nil, "run-x", "stage-b")

	// Assert
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
}

func TestCheckpoint_NilLogger_DoesNotPanic(t *testing.T) {
	// Arrange
	ctx := context.Background()
	adapter := &checkpointAdapter{mockAdapter: *newMock()}
	adapter.runs["r1"] = &database.PipelineRun{RunID: "r1"}

	// Act / Assert: must not panic
	_ = orchestrator.Checkpoint(ctx, adapter, nil, "r1", "stage-nil-logger")
}

// ── FinalizeRun ──────────────────────────────────────────────────────────────

func TestFinalizeRun_HappyPath(t *testing.T) {
	// Arrange
	ctx := context.Background()
	runID := "run-002"
	adapter := &checkpointAdapter{mockAdapter: *newMock()}
	adapter.runs[runID] = &database.PipelineRun{RunID: runID, Status: "started"}

	// Act
	err := orchestrator.FinalizeRun(ctx, adapter, nil, runID, "completed")

	// Assert
	if err != nil {
		t.Fatalf("FinalizeRun returned unexpected error: %v", err)
	}
	if adapter.runs[runID].Status != "completed" {
		t.Errorf("expected status=completed, got %s", adapter.runs[runID].Status)
	}
}

func TestFinalizeRun_AdapterError_PropagatesError(t *testing.T) {
	// Arrange
	ctx := context.Background()
	sentinel := errors.New("status update failed")
	adapter := &checkpointAdapter{
		mockAdapter:     *newMock(),
		updateStatusErr: sentinel,
	}

	// Act
	err := orchestrator.FinalizeRun(ctx, adapter, nil, "run-y", "failed")

	// Assert
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
}

func TestFinalizeRun_NilLogger_DoesNotPanic(t *testing.T) {
	// Arrange
	ctx := context.Background()
	adapter := &checkpointAdapter{mockAdapter: *newMock()}
	adapter.runs["r2"] = &database.PipelineRun{RunID: "r2"}

	// Act / Assert: must not panic
	_ = orchestrator.FinalizeRun(ctx, adapter, nil, "r2", "partial")
}

// ── ResumeFromCheckpoint ─────────────────────────────────────────────────────

func TestResumeFromCheckpoint_RunWithStage_ReturnsStage(t *testing.T) {
	// Arrange
	ctx := context.Background()
	stage := "stage-c"
	run := &database.PipelineRun{RunID: "run-003", LastCompletedStage: &stage}
	adapter := &checkpointAdapter{mockAdapter: *newMock(), getRun: run}

	// Act
	got, err := orchestrator.ResumeFromCheckpoint(ctx, adapter, "run-003")

	// Assert
	if err != nil {
		t.Fatalf("ResumeFromCheckpoint returned unexpected error: %v", err)
	}
	if got != stage {
		t.Errorf("expected stage=%s, got %s", stage, got)
	}
}

func TestResumeFromCheckpoint_RunWithNoStage_ReturnsEmpty(t *testing.T) {
	// Arrange
	ctx := context.Background()
	run := &database.PipelineRun{RunID: "run-004", LastCompletedStage: nil}
	adapter := &checkpointAdapter{mockAdapter: *newMock(), getRun: run}

	// Act
	got, err := orchestrator.ResumeFromCheckpoint(ctx, adapter, "run-004")

	// Assert
	if err != nil {
		t.Fatalf("ResumeFromCheckpoint returned unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty stage, got %q", got)
	}
}

func TestResumeFromCheckpoint_NotFound_ReturnsError(t *testing.T) {
	// Arrange
	ctx := context.Background()
	adapter := &checkpointAdapter{
		mockAdapter: *newMock(),
		getRunErr:   database.ErrNotFound,
	}

	// Act
	_, err := orchestrator.ResumeFromCheckpoint(ctx, adapter, "no-such-run")

	// Assert
	if !errors.Is(err, database.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
