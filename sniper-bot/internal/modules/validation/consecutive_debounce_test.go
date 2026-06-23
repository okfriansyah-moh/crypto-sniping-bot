package validation

import (
	"context"
	"strings"
	"testing"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

func validatedCfg(required int32, windowSec int32) *config.ValidationConfig {
	return &config.ValidationConfig{
		PriorProbability:             0.6,
		PriorGainBps:                 1000,
		PriorLossBps:                 500,
		PriorSlippageBps:             50,
		EvThresholdBps:               10,
		FixedCostsBps:                50,
		BuildSubmitP95Ms:             100,
		TTLSeconds:                   5,
		RequiredConsecutivePasses:    required,
		ConsecutivePassWindowSeconds: windowSec,
	}
}

func passingEdge() contracts.EdgeDTO {
	return contracts.EdgeDTO{
		EventID:             "edge-1",
		TraceID:             "tr",
		VersionID:           "v",
		TokenLifecycleID:    "tl",
		TokenAddress:        "0xt",
		EdgeType:            "NEW_LAUNCH_EDGE",
		OpportunityWindowMs: 5000,
	}
}

// TestDebounce_PendingThenAccept: 3 consecutive passes are required;
// only the 3rd evaluation emits ACCEPT.
func TestDebounce_PendingThenAccept(t *testing.T) {
	m := New(validatedCfg(3, 60))
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	prior := PriorPassState{}
	e := passingEdge()

	out1, _ := m.ProcessWithDebounce(context.Background(), e, prior, t0)
	if out1.Decision != "REJECT" || !strings.HasPrefix(out1.RejectReason, "consecutive_pass_pending") {
		t.Fatalf("call 1: decision=%q reason=%q (want pending)", out1.Decision, out1.RejectReason)
	}
	if out1.ConsecutivePassCount != 1 {
		t.Errorf("call 1: count=%d want 1", out1.ConsecutivePassCount)
	}

	prior = PriorPassState{Count: out1.ConsecutivePassCount, WindowStart: out1.ConsecutivePassWindowStart}
	out2, _ := m.ProcessWithDebounce(context.Background(), e, prior, t0.Add(10*time.Second))
	if out2.Decision != "REJECT" || out2.ConsecutivePassCount != 2 {
		t.Fatalf("call 2: decision=%q count=%d", out2.Decision, out2.ConsecutivePassCount)
	}

	prior = PriorPassState{Count: out2.ConsecutivePassCount, WindowStart: out2.ConsecutivePassWindowStart}
	out3, _ := m.ProcessWithDebounce(context.Background(), e, prior, t0.Add(20*time.Second))
	if out3.Decision != "ACCEPT" {
		t.Fatalf("call 3: decision=%q (want ACCEPT) reason=%q", out3.Decision, out3.RejectReason)
	}
	if out3.ConsecutivePassCount != 3 {
		t.Errorf("call 3: count=%d want 3", out3.ConsecutivePassCount)
	}
}

// TestDebounce_RejectClearsCounter: a base REJECT zeroes the counter.
func TestDebounce_RejectClearsCounter(t *testing.T) {
	m := New(validatedCfg(3, 60))
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	rejectEdge := passingEdge()
	rejectEdge.EdgeType = "" // produces base REJECT (no_edge_detected)

	prior := PriorPassState{Count: 2, WindowStart: t0.Add(-10 * time.Second).Format(time.RFC3339Nano)}
	out, _ := m.ProcessWithDebounce(context.Background(), rejectEdge, prior, t0)
	if out.Decision != "REJECT" {
		t.Fatalf("decision=%q want REJECT", out.Decision)
	}
	if out.ConsecutivePassCount != 0 || out.ConsecutivePassWindowStart != "" {
		t.Errorf("counters not cleared: count=%d windowStart=%q",
			out.ConsecutivePassCount, out.ConsecutivePassWindowStart)
	}
}

// TestDebounce_WindowExpiry: a pass after the window expires resets to 1.
func TestDebounce_WindowExpiry(t *testing.T) {
	m := New(validatedCfg(3, 60)) // 60s window
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	prior := PriorPassState{Count: 2, WindowStart: t0.Format(time.RFC3339Nano)}
	// Evaluate 70s later — window expired.
	out, _ := m.ProcessWithDebounce(context.Background(), passingEdge(), prior, t0.Add(70*time.Second))
	if out.ConsecutivePassCount != 1 {
		t.Errorf("expected reset to 1, got %d", out.ConsecutivePassCount)
	}
	if out.Decision != "REJECT" {
		t.Errorf("decision=%q want REJECT (still pending after reset)", out.Decision)
	}
}

// TestDebounce_DisabledWhenRequired1: required<=1 acts as a pass-through.
func TestDebounce_DisabledWhenRequired1(t *testing.T) {
	m := New(validatedCfg(1, 60))
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	out, _ := m.ProcessWithDebounce(context.Background(), passingEdge(), PriorPassState{}, t0)
	if out.Decision != "ACCEPT" {
		t.Fatalf("decision=%q want ACCEPT (gate disabled)", out.Decision)
	}
}
