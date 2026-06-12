package edge

import (
	"context"
	"strings"
	"testing"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

func defaultEdgeCfg() *config.EdgeConfig {
	return &config.EdgeConfig{
		MinVelocityScore:     0.2,
		MinLiquidityScore:    0.3,
		MaxAgeSeconds:        30,
		BaseWindowMs:         5000,
		WindowMomentumFactor: 0.2,
		TTLSeconds:           8,
	}
}

func highScoreFeature() contracts.FeatureDTO {
	return contracts.FeatureDTO{
		EventID:          "feat-1",
		TraceID:          "trace-1",
		CorrelationID:    "corr-1",
		VersionID:        "v1",
		TokenLifecycleID: "lc-1",
		TokenAddress:     "0xTOKEN",
		LiquidityScore:   0.8,
		TxVelocityScore:  0.6,
		ContractSafety:   0.9,
		VolumeMomentum:   0.7,
		Confidence: contracts.FeatureConfidence{
			LiquidityScore: 0.7,
			ContractSafety: 0.8,
		},
	}
}

func lowScoreFeature() contracts.FeatureDTO {
	f := highScoreFeature()
	f.EventID = "feat-low"
	f.LiquidityScore = 0.1  // below 0.3 threshold
	f.TxVelocityScore = 0.1 // below 0.2 threshold
	return f
}

// ── New ──────────────────────────────────────────────────────────────────────

func TestNew_NilConfig_UsesDefaults(t *testing.T) {
	m := New(nil)
	if m == nil {
		t.Fatal("New returned nil")
	}
	if m.cfg.MinLiquidityScore == 0 {
		t.Error("expected non-zero MinLiquidityScore default")
	}
}

// ── Process: edge detected ────────────────────────────────────────────────────

func TestProcess_HighScores_EdgeDetected(t *testing.T) {
	// Arrange
	m := New(defaultEdgeCfg())
	in := highScoreFeature()

	// Act
	out, err := m.Process(context.Background(), in)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.EdgeType != "NEW_LAUNCH_EDGE" {
		t.Errorf("expected EdgeType=NEW_LAUNCH_EDGE, got %q", out.EdgeType)
	}
	if out.EdgeStrength == 0 {
		t.Error("EdgeStrength must be > 0 when edge is detected")
	}
	if out.EdgeConfidence == 0 {
		t.Error("EdgeConfidence must be > 0 when edge is detected")
	}
}

func TestProcess_EdgeDetected_TraceFieldsPropagated(t *testing.T) {
	m := New(defaultEdgeCfg())
	in := highScoreFeature()

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.TraceID != "trace-1" {
		t.Errorf("TraceID not propagated: %q", out.TraceID)
	}
	if out.CausationID != in.EventID {
		t.Errorf("CausationID should equal upstream EventID: got %q", out.CausationID)
	}
	if out.TokenAddress != "0xTOKEN" {
		t.Errorf("TokenAddress not propagated: %q", out.TokenAddress)
	}
}

func TestProcess_EdgeDetected_EventIDDeterministic(t *testing.T) {
	m := New(defaultEdgeCfg())
	in := highScoreFeature()

	out1, _ := m.Process(context.Background(), in)
	out2, _ := m.Process(context.Background(), in)

	if out1.EventID != out2.EventID {
		t.Errorf("EventID not deterministic: %q vs %q", out1.EventID, out2.EventID)
	}
}

func TestProcess_EdgeDetected_OpportunityWindowCalculated(t *testing.T) {
	m := New(defaultEdgeCfg())
	in := highScoreFeature()
	in.VolumeMomentum = 0.5

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// MomentumScore(priceMomentum=0, volumeMomentum=0.5) = 0.6*0.5 + 0.4*0 = 0.30
	// window = 5000 * (1 + 0.2 * 0.30) = 5000 * 1.06 = 5300
	expected := int32(5300)
	if out.OpportunityWindowMs != expected {
		t.Errorf("expected OpportunityWindowMs=%d, got %d", expected, out.OpportunityWindowMs)
	}
}

func TestProcess_EdgeDetected_ExpiresAtSet(t *testing.T) {
	m := New(defaultEdgeCfg())
	in := highScoreFeature()

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ExpiresAt == "" {
		t.Error("ExpiresAt must not be empty")
	}
	if out.DetectedAt == "" {
		t.Error("DetectedAt must not be empty")
	}
}

// ── Process: no edge ─────────────────────────────────────────────────────────

func TestProcess_LowScores_NoEdge(t *testing.T) {
	m := New(defaultEdgeCfg())
	in := lowScoreFeature()

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.EdgeType != contracts.EdgeTypeNone {
		t.Errorf("expected EdgeType=NONE for low scores, got %q", out.EdgeType)
	}
	if out.IsEdgeDetected() {
		t.Errorf("NONE must report IsEdgeDetected()=false")
	}
	if out.EdgeStrength != 0 {
		t.Errorf("expected EdgeStrength=0, got %f", out.EdgeStrength)
	}
	if out.RejectReason == "" {
		t.Error("NONE must populate RejectReason")
	}
}

func TestProcess_LowScores_WindowReflectsMomentumScore(t *testing.T) {
	// Per F-4 fix: MomentumScore is always derivable from
	// PriceMomentum/VolumeMomentum and the opportunity window scales
	// with it even when no edge fires — downstream observers see
	// continuous signal evolution.
	cfg := defaultEdgeCfg()
	m := New(cfg)
	in := lowScoreFeature() // VolumeMomentum=0.7, PriceMomentum=0

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// MomentumScore = 0.6*0.7 + 0.4*0 = 0.42
	// window = 5000 * (1 + 0.2 * 0.42) = 5420
	expected := int32(5420)
	if out.OpportunityWindowMs != expected {
		t.Errorf("expected %d, got %d", expected, out.OpportunityWindowMs)
	}
}

// ── Mode-aware edge strength floor (PLAN Task 3) ─────────────────────────────

func TestProcessWithContext_ModeEdgeStrengthFloor_ExplorationAcceptsWeakerThanBalanced(t *testing.T) {
	m := New(defaultEdgeCfg())
	in := highScoreFeature()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// highScoreFeature NEW_LAUNCH strength ≈ 0.59 (between EXPLORATION 0.45 and BALANCED 0.60).
	outBalanced, err := m.ProcessWithContext(context.Background(), in, BaselineSnapshot{}, 0.60, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outBalanced.EdgeType != contracts.EdgeTypeNone {
		t.Errorf("BALANCED floor 0.60 should reject strength ~0.59, got edge_type=%q", outBalanced.EdgeType)
	}
	if !strings.Contains(outBalanced.RejectReason, "edge_strength_below_floor") {
		t.Errorf("expected edge_strength_below_floor reject reason, got %q", outBalanced.RejectReason)
	}

	outExploration, err := m.ProcessWithContext(context.Background(), in, BaselineSnapshot{}, 0.45, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outExploration.EdgeType != contracts.EdgeTypeNewLaunch {
		t.Errorf("EXPLORATION floor 0.45 should accept strength ~0.59, got edge_type=%q", outExploration.EdgeType)
	}
}

// ── minFloat helper ───────────────────────────────────────────────────────────

func TestMinFloat_ReturnsSmaller(t *testing.T) {
	if minFloat(0.3, 0.7) != 0.3 {
		t.Error("expected 0.3")
	}
	if minFloat(0.9, 0.2) != 0.2 {
		t.Error("expected 0.2")
	}
	if minFloat(0.5, 0.5) != 0.5 {
		t.Error("expected 0.5")
	}
}
