package validation

import (
	"context"
	"testing"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// acceptEstimateCfg returns a config where EV is clearly positive.
func acceptEstimateCfg() *config.ValidationConfig {
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

func edgeForEstimates() contracts.EdgeDTO {
	return contracts.EdgeDTO{
		EventID:             "edge-est-1",
		TraceID:             "trace-est-1",
		CorrelationID:       "corr-est-1",
		VersionID:           "v1",
		TokenLifecycleID:    "lc-est-1",
		TokenAddress:        "0xTOKENEST",
		EdgeType:            "NEW_LAUNCH_EDGE",
		EdgeStrength:        0.8,
		OpportunityWindowMs: 10000,
	}
}

// TestProcessWithEstimates_AllNil_UsesConfigPriors ensures nil estimates fall back to priors.
func TestProcessWithEstimates_AllNil_UsesConfigPriors(t *testing.T) {
	// Arrange
	m := New(acceptEstimateCfg())
	in := edgeForEstimates()

	// Act
	out, err := m.ProcessWithEstimates(context.Background(), in, nil, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "ACCEPT" {
		t.Errorf("expected ACCEPT with good priors, got %q (reason: %s)", out.Decision, out.RejectReason)
	}
	if out.ProbabilityUsed != acceptEstimateCfg().PriorProbability {
		t.Errorf("expected prior probability %f, got %f",
			acceptEstimateCfg().PriorProbability, out.ProbabilityUsed)
	}
}

// TestProcessWithEstimates_ProbabilityOverride applies provided probability.
func TestProcessWithEstimates_ProbabilityOverride(t *testing.T) {
	// Arrange — override probability with 0.9 (better than prior 0.7)
	m := New(acceptEstimateCfg())
	in := edgeForEstimates()
	prob := &contracts.ProbabilityEstimateDTO{Probability: 0.9}

	// Act
	out, err := m.ProcessWithEstimates(context.Background(), in, prob, nil, nil)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "ACCEPT" {
		t.Errorf("expected ACCEPT, got %q", out.Decision)
	}
	if out.ProbabilityUsed != 0.9 {
		t.Errorf("expected prob=0.9, got %f", out.ProbabilityUsed)
	}
}

// TestProcessWithEstimates_SlippageOverride applies provided slippage.
func TestProcessWithEstimates_SlippageOverride(t *testing.T) {
	// Arrange — very low slippage improves EV
	m := New(acceptEstimateCfg())
	in := edgeForEstimates()
	slip := &contracts.SlippageEstimateDTO{ExpectedP95Bps: 10}

	out, err := m.ProcessWithEstimates(context.Background(), in, nil, slip, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.SlippageP95BpsUsed != 10 {
		t.Errorf("expected slippage=10, got %d", out.SlippageP95BpsUsed)
	}
}

// TestProcessWithEstimates_LatencyOverride applies provided latency profile.
func TestProcessWithEstimates_LatencyOverride(t *testing.T) {
	// Arrange — latency well within window
	m := New(acceptEstimateCfg())
	in := edgeForEstimates()
	in.OpportunityWindowMs = 2000
	lat := &contracts.LatencyProfileDTO{ExpectedP95Ms: 300}

	out, err := m.ProcessWithEstimates(context.Background(), in, nil, nil, lat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.LatencyGatePassed {
		t.Error("expected latency gate to pass")
	}
	if out.ExpectedLatencyMs != 300 {
		t.Errorf("expected latency=300, got %d", out.ExpectedLatencyMs)
	}
}

// TestProcessWithEstimates_NoEdgeType_Rejects ensures empty EdgeType is rejected.
func TestProcessWithEstimates_NoEdgeType_Rejects(t *testing.T) {
	m := New(acceptEstimateCfg())
	in := edgeForEstimates()
	in.EdgeType = ""

	out, err := m.ProcessWithEstimates(context.Background(), in, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT for empty EdgeType, got %q", out.Decision)
	}
	if out.RejectReason != "no_edge_detected" {
		t.Errorf("expected no_edge_detected, got %q", out.RejectReason)
	}
}

// TestProcessWithEstimates_HighSlippage_Rejects when slippage pushes EV below threshold.
func TestProcessWithEstimates_HighSlippage_Rejects(t *testing.T) {
	// Arrange — very high slippage drives EV below threshold
	cfg := &config.ValidationConfig{
		PriorProbability: 0.5,
		PriorGainBps:     100,
		PriorLossBps:     100,
		PriorSlippageBps: 50,
		EvThresholdBps:   50,
		FixedCostsBps:    10,
		BuildSubmitP95Ms: 500,
		TTLSeconds:       5,
	}
	m := New(cfg)
	in := edgeForEstimates()
	// High slippage override: EV = 0.5*100 - 0.5*100 - 10 - 5000 = -5010 < 50
	slip := &contracts.SlippageEstimateDTO{ExpectedP95Bps: 5000}

	out, err := m.ProcessWithEstimates(context.Background(), in, nil, slip, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "REJECT" {
		t.Errorf("expected REJECT for excessive slippage, got %q", out.Decision)
	}
}

// TestProcessWithEstimates_LatencyExceedsWindow_Rejects tests latency gate failure.
func TestProcessWithEstimates_LatencyExceedsWindow_Rejects(t *testing.T) {
	m := New(acceptEstimateCfg())
	in := edgeForEstimates()
	in.OpportunityWindowMs = 100 // tiny window
	lat := &contracts.LatencyProfileDTO{ExpectedP95Ms: 5000} // latency >> window

	out, err := m.ProcessWithEstimates(context.Background(), in, nil, nil, lat)
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

// TestProcessWithEstimates_TraceFieldsPropagated verifies trace fields are copied.
func TestProcessWithEstimates_TraceFieldsPropagated(t *testing.T) {
	m := New(acceptEstimateCfg())
	in := edgeForEstimates()

	out, err := m.ProcessWithEstimates(context.Background(), in, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.TraceID != in.TraceID {
		t.Errorf("TraceID: want %q, got %q", in.TraceID, out.TraceID)
	}
	if out.CorrelationID != in.CorrelationID {
		t.Errorf("CorrelationID not propagated")
	}
	if out.CausationID != in.EventID {
		t.Errorf("CausationID should equal upstream EventID: got %q", out.CausationID)
	}
	if out.VersionID != in.VersionID {
		t.Errorf("VersionID not propagated")
	}
}

// TestProcessWithEstimates_EventIDDeterministic checks content-addressable EventID.
func TestProcessWithEstimates_EventIDDeterministic(t *testing.T) {
	m := New(acceptEstimateCfg())
	in := edgeForEstimates()

	out1, _ := m.ProcessWithEstimates(context.Background(), in, nil, nil, nil)
	out2, _ := m.ProcessWithEstimates(context.Background(), in, nil, nil, nil)

	if out1.EventID != out2.EventID {
		t.Errorf("EventID non-deterministic: %q vs %q", out1.EventID, out2.EventID)
	}
}

// TestProcessWithEstimates_InvalidProbability_UsesPrior verifies OOB probability values are ignored.
func TestProcessWithEstimates_InvalidProbability_UsesPrior(t *testing.T) {
	// Arrange — probability = 0 should be ignored (uses prior)
	m := New(acceptEstimateCfg())
	in := edgeForEstimates()
	prob := &contracts.ProbabilityEstimateDTO{Probability: 0}

	out, err := m.ProcessWithEstimates(context.Background(), in, prob, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ProbabilityUsed != acceptEstimateCfg().PriorProbability {
		t.Errorf("expected prior probability when provided is 0, got %f", out.ProbabilityUsed)
	}
}
