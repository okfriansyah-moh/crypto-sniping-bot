// Package unit contains focused unit tests for exported functions that live in
// packages where adding new _test.go files would conflict with existing ones.
package unit_test

import (
	"testing"

	"crypto-sniping-bot/shared/contracts"
)

// ── PropagateTrace ────────────────────────────────────────────────────────────

func TestPropagateTrace_SetsNewCausationID(t *testing.T) {
	// Arrange
	parent := contracts.NewTraceFields("trace-p", "corr-p", "old-cause", "ver-1")

	// Act
	child := contracts.PropagateTrace(parent, "new-cause-evt")

	// Assert
	if child.CausationID != "new-cause-evt" {
		t.Errorf("CausationID: want new-cause-evt, got %q", child.CausationID)
	}
}

func TestPropagateTrace_PreservesTraceID(t *testing.T) {
	// Arrange
	parent := contracts.NewTraceFields("trace-preserve", "corr-1", "cause-1", "v1")

	// Act
	child := contracts.PropagateTrace(parent, "any-cause")

	// Assert
	if child.TraceID != "trace-preserve" {
		t.Errorf("TraceID must be preserved: got %q", child.TraceID)
	}
}

func TestPropagateTrace_PreservesCorrelationID(t *testing.T) {
	// Arrange
	parent := contracts.NewTraceFields("t1", "corr-preserve", "cause-1", "v1")

	// Act
	child := contracts.PropagateTrace(parent, "any-cause")

	// Assert
	if child.CorrelationID != "corr-preserve" {
		t.Errorf("CorrelationID must be preserved: got %q", child.CorrelationID)
	}
}

func TestPropagateTrace_PreservesVersionID(t *testing.T) {
	// Arrange
	parent := contracts.NewTraceFields("t1", "c1", "cause-1", "ver-preserve")

	// Act
	child := contracts.PropagateTrace(parent, "any-cause")

	// Assert
	if child.VersionID != "ver-preserve" {
		t.Errorf("VersionID must be preserved: got %q", child.VersionID)
	}
}

func TestPropagateTrace_DoesNotMutateParent(t *testing.T) {
	// Arrange
	parent := contracts.NewTraceFields("t1", "c1", "original-cause", "v1")

	// Act
	_ = contracts.PropagateTrace(parent, "different-cause")

	// Assert: parent must be unchanged (value semantics)
	if parent.CausationID != "original-cause" {
		t.Errorf("PropagateTrace mutated parent: CausationID=%q", parent.CausationID)
	}
}

func TestPropagateTrace_Deterministic(t *testing.T) {
	// Arrange
	parent := contracts.NewTraceFields("t1", "c1", "cause-old", "v1")

	// Act: two identical calls
	child1 := contracts.PropagateTrace(parent, "cause-new")
	child2 := contracts.PropagateTrace(parent, "cause-new")

	// Assert
	if child1 != child2 {
		t.Errorf("PropagateTrace is non-deterministic: %v vs %v", child1, child2)
	}
}
