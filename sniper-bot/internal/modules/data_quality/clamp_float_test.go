package data_quality

import "testing"

// ── clampFloat ────────────────────────────────────────────────────────────────

func TestClampFloat_BelowLo_ReturnsLo(t *testing.T) {
	// Arrange / Act / Assert
	if got := clampFloat(-5.0, 0.0, 1.0); got != 0.0 {
		t.Errorf("clampFloat(-5, 0, 1): want 0, got %v", got)
	}
}

func TestClampFloat_AboveHi_ReturnsHi(t *testing.T) {
	// Arrange / Act / Assert
	if got := clampFloat(10.0, 0.0, 1.0); got != 1.0 {
		t.Errorf("clampFloat(10, 0, 1): want 1, got %v", got)
	}
}

func TestClampFloat_InRange_PassesThrough(t *testing.T) {
	// Arrange / Act / Assert
	if got := clampFloat(0.5, 0.0, 1.0); got != 0.5 {
		t.Errorf("clampFloat(0.5, 0, 1): want 0.5, got %v", got)
	}
}

func TestClampFloat_ExactlyLo_ReturnsLo(t *testing.T) {
	// Arrange / Act / Assert
	if got := clampFloat(0.0, 0.0, 1.0); got != 0.0 {
		t.Errorf("clampFloat(0, 0, 1): want 0, got %v", got)
	}
}

func TestClampFloat_ExactlyHi_ReturnsHi(t *testing.T) {
	// Arrange / Act / Assert
	if got := clampFloat(1.0, 0.0, 1.0); got != 1.0 {
		t.Errorf("clampFloat(1, 0, 1): want 1, got %v", got)
	}
}

func TestClampFloat_NegativeBounds_Works(t *testing.T) {
	// Arrange: clamp to [-10, -1] range.
	if got := clampFloat(-20.0, -10.0, -1.0); got != -10.0 {
		t.Errorf("clampFloat(-20, -10, -1): want -10, got %v", got)
	}
	if got := clampFloat(5.0, -10.0, -1.0); got != -1.0 {
		t.Errorf("clampFloat(5, -10, -1): want -1, got %v", got)
	}
	if got := clampFloat(-5.0, -10.0, -1.0); got != -5.0 {
		t.Errorf("clampFloat(-5, -10, -1): want -5, got %v", got)
	}
}
