package edge

import (
	"context"
	"testing"

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
	// window = 5000 * (1 + 0.2 * 0.5) = 5000 * 1.1 = 5500
	expected := int32(5500)
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
	if out.EdgeType != "" {
		t.Errorf("expected empty EdgeType for low scores, got %q", out.EdgeType)
	}
	if out.EdgeStrength != 0 {
		t.Errorf("expected EdgeStrength=0, got %f", out.EdgeStrength)
	}
}

func TestProcess_LowScores_BaseWindowOnlyUsed(t *testing.T) {
	// With no edge (momentum=0), window = BaseWindowMs * 1.0.
	m := New(defaultEdgeCfg())
	in := lowScoreFeature()

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.OpportunityWindowMs != int32(defaultEdgeCfg().BaseWindowMs) {
		t.Errorf("expected base window %d, got %d", defaultEdgeCfg().BaseWindowMs, out.OpportunityWindowMs)
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
