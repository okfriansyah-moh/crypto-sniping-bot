package validation

import (
	"context"
	"math"
	"testing"
	"time"

	"crypto-sniping-bot/shared/contracts"
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
	in.OpportunityWindowMs = 100                             // tiny window
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

// TestProcessWithEstimates_BoundaryZero_UsesZero verifies F-SEC-01: with no
// strict-reject runtime config attached, Probability=0.0 is honoured exactly
// (boundary-inclusive). The previous `> 0 && < 1` check silently substituted
// the prior — that boundary-value silent prior was the security finding.
func TestProcessWithEstimates_BoundaryZero_UsesZero(t *testing.T) {
	m := New(acceptEstimateCfg())
	in := edgeForEstimates()
	prob := &contracts.ProbabilityEstimateDTO{Probability: 0.0}

	out, err := m.ProcessWithEstimates(context.Background(), in, prob, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ProbabilityUsed != 0.0 {
		t.Errorf("Probability=0.0 must be used exactly; got %v", out.ProbabilityUsed)
	}
	for _, r := range out.FallbackReasons {
		if r == "prob_boundary_value" {
			t.Errorf("0.0 is in [0,1]; must NOT emit prob_boundary_value; got %v", out.FallbackReasons)
		}
	}
}

// TestProcessWithEstimates_BoundaryOne_UsesOne verifies Probability=1.0 is
// honoured exactly (F-SEC-01 boundary-inclusive).
func TestProcessWithEstimates_BoundaryOne_UsesOne(t *testing.T) {
	m := New(acceptEstimateCfg())
	in := edgeForEstimates()
	prob := &contracts.ProbabilityEstimateDTO{Probability: 1.0}

	out, err := m.ProcessWithEstimates(context.Background(), in, prob, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ProbabilityUsed != 1.0 {
		t.Errorf("Probability=1.0 must be used exactly; got %v", out.ProbabilityUsed)
	}
}

// TestProcessWithEstimates_NegativeBoundary_FallbackTagged verifies F-SEC-01:
// when RejectOutOfRange is not configured, an out-of-range Probability falls
// back to the prior AND emits prob_boundary_value in FallbackReasons. No
// silent substitution path remains.
func TestProcessWithEstimates_NegativeBoundary_FallbackTagged(t *testing.T) {
	m := New(acceptEstimateCfg()) // no probCfg attached; RejectOutOfRange is implicitly false
	in := edgeForEstimates()
	prob := &contracts.ProbabilityEstimateDTO{Probability: -0.1}

	out, err := m.ProcessWithEstimates(context.Background(), in, prob, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ProbabilityUsed != acceptEstimateCfg().PriorProbability {
		t.Errorf("OOB probability must fall back to prior; got %v", out.ProbabilityUsed)
	}
	found := false
	for _, r := range out.FallbackReasons {
		if r == "prob_boundary_value" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected prob_boundary_value in FallbackReasons; got %v", out.FallbackReasons)
	}
}

// TestProcessWithEstimates_NaNBoundary_FallbackTagged verifies F-SEC-01:
// NaN Probability with RejectOutOfRange=false falls back + emits the tag.
func TestProcessWithEstimates_NaNBoundary_FallbackTagged(t *testing.T) {
	m := New(acceptEstimateCfg())
	in := edgeForEstimates()
	prob := &contracts.ProbabilityEstimateDTO{Probability: math.NaN()}

	out, err := m.ProcessWithEstimates(context.Background(), in, prob, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ProbabilityUsed != acceptEstimateCfg().PriorProbability {
		t.Errorf("NaN probability must fall back to prior; got %v", out.ProbabilityUsed)
	}
	found := false
	for _, r := range out.FallbackReasons {
		if r == "prob_boundary_value" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected prob_boundary_value in FallbackReasons; got %v", out.FallbackReasons)
	}
}

// TestProcessWithEstimatesAt_DeterministicTimestamps verifies F-SEC-04: two
// calls with the same inputs and the same `now` produce identical
// ExpiresAt and ValidatedAt. This is the replay-bit-for-bit guarantee.
func TestProcessWithEstimatesAt_DeterministicTimestamps(t *testing.T) {
	m := New(acceptEstimateCfg())
	in := edgeForEstimates()
	now := time.Date(2026, 1, 2, 3, 4, 5, 600_000_000, time.UTC)

	out1, err := m.ProcessWithEstimatesAt(context.Background(), in, nil, nil, nil, 0, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out2, err := m.ProcessWithEstimatesAt(context.Background(), in, nil, nil, nil, 0, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out1.ValidatedAt != out2.ValidatedAt {
		t.Errorf("ValidatedAt non-deterministic under fixed `now`: %q vs %q", out1.ValidatedAt, out2.ValidatedAt)
	}
	if out1.ExpiresAt != out2.ExpiresAt {
		t.Errorf("ExpiresAt non-deterministic under fixed `now`: %q vs %q", out1.ExpiresAt, out2.ExpiresAt)
	}
	// Sanity: ValidatedAt should reflect the injected `now` exactly.
	wantValidatedAt := now.Format(time.RFC3339Nano)
	if out1.ValidatedAt != wantValidatedAt {
		t.Errorf("ValidatedAt did not honour injected now: want %q got %q", wantValidatedAt, out1.ValidatedAt)
	}
}

// borderlineEstimateCfg yields EV ≈ 30 bps with default priors (p=0.5).
func borderlineEstimateCfg() *config.ValidationConfig {
	return &config.ValidationConfig{
		PriorProbability: 0.5,
		PriorGainBps:     200,
		PriorLossBps:     100,
		PriorSlippageBps: 10,
		EvThresholdBps:   100,
		FixedCostsBps:    10,
		BuildSubmitP95Ms: 500,
		TTLSeconds:       5,
	}
}

// TestProcessWithEstimatesAt_ModeThreshold_ExplorationAcceptsBorderlineEV verifies
// a per-call exploration threshold (30 bps) accepts an edge rejected at 100 bps.
func TestProcessWithEstimatesAt_ModeThreshold_ExplorationAcceptsBorderlineEV(t *testing.T) {
	m := New(borderlineEstimateCfg())
	in := edgeForEstimates()

	outStrict, err := m.ProcessWithEstimatesAt(context.Background(), in, nil, nil, nil, 100, time.Now().UTC())
	if err != nil {
		t.Fatalf("strict threshold call: %v", err)
	}
	if outStrict.Decision != "REJECT" {
		t.Fatalf("expected REJECT at threshold=100, got %q", outStrict.Decision)
	}
	if outStrict.EvThresholdApplied != 100 {
		t.Fatalf("EvThresholdApplied: want 100, got %d", outStrict.EvThresholdApplied)
	}

	outExploration, err := m.ProcessWithEstimatesAt(context.Background(), in, nil, nil, nil, 30, time.Now().UTC())
	if err != nil {
		t.Fatalf("exploration threshold call: %v", err)
	}
	if outExploration.Decision != "ACCEPT" {
		t.Fatalf("expected ACCEPT at threshold=30, got %q (%s)", outExploration.Decision, outExploration.RejectReason)
	}
	if outExploration.EvThresholdApplied != 30 {
		t.Fatalf("EvThresholdApplied: want 30, got %d", outExploration.EvThresholdApplied)
	}
}

// TestProcessWithEstimatesAt_ModeThresholdZero_FallsBackToConfig ensures zero
// uses ValidationConfig.EvThresholdBps (legacy/test path).
func TestProcessWithEstimatesAt_ModeThresholdZero_FallsBackToConfig(t *testing.T) {
	cfg := borderlineEstimateCfg()
	cfg.EvThresholdBps = 25
	m := New(cfg)
	in := edgeForEstimates()

	out, err := m.ProcessWithEstimatesAt(context.Background(), in, nil, nil, nil, 0, time.Now().UTC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.EvThresholdApplied != 25 {
		t.Fatalf("EvThresholdApplied: want 25 from config fallback, got %d", out.EvThresholdApplied)
	}
	if out.Decision != "ACCEPT" {
		t.Fatalf("expected ACCEPT with threshold 25, got %q", out.Decision)
	}
}
