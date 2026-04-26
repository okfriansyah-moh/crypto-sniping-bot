package validation

import (
	"context"
	"testing"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

func defaultValCfg() *config.ValidationConfig {
	return &config.ValidationConfig{
		PriorProbability: 0.55,
		PriorGainBps:     500,
		PriorLossBps:     300,
		PriorSlippageBps: 100,
		EvThresholdBps:   10,
		FixedCostsBps:    50,
		BuildSubmitP95Ms: 500,
		TTLSeconds:       5,
	}
}

func edgeWithType() contracts.EdgeDTO {
	return contracts.EdgeDTO{
		EventID:             "edge-1",
		TraceID:             "trace-1",
		CorrelationID:       "corr-1",
		VersionID:           "v1",
		TokenLifecycleID:    "lc-1",
		TokenAddress:        "0xTOKEN",
		EdgeType:            "NEW_LAUNCH_EDGE",
		EdgeStrength:        0.7,
		OpportunityWindowMs: 10000, // large window, latency gate passes
	}
}

// ── New ──────────────────────────────────────────────────────────────────────

func TestNew_NilConfig_UsesDefaults(t *testing.T) {
	m := New(nil)
	if m == nil {
		t.Fatal("New returned nil")
	}
	if m.cfg.PriorProbability == 0 {
		t.Error("expected non-zero PriorProbability default")
	}
}

// ── Process: accepted ─────────────────────────────────────────────────────────

func acceptCfg() *config.ValidationConfig {
	// EV = 0.7*1000 - 0.3*200 - 20 - 50 = 570 > 10 → ACCEPT
	return &config.ValidationConfig{
		PriorProbability: 0.7,
		PriorGainBps:     1000,
		PriorLossBps:     200,
		PriorSlippageBps: 50,
		EvThresholdBps:   10,
		FixedCostsBps:    20,
		BuildSubmitP95Ms: 500,
		TTLSeconds:       5,
	}
}

func TestProcess_ValidEdge_DecisionACCEPT(t *testing.T) {
	// Arrange — use a config where EV is positive and above threshold.
	m := New(acceptCfg())
	in := edgeWithType()

	// Act
	out, err := m.Process(context.Background(), in)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "ACCEPT" {
		t.Errorf("expected Decision=ACCEPT, got %q (reason: %s)", out.Decision, out.RejectReason)
	}
	if out.RejectReason != "" {
		t.Errorf("expected empty RejectReason for accepted edge: %q", out.RejectReason)
	}
}

func TestProcess_Accepted_TraceFieldsPropagated(t *testing.T) {
	m := New(acceptCfg())
	in := edgeWithType()

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

func TestProcess_Accepted_EVCalculated(t *testing.T) {
	// Use acceptCfg where EV = 570 bps.
	m := New(acceptCfg())
	in := edgeWithType()

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ExpectedValueBps <= 0 {
		t.Errorf("expected positive ExpectedValueBps, got %d", out.ExpectedValueBps)
	}
}

func TestProcess_HighEV_DecisionACCEPT(t *testing.T) {
	// Already covered by acceptCfg; this test validates the EV label output.
	m := New(acceptCfg())
	in := edgeWithType()

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "ACCEPT" {
		t.Errorf("expected ACCEPT for high EV, got %q (reason: %s)", out.Decision, out.RejectReason)
	}
	if out.ExpectedValueBps <= 0 {
		t.Errorf("expected positive ExpectedValueBps, got %d", out.ExpectedValueBps)
	}
}

func TestProcess_Accepted_LatencyGatePassed(t *testing.T) {
	cfg := &config.ValidationConfig{
		PriorProbability: 0.7,
		PriorGainBps:     1000,
		PriorLossBps:     200,
		PriorSlippageBps: 50,
		EvThresholdBps:   10,
		FixedCostsBps:    20,
		BuildSubmitP95Ms: 500,
		TTLSeconds:       5,
	}
	m := New(cfg)
	in := edgeWithType()
	in.OpportunityWindowMs = 10000 // window > BuildSubmitP95Ms

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "ACCEPT" {
		t.Errorf("expected ACCEPT, got %q", out.Decision)
	}
	if !out.LatencyGatePassed {
		t.Error("expected LatencyGatePassed=true when window > p95")
	}
}

func TestProcess_EventIDDeterministic(t *testing.T) {
	cfg := &config.ValidationConfig{
		PriorProbability: 0.7,
		PriorGainBps:     1000,
		PriorLossBps:     200,
		PriorSlippageBps: 50,
		EvThresholdBps:   10,
		FixedCostsBps:    20,
		BuildSubmitP95Ms: 500,
		TTLSeconds:       5,
	}
	m := New(cfg)
	in := edgeWithType()

	out1, _ := m.Process(context.Background(), in)
	out2, _ := m.Process(context.Background(), in)

	if out1.EventID != out2.EventID {
		t.Errorf("EventID not deterministic: %q vs %q", out1.EventID, out2.EventID)
	}
}

// ── Process: rejected ─────────────────────────────────────────────────────────

func TestProcess_EmptyEdgeType_DecisionREJECT(t *testing.T) {
	m := New(defaultValCfg())
	in := edgeWithType()
	in.EdgeType = ""

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected Decision=REJECT for empty EdgeType, got %q", out.Decision)
	}
	if out.RejectReason != "no_edge_detected" {
		t.Errorf("expected no_edge_detected, got %q", out.RejectReason)
	}
}

func TestProcess_LatencyExceedsWindow_DecisionREJECT(t *testing.T) {
	cfg := &config.ValidationConfig{
		PriorProbability: 0.7,
		PriorGainBps:     1000,
		PriorLossBps:     200,
		PriorSlippageBps: 50,
		EvThresholdBps:   10,
		FixedCostsBps:    20,
		BuildSubmitP95Ms: 2000, // latency > window
		TTLSeconds:       5,
	}
	m := New(cfg)
	in := edgeWithType()
	in.OpportunityWindowMs = 500 // window < BuildSubmitP95Ms

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT for latency exceeding window, got %q", out.Decision)
	}
	if out.LatencyGatePassed {
		t.Error("expected LatencyGatePassed=false")
	}
}

func TestProcess_LowEV_DecisionREJECT(t *testing.T) {
	// EV below threshold.
	cfg := &config.ValidationConfig{
		PriorProbability: 0.4,
		PriorGainBps:     100,
		PriorLossBps:     500,
		PriorSlippageBps: 100,
		EvThresholdBps:   50,
		FixedCostsBps:    100,
		BuildSubmitP95Ms: 500,
		TTLSeconds:       5,
	}
	// EV = 0.4*100 - 0.6*500 - 100 - 100 = 40 - 300 - 100 - 100 = -460 < 50
	m := New(cfg)
	in := edgeWithType()
	in.OpportunityWindowMs = 10000

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT for negative EV, got %q", out.Decision)
	}
}

func TestProcess_ExpiresAtSet(t *testing.T) {
	m := New(defaultValCfg())
	in := edgeWithType()

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ExpiresAt == "" {
		t.Error("ExpiresAt must not be empty")
	}
	if out.ValidatedAt == "" {
		t.Error("ValidatedAt must not be empty")
	}
}
