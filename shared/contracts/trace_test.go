package contracts_test

import "testing"
import "crypto-sniping-bot/shared/contracts"

// ── NewTraceFields ────────────────────────────────────────────────────────────

func TestNewTraceFields_SetsAllFields(t *testing.T) {
	// Arrange / Act
	tf := contracts.NewTraceFields("trace-1", "corr-1", "cause-1", "ver-1")

	// Assert
	if tf.TraceID != "trace-1" {
		t.Errorf("TraceID: got %q", tf.TraceID)
	}
	if tf.CorrelationID != "corr-1" {
		t.Errorf("CorrelationID: got %q", tf.CorrelationID)
	}
	if tf.CausationID != "cause-1" {
		t.Errorf("CausationID: got %q", tf.CausationID)
	}
	if tf.VersionID != "ver-1" {
		t.Errorf("VersionID: got %q", tf.VersionID)
	}
}

// ── Propagate ─────────────────────────────────────────────────────────────────

func TestPropagate_PreservesTraceAndCorrelation(t *testing.T) {
	// Arrange
	parent := contracts.NewTraceFields("trace-parent", "corr-parent", "cause-parent", "ver-2")

	// Act
	child := parent.Propagate("new-cause-id")

	// Assert: TraceID and CorrelationID must be preserved
	if child.TraceID != "trace-parent" {
		t.Errorf("TraceID changed: got %q", child.TraceID)
	}
	if child.CorrelationID != "corr-parent" {
		t.Errorf("CorrelationID changed: got %q", child.CorrelationID)
	}
}

func TestPropagate_SetsCausationID(t *testing.T) {
	// Arrange
	parent := contracts.NewTraceFields("t1", "c1", "old-cause", "v1")

	// Act
	child := parent.Propagate("evt-001")

	// Assert
	if child.CausationID != "evt-001" {
		t.Errorf("CausationID: want evt-001, got %q", child.CausationID)
	}
}

func TestPropagate_PreservesVersionID(t *testing.T) {
	// Arrange
	parent := contracts.NewTraceFields("t1", "c1", "old", "ver-42")

	// Act
	child := parent.Propagate("some-cause")

	// Assert
	if child.VersionID != "ver-42" {
		t.Errorf("VersionID changed: got %q", child.VersionID)
	}
}

func TestPropagate_DoesNotMutateParent(t *testing.T) {
	// Arrange
	parent := contracts.NewTraceFields("t1", "c1", "orig-cause", "v1")

	// Act
	_ = parent.Propagate("new-cause")

	// Assert: parent must be unchanged (value semantics)
	if parent.CausationID != "orig-cause" {
		t.Errorf("Propagate mutated parent CausationID: got %q", parent.CausationID)
	}
}
