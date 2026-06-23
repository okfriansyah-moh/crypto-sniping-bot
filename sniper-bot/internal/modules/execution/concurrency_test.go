package execution

import (
	"context"
	"testing"
	"time"
)

// TestNewExecutionSemaphore_InitialLimit verifies the semaphore starts with the specified capacity.
func TestNewExecutionSemaphore_InitialLimit(t *testing.T) {
	// Arrange / Act
	s := NewExecutionSemaphore(5, 1, 20)

	// Assert
	if s.CurrentLimit() != 5 {
		t.Errorf("expected limit=5, got %d", s.CurrentLimit())
	}
}

// TestNewExecutionSemaphore_ClampsToMin ensures initial < minLimit is clamped up.
func TestNewExecutionSemaphore_ClampsToMin(t *testing.T) {
	s := NewExecutionSemaphore(0, 3, 20)
	if s.CurrentLimit() != 3 {
		t.Errorf("expected clamped limit=3, got %d", s.CurrentLimit())
	}
}

// TestNewExecutionSemaphore_ClampsToMax ensures initial > maxLimit is clamped down.
func TestNewExecutionSemaphore_ClampsToMax(t *testing.T) {
	s := NewExecutionSemaphore(100, 1, 10)
	if s.CurrentLimit() != 10 {
		t.Errorf("expected clamped limit=10, got %d", s.CurrentLimit())
	}
}

// TestExecutionSemaphore_AcquireRelease verifies a slot is acquired and released correctly.
func TestExecutionSemaphore_AcquireRelease(t *testing.T) {
	// Arrange
	s := NewExecutionSemaphore(2, 1, 10)

	// Act
	ctx := context.Background()
	if err := s.Acquire(ctx); err != nil {
		t.Fatalf("unexpected error on first Acquire: %v", err)
	}
	if err := s.Acquire(ctx); err != nil {
		t.Fatalf("unexpected error on second Acquire: %v", err)
	}

	// Release both slots so a third acquire succeeds.
	s.Release()
	if err := s.Acquire(ctx); err != nil {
		t.Fatalf("unexpected error after Release: %v", err)
	}
}

// TestExecutionSemaphore_AcquireCancelledContext ensures cancelled context returns error.
func TestExecutionSemaphore_AcquireCancelledContext(t *testing.T) {
	// Arrange — semaphore full (0 initial slots)
	s := NewExecutionSemaphore(2, 1, 10)
	ctx := context.Background()
	_ = s.Acquire(ctx)
	_ = s.Acquire(ctx)

	// Act — cancel context before trying to acquire a third slot
	cancelCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	err := s.Acquire(cancelCtx)

	// Assert
	if err == nil {
		t.Fatal("expected error when semaphore is full and context cancelled")
	}
}

// TestExecutionSemaphore_AdjustLimit_Increase verifies increasing the limit adds capacity.
func TestExecutionSemaphore_AdjustLimit_Increase(t *testing.T) {
	// Arrange — start with limit 2, consume both slots
	s := NewExecutionSemaphore(2, 1, 10)
	ctx := context.Background()
	_ = s.Acquire(ctx)
	_ = s.Acquire(ctx)

	// Act — increase limit to 4, should add 2 tokens
	s.AdjustLimit(4)

	// Assert — can now acquire 2 more
	if s.CurrentLimit() != 4 {
		t.Errorf("expected limit=4 after AdjustLimit, got %d", s.CurrentLimit())
	}
	if err := s.Acquire(ctx); err != nil {
		t.Fatalf("expected to acquire after limit increase: %v", err)
	}
}

// TestExecutionSemaphore_AdjustLimit_Decrease verifies decreasing the limit works.
func TestExecutionSemaphore_AdjustLimit_Decrease(t *testing.T) {
	// Arrange
	s := NewExecutionSemaphore(5, 1, 10)

	// Act
	s.AdjustLimit(2)

	// Assert
	if s.CurrentLimit() != 2 {
		t.Errorf("expected limit=2 after decrease, got %d", s.CurrentLimit())
	}
}

// TestExecutionSemaphore_AdjustLimit_ClampsToMin ensures AdjustLimit respects min.
func TestExecutionSemaphore_AdjustLimit_ClampsToMin(t *testing.T) {
	s := NewExecutionSemaphore(5, 3, 10)
	s.AdjustLimit(1) // below minLimit=3
	if s.CurrentLimit() != 3 {
		t.Errorf("expected limit clamped to min=3, got %d", s.CurrentLimit())
	}
}

// TestExecutionSemaphore_AdjustLimit_ClampsToMax ensures AdjustLimit respects max.
func TestExecutionSemaphore_AdjustLimit_ClampsToMax(t *testing.T) {
	s := NewExecutionSemaphore(5, 1, 10)
	s.AdjustLimit(100) // above maxLimit=10
	if s.CurrentLimit() != 10 {
		t.Errorf("expected limit clamped to max=10, got %d", s.CurrentLimit())
	}
}

// TestExecutionSemaphore_Release_ExtraRelease verifies extra Release does not panic.
func TestExecutionSemaphore_Release_ExtraRelease(t *testing.T) {
	// Arrange — semaphore at full capacity (no outstanding acquires)
	s := NewExecutionSemaphore(2, 1, 5)
	// Extra release should be a no-op (channel already full up to capacity).
	s.Release()
	// No panic means the test passes.
}
