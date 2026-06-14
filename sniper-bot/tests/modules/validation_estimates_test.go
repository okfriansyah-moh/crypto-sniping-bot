package modules_test

import (
	"context"
	"strings"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/sniper-bot/internal/modules/validation"
)

func validationCfgFixture() *config.ValidationConfig {
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

func TestProcessWithEstimates_AcceptsWhenAllEstimatesProvided(t *testing.T) {
	mod := validation.New(validationCfgFixture())

	prob := &contracts.ProbabilityEstimateDTO{Probability: 0.7}
	slip := &contracts.SlippageEstimateDTO{ExpectedP95Bps: 80}
	lat := &contracts.LatencyProfileDTO{ExpectedP95Ms: 400}

	out, err := mod.ProcessWithEstimates(context.Background(), edgeDTOFixture(), prob, slip, lat)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Decision != "ACCEPT" {
		t.Fatalf("expected ACCEPT, got %q (reason=%q)", out.Decision, out.RejectReason)
	}
	if out.ProbabilityUsed != 0.7 {
		t.Errorf("expected model probability 0.7, got %v", out.ProbabilityUsed)
	}
	if out.SlippageP95BpsUsed != 80 {
		t.Errorf("expected model slippage 80, got %d", out.SlippageP95BpsUsed)
	}
	if out.ExpectedLatencyMs != 400 {
		t.Errorf("expected model latency 400, got %d", out.ExpectedLatencyMs)
	}
	if !out.LatencyGatePassed {
		t.Error("latency gate should pass when p95 < window")
	}
}

func TestProcessWithEstimates_FallsBackToPriorsWhenNil(t *testing.T) {
	cfg := validationCfgFixture()
	mod := validation.New(cfg)

	out, err := mod.ProcessWithEstimates(context.Background(), edgeDTOFixture(), nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ProbabilityUsed != cfg.PriorProbability {
		t.Errorf("expected prior probability, got %v", out.ProbabilityUsed)
	}
	if out.SlippageP95BpsUsed != cfg.PriorSlippageBps {
		t.Errorf("expected prior slippage, got %d", out.SlippageP95BpsUsed)
	}
	if out.ExpectedLatencyMs != int32(cfg.BuildSubmitP95Ms) {
		t.Errorf("expected prior latency, got %d", out.ExpectedLatencyMs)
	}
}

func TestProcessWithEstimates_RejectsOutOfRangeProbability(t *testing.T) {
	cfg := validationCfgFixture()
	mod := validation.New(cfg)

	// F-SEC-01: 0.0 and 1.0 are boundary-inclusive — used exactly, no
	// silent prior substitution. (The legacy `> 0 && < 1` check that
	// produced fallback was the security finding.)
	probZero := &contracts.ProbabilityEstimateDTO{Probability: 0}
	out, _ := mod.ProcessWithEstimates(context.Background(), edgeDTOFixture(), probZero, nil, nil)
	if out.ProbabilityUsed != 0.0 {
		t.Errorf("p=0 must be honoured exactly, got %v", out.ProbabilityUsed)
	}

	probOne := &contracts.ProbabilityEstimateDTO{Probability: 1.0}
	out2, _ := mod.ProcessWithEstimates(context.Background(), edgeDTOFixture(), probOne, nil, nil)
	if out2.ProbabilityUsed != 1.0 {
		t.Errorf("p=1 must be honoured exactly, got %v", out2.ProbabilityUsed)
	}

	// Out-of-range values (here −0.5) MUST fall back to prior AND emit the
	// `prob_boundary_value` diagnostic in FallbackReasons.
	probOOB := &contracts.ProbabilityEstimateDTO{Probability: -0.5}
	out3, _ := mod.ProcessWithEstimates(context.Background(), edgeDTOFixture(), probOOB, nil, nil)
	if out3.ProbabilityUsed != cfg.PriorProbability {
		t.Errorf("OOB probability should fall back to prior, got %v", out3.ProbabilityUsed)
	}
	found := false
	for _, r := range out3.FallbackReasons {
		if r == "prob_boundary_value" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected prob_boundary_value tag; got %v", out3.FallbackReasons)
	}
}

func TestProcessWithEstimates_RejectsEmptyEdgeType(t *testing.T) {
	mod := validation.New(validationCfgFixture())
	in := edgeDTOFixture()
	in.EdgeType = ""
	out, _ := mod.ProcessWithEstimates(context.Background(), in, nil, nil, nil)
	if out.Decision != "REJECT" {
		t.Fatalf("expected REJECT, got %q", out.Decision)
	}
	if out.RejectReason != "no_edge_detected" {
		t.Errorf("expected no_edge_detected, got %q", out.RejectReason)
	}
	if out.LatencyGatePassed {
		t.Error("latency gate must be false on reject")
	}
}

func TestProcessWithEstimates_RejectsEvBelowThreshold(t *testing.T) {
	cfg := validationCfgFixture()
	cfg.EvThresholdBps = 10_000 // unreachable
	mod := validation.New(cfg)
	out, _ := mod.ProcessWithEstimates(context.Background(), edgeDTOFixture(), nil, nil, nil)
	if out.Decision != "REJECT" {
		t.Fatalf("expected REJECT, got %q", out.Decision)
	}
	if !strings.HasPrefix(out.RejectReason, "ev_below_threshold") {
		t.Errorf("expected ev_below_threshold reason, got %q", out.RejectReason)
	}
}

func TestProcessWithEstimates_RejectsLatencyExceedsWindow(t *testing.T) {
	cfg := validationCfgFixture()
	// Ensure EV passes so latency is the sole reject reason.
	cfg.PriorGainBps = 2_000
	cfg.PriorLossBps = 100
	mod := validation.New(cfg)
	in := edgeDTOFixture()
	in.OpportunityWindowMs = 100
	lat := &contracts.LatencyProfileDTO{ExpectedP95Ms: 5_000}

	out, _ := mod.ProcessWithEstimates(context.Background(), in, nil, nil, lat)
	if out.Decision != "REJECT" {
		t.Fatalf("expected REJECT, got %q", out.Decision)
	}
	if out.RejectReason != "latency_exceeds_window" {
		t.Errorf("expected latency_exceeds_window, got %q", out.RejectReason)
	}
	if out.LatencyGatePassed {
		t.Error("latency gate must be false")
	}
}

func TestProcessWithEstimates_DeterministicEventID(t *testing.T) {
	mod := validation.New(validationCfgFixture())
	in := edgeDTOFixture()
	prob := &contracts.ProbabilityEstimateDTO{Probability: 0.6}

	a, _ := mod.ProcessWithEstimates(context.Background(), in, prob, nil, nil)
	b, _ := mod.ProcessWithEstimates(context.Background(), in, prob, nil, nil)
	if a.EventID == "" {
		t.Fatal("event id must not be empty")
	}
	if a.EventID != b.EventID {
		t.Errorf("event id must be deterministic: %q vs %q", a.EventID, b.EventID)
	}
	if a.Decision != b.Decision {
		t.Errorf("decision must be deterministic: %q vs %q", a.Decision, b.Decision)
	}
}

func TestProcessWithEstimates_PreservesTraceFields(t *testing.T) {
	mod := validation.New(validationCfgFixture())
	in := edgeDTOFixture()
	out, _ := mod.ProcessWithEstimates(context.Background(), in, nil, nil, nil)

	if out.TraceID != in.TraceID {
		t.Errorf("trace id mismatch")
	}
	if out.CorrelationID != in.CorrelationID {
		t.Errorf("correlation id mismatch")
	}
	if out.CausationID != in.EventID {
		t.Errorf("causation id should be parent event id")
	}
	if out.VersionID != in.VersionID {
		t.Errorf("version id must propagate")
	}
	if out.TokenAddress != in.TokenAddress {
		t.Errorf("token address must propagate")
	}
}
