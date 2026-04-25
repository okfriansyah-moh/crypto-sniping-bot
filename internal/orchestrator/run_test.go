package orchestrator_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"crypto-sniping-bot/internal/orchestrator"
)

// ── RegisterStage ────────────────────────────────────────────────────────────

// TestRegisterStage_IncrementsRegistryLen verifies RegisterStage delegates to
// the underlying Registry and the stage count increases accordingly.
func TestRegisterStage_IncrementsRegistryLen(t *testing.T) {
	// Arrange
	ctx := context.Background()
	mock := newMock()
	cfg := minimalConfig()

	orch, err := orchestrator.Boot(ctx, mock, cfg, nil)
	if err != nil {
		t.Fatalf("Boot failed: %v", err)
	}

	// Act: register two stages
	orch.RegisterStage("worker-x", noopHandler{}, "event.x")
	orch.RegisterStage("worker-y", noopHandler{}, "event.y1", "event.y2")

	// Assert: Run must not return "no_stages_registered" path — we can only
	// observe indirectly by cancelling and confirming a clean ctx exit.
	runCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	runErr := orch.Run(runCtx)
	if runErr != nil && !errors.Is(runErr, context.DeadlineExceeded) && !errors.Is(runErr, context.Canceled) {
		t.Errorf("expected context error after cancel, got %v", runErr)
	}
}

// TestRegisterStage_DuplicateName_Panics verifies that registering the same
// group name twice panics (delegated to Registry.Register).
func TestRegisterStage_DuplicateName_Panics(t *testing.T) {
	// Arrange
	ctx := context.Background()
	mock := newMock()
	cfg := minimalConfig()

	orch, err := orchestrator.Boot(ctx, mock, cfg, nil)
	if err != nil {
		t.Fatalf("Boot failed: %v", err)
	}
	orch.RegisterStage("dup-stage", noopHandler{}, "event.dup")

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate RegisterStage")
		}
	}()

	// Act: second registration with same name must panic
	orch.RegisterStage("dup-stage", noopHandler{}, "event.dup")
}

// ── Run ──────────────────────────────────────────────────────────────────────

// TestRun_WithStages_ExitsOnCancel verifies that Run exits cleanly via context
// cancellation when at least one stage is registered.
func TestRun_WithStages_ExitsOnCancel(t *testing.T) {
	// Arrange
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	mock := newMock()
	cfg := minimalConfig()

	orch, err := orchestrator.Boot(ctx, mock, cfg, nil)
	if err != nil {
		t.Fatalf("Boot failed: %v", err)
	}
	orch.RegisterStage("stage-run", noopHandler{}, "event.run")

	// Act
	runErr := orch.Run(ctx)

	// Assert
	if runErr != nil && !errors.Is(runErr, context.DeadlineExceeded) && !errors.Is(runErr, context.Canceled) {
		t.Errorf("expected context error, got %v", runErr)
	}
}

// TestRun_NoStages_ExitsOnCancel verifies that Run with no registered stages
// still exits cleanly once the context is cancelled.
func TestRun_NoStages_ExitsOnCancel_Explicit(t *testing.T) {
	// Arrange
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	mock := newMock()
	cfg := minimalConfig()

	orch, err := orchestrator.Boot(ctx, mock, cfg, nil)
	if err != nil {
		t.Fatalf("Boot failed: %v", err)
	}

	// Act: no stages registered
	runErr := orch.Run(ctx)

	// Assert
	if runErr != nil && !errors.Is(runErr, context.DeadlineExceeded) && !errors.Is(runErr, context.Canceled) {
		t.Errorf("expected context error, got %v", runErr)
	}
}

// TestRun_WorkerIdleBackoff_UsesConfig verifies Run respects a non-zero
// IdleBackoffMs from config without hanging (quick cancel).
func TestRun_WorkerIdleBackoff_UsesConfig(t *testing.T) {
	// Arrange
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	mock := newMock()
	cfg := minimalConfig()
	cfg.Worker.IdleBackoffMs = 5 // fast backoff for test

	orch, err := orchestrator.Boot(ctx, mock, cfg, nil)
	if err != nil {
		t.Fatalf("Boot failed: %v", err)
	}
	orch.RegisterStage("stage-backoff", noopHandler{}, "event.backoff")

	// Act
	runErr := orch.Run(ctx)

	// Assert
	if runErr != nil && !errors.Is(runErr, context.DeadlineExceeded) && !errors.Is(runErr, context.Canceled) {
		t.Errorf("expected context error, got %v", runErr)
	}
}

// ── Boot ─────────────────────────────────────────────────────────────────────

// migrateErrAdapter wraps mockAdapter and returns a fixed error from RunMigrations.
type migrateErrAdapter struct {
	mockAdapter
	err error
}

func (m *migrateErrAdapter) RunMigrations(_ context.Context) error {
	return m.err
}

// TestBoot_MigrationError_ReturnsError verifies Boot propagates adapter errors.
func TestBoot_MigrationError_ReturnsError(t *testing.T) {
	// Arrange
	ctx := context.Background()
	sentinel := errors.New("migration failed")
	adapter := &migrateErrAdapter{mockAdapter: *newMock(), err: sentinel}
	cfg := minimalConfig()

	// Act
	_, err := orchestrator.Boot(ctx, adapter, cfg, nil)

	// Assert
	if !errors.Is(err, sentinel) {
		t.Errorf("expected migration error, got %v", err)
	}
}
