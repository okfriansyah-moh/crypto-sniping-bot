package contracts_test

import (
	"testing"

	"crypto-sniping-bot/contracts"
)

// ── PropagateTrace ────────────────────────────────────────────────────────────

func TestPropagateTrace_PreservesTraceAndCorrelation(t *testing.T) {
	// Arrange
	parent := contracts.NewTraceFields("trace-A", "corr-A", "cause-A", "ver-A")

	// Act
	child := contracts.PropagateTrace(parent, "new-event-id")

	// Assert: TraceID and CorrelationID must propagate unchanged.
	if child.TraceID != "trace-A" {
		t.Errorf("TraceID: want %q, got %q", "trace-A", child.TraceID)
	}
	if child.CorrelationID != "corr-A" {
		t.Errorf("CorrelationID: want %q, got %q", "corr-A", child.CorrelationID)
	}
}

func TestPropagateTrace_SetsCausationID(t *testing.T) {
	// Arrange
	parent := contracts.NewTraceFields("t1", "c1", "old-cause", "v1")

	// Act
	child := contracts.PropagateTrace(parent, "evt-999")

	// Assert
	if child.CausationID != "evt-999" {
		t.Errorf("CausationID: want %q, got %q", "evt-999", child.CausationID)
	}
}

func TestPropagateTrace_PreservesVersionID(t *testing.T) {
	// Arrange
	parent := contracts.NewTraceFields("t1", "c1", "old", "ver-55")

	// Act
	child := contracts.PropagateTrace(parent, "cause-new")

	// Assert
	if child.VersionID != "ver-55" {
		t.Errorf("VersionID: want %q, got %q", "ver-55", child.VersionID)
	}
}

func TestPropagateTrace_DoesNotMutateParent(t *testing.T) {
	// Arrange
	parent := contracts.NewTraceFields("t1", "c1", "orig-cause", "v1")

	// Act
	_ = contracts.PropagateTrace(parent, "replacement-cause")

	// Assert: parent must be unchanged (value semantics).
	if parent.CausationID != "orig-cause" {
		t.Errorf("PropagateTrace mutated parent CausationID: got %q", parent.CausationID)
	}
}

func TestPropagateTrace_EmptyCausationID_IsValidForLayer0(t *testing.T) {
	// Arrange: Layer 0 root events may legitimately use "" as causation.
	parent := contracts.NewTraceFields("t1", "c1", "old", "v1")

	// Act
	child := contracts.PropagateTrace(parent, "")

	// Assert: the function must not reject empty causation — it is the caller's responsibility.
	if child.CausationID != "" {
		t.Errorf("CausationID: want empty string, got %q", child.CausationID)
	}
}
