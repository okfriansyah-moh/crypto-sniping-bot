package contracts_test

import (
	"testing"

	"crypto-sniping-bot/shared/contracts"
)

// ── IsEdgeDetected ────────────────────────────────────────────────────────────

func TestIsEdgeDetected_EmptyEdgeType_ReturnsFalse(t *testing.T) {
	// Arrange
	e := contracts.EdgeDTO{EdgeType: ""}

	// Act / Assert
	if e.IsEdgeDetected() {
		t.Error("IsEdgeDetected() want false for empty EdgeType, got true")
	}
}

func TestIsEdgeDetected_NoneEdgeType_ReturnsFalse(t *testing.T) {
	// Arrange
	e := contracts.EdgeDTO{EdgeType: contracts.EdgeTypeNone}

	// Act / Assert
	if e.IsEdgeDetected() {
		t.Error("IsEdgeDetected() want false for EdgeTypeNone, got true")
	}
}

func TestIsEdgeDetected_NewLaunchEdge_ReturnsTrue(t *testing.T) {
	// Arrange
	e := contracts.EdgeDTO{EdgeType: contracts.EdgeTypeNewLaunch}

	// Act / Assert
	if !e.IsEdgeDetected() {
		t.Errorf("IsEdgeDetected() want true for EdgeType=%q, got false", contracts.EdgeTypeNewLaunch)
	}
}

func TestIsEdgeDetected_MomentumEdge_ReturnsTrue(t *testing.T) {
	// Arrange
	e := contracts.EdgeDTO{EdgeType: contracts.EdgeTypeMomentum}

	// Act / Assert
	if !e.IsEdgeDetected() {
		t.Errorf("IsEdgeDetected() want true for EdgeType=%q, got false", contracts.EdgeTypeMomentum)
	}
}

func TestIsEdgeDetected_ArbitraryNonEmptyNonNone_ReturnsTrue(t *testing.T) {
	// Arrange: any non-empty, non-NONE edge type should be detected.
	edgeTypes := []string{"CUSTOM_EDGE", "ARBITRAGE", "LIQUIDITY_SHIFT"}
	for _, et := range edgeTypes {
		e := contracts.EdgeDTO{EdgeType: et}

		// Act / Assert
		if !e.IsEdgeDetected() {
			t.Errorf("IsEdgeDetected() want true for EdgeType=%q, got false", et)
		}
	}
}
