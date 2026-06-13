// Phase 9 (Profitability Restoration § 9.3) — validation guard tests.
package validation

import (
	"context"
	"math"
	"strings"
	"testing"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

func validationCfg() *config.ValidationConfig {
	return &config.ValidationConfig{
		PriorProbability: 0.55,
		PriorGainBps:     500, PriorLossBps: 300, PriorSlippageBps: 100,
		EvThresholdBps: 10, FixedCostsBps: 50,
		BuildSubmitP95Ms: 500, TTLSeconds: 5,
	}
}

func phase9ProbCfg() *config.ProbabilityRuntimeConfig {
	return &config.ProbabilityRuntimeConfig{
		UseModelOutput:     true,
		PriorProbability:   0.35,
		MinModelConfidence: 0.40,
		ProbJoinTimeoutMs:  200,
		RejectOutOfRange:   true,
		RejectNanOrInf:     true,
	}
}

func goodEdge() contracts.EdgeDTO {
	return contracts.EdgeDTO{
		EventID: "e1", TraceID: "t1", VersionID: "v1",
		EdgeType:            "NEW_LAUNCH_EDGE",
		OpportunityWindowMs: 10_000,
	}
}

func TestProcessWithEstimates_NaN_RejectsInvalid(t *testing.T) {
	mod := New(validationCfg()).WithProbabilityRuntime(phase9ProbCfg())
	prob := &contracts.ProbabilityEstimateDTO{Probability: math.NaN()}
	got, _ := mod.ProcessWithEstimates(context.Background(), goodEdge(), prob, nil, nil)
	if got.Decision != "REJECT" || !strings.Contains(got.RejectReason, "invalid_probability") {
		t.Fatalf("NaN must reject invalid_probability; got %s / %q", got.Decision, got.RejectReason)
	}
}

func TestProcessWithEstimates_OutOfRange_RejectsInvalid(t *testing.T) {
	mod := New(validationCfg()).WithProbabilityRuntime(phase9ProbCfg())
	prob := &contracts.ProbabilityEstimateDTO{Probability: 1.5}
	got, _ := mod.ProcessWithEstimates(context.Background(), goodEdge(), prob, nil, nil)
	if got.Decision != "REJECT" || !strings.Contains(got.RejectReason, "invalid_probability") {
		t.Fatalf("out-of-range must reject invalid_probability; got %s / %q", got.Decision, got.RejectReason)
	}
}

func TestProcessWithEstimates_LowConfidence_FallsBack(t *testing.T) {
	mod := New(validationCfg()).WithProbabilityRuntime(phase9ProbCfg())
	// Confidence=0.10 is below MinModelConfidence=0.40 → fallback to prior.
	// Calibration is a model-accuracy metric and must NOT be used as the confidence value.
	prob := &contracts.ProbabilityEstimateDTO{Probability: 0.7, Confidence: 0.10, Calibration: 0.2}
	got, _ := mod.ProcessWithEstimates(context.Background(), goodEdge(), prob, nil, nil)
	// low_model_confidence is a fallback signal, NOT a reject reason.
	// Per contracts/validated_edge.go RejectReason MUST be empty on ACCEPT;
	// the fallback is observable via ProbabilityUsed reverting to the prior.
	if got.Decision == "ACCEPT" && got.RejectReason != "" {
		t.Fatalf("RejectReason must be empty on ACCEPT; got %q", got.RejectReason)
	}
	// Probability used must be the prior, not 0.7 — this is the contract-
	// preserving way to observe fallback behavior.
	if math.Abs(got.ProbabilityUsed-0.55) > 1e-9 {
		t.Errorf("expected fallback to prior 0.55; got %v", got.ProbabilityUsed)
	}
}

func TestProcessWithEstimates_NilProb_RejectsProbabilityUnavailable(t *testing.T) {
	// Production mode (UseModelOutput=true): a missing probability estimate
	// MUST surface as an explicit REJECT with reject_reason="probability_unavailable".
	// The historical "silently substitute PriorProbability and tag prob_join_timeout"
	// path produced deterministic mass-rejects at large negative ev_bps and
	// starved Layers 6–10 — see docs/reference/architecture.md § 3.5.
	mod := New(validationCfg()).WithProbabilityRuntime(phase9ProbCfg())
	got, _ := mod.ProcessWithEstimates(context.Background(), goodEdge(), nil, nil, nil)

	if got.Decision != "REJECT" {
		t.Fatalf("missing prob in production mode must REJECT; got %s", got.Decision)
	}
	if !strings.Contains(got.RejectReason, "probability_unavailable") {
		t.Fatalf("expected probability_unavailable; got %q", got.RejectReason)
	}
	// The legacy fallback tag must NOT appear — operators distinguish a
	// real join failure from a low-confidence prior fallback by reason.
	if strings.Contains(got.RejectReason, "prob_join_timeout") {
		t.Fatalf("legacy prob_join_timeout tag must not be emitted; got %q", got.RejectReason)
	}
	// ProbabilityUsed must NOT carry the prior — that would imply the prior
	// fed EV and is exactly the silent-substitution we are rejecting.
	if got.ProbabilityUsed != 0 {
		t.Fatalf("ProbabilityUsed must be 0 (no model input); got %v", got.ProbabilityUsed)
	}
}

func TestProcessWithEstimates_GoodProb_Used(t *testing.T) {
	mod := New(validationCfg()).WithProbabilityRuntime(phase9ProbCfg())
	prob := &contracts.ProbabilityEstimateDTO{Probability: 0.8, Calibration: 0.7}
	got, _ := mod.ProcessWithEstimates(context.Background(), goodEdge(), prob, nil, nil)
	if math.Abs(got.ProbabilityUsed-0.8) > 1e-9 {
		t.Fatalf("good prob must be used; got %v", got.ProbabilityUsed)
	}
}
