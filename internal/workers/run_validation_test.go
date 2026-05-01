package workers

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
)

// validationStubAdapter extends stubAdapter with controllable join behaviour
// so the bounded-wait fix in fetchEstimates can be exercised deterministically.
//
// probReadyAfter / slipReadyAfter simulate the producer commit latency on the
// bus — the join lookup returns nil/ErrNotFound until the configured deadline
// elapses, then returns the staged DTO. probReads / slipReads count the
// number of bus polls so we can assert the bounded poll loop actually loops.
type validationStubAdapter struct {
	stubAdapter

	prob           *contracts.ProbabilityEstimateDTO
	slip           *contracts.SlippageEstimateDTO
	probReady      time.Time
	slipReady      time.Time
	probReads      atomic.Int64
	slipReads      atomic.Int64
	combinedReads  atomic.Int64
	insertedVedge  atomic.Pointer[contracts.ValidatedEdgeDTO]
	transitionLast atomic.Pointer[database.TransitionRequest]
}

func (s *validationStubAdapter) GetProbabilityEstimateByTrace(_ context.Context, _ string) (*contracts.ProbabilityEstimateDTO, error) {
	s.probReads.Add(1)
	if s.prob != nil && !time.Now().Before(s.probReady) {
		return s.prob, nil
	}
	return nil, database.ErrNotFound
}

func (s *validationStubAdapter) GetRealizedFillSamples(_ context.Context, _ int) (map[string][]database.FillSample, error) {
	return nil, nil
}
func (s *validationStubAdapter) UpsertSlippageAlpha(_ context.Context, _ string, _, _, _ float64, _ int) error {
	return nil
}
func (s *validationStubAdapter) GetSlippageAlpha(_ context.Context, _ string) (float64, error) {
	return 1.0, nil
}
func (s *validationStubAdapter) GetSlippageEstimateByTrace(_ context.Context, _ string) (*contracts.SlippageEstimateDTO, error) {
	s.slipReads.Add(1)
	if s.slip != nil && !time.Now().Before(s.slipReady) {
		return s.slip, nil
	}
	return nil, database.ErrNotFound
}

// GetEstimatesByTrace is the F-SEC-05 combined join. The validation worker
// uses ONLY this method on the hot path; the per-side counters above remain
// available for tests that exercise the legacy interface methods directly.
func (s *validationStubAdapter) GetEstimatesByTrace(ctx context.Context, traceID string) (*contracts.ProbabilityEstimateDTO, *contracts.SlippageEstimateDTO, error) {
	s.combinedReads.Add(1)
	var p *contracts.ProbabilityEstimateDTO
	var sl *contracts.SlippageEstimateDTO
	if s.prob != nil && !time.Now().Before(s.probReady) {
		p = s.prob
	}
	if s.slip != nil && !time.Now().Before(s.slipReady) {
		sl = s.slip
	}
	return p, sl, nil
}

func (s *validationStubAdapter) InsertValidatedEdge(_ context.Context, dto contracts.ValidatedEdgeDTO) error {
	s.insertedVedge.Store(&dto)
	return nil
}

func (s *validationStubAdapter) GetLifecycle(_ context.Context, _ string) (*database.Lifecycle, error) {
	return &database.Lifecycle{TokenLifecycleID: "lc-1", CurrentState: "EDGE_DETECTED", StateVersion: 1}, nil
}

func (s *validationStubAdapter) TransitionState(_ context.Context, req database.TransitionRequest) error {
	s.transitionLast.Store(&req)
	return nil
}

// validationCfgWithJoin builds a Config wired for the bounded-join path:
// probability model is enabled (production mode) and the join window is
// generous enough for the producer-commit-before-timeout case.
func validationCfgWithJoin(joinTimeoutMs, joinPollMs int) *config.Config {
	c := minConfig()
	c.Validation.JoinTimeoutMs = joinTimeoutMs
	c.Validation.JoinPollIntervalMs = joinPollMs
	c.ProbabilityRuntime = config.ProbabilityRuntimeConfig{
		UseModelOutput:    true,
		PriorProbability:  0.35,
		ProbJoinTimeoutMs: joinTimeoutMs,
		RejectOutOfRange:  true,
		RejectNanOrInf:    true,
	}
	return c
}

func makeEdgeEvent(t *testing.T, edge contracts.EdgeDTO) *database.Event {
	t.Helper()
	payload, err := json.Marshal(edge)
	if err != nil {
		t.Fatalf("marshal edge: %v", err)
	}
	return &database.Event{
		EventID:       "edge-evt-1",
		EventType:     "edge_event",
		Payload:       payload,
		TraceID:       edge.TraceID,
		CorrelationID: "corr-1",
		VersionID:     edge.VersionID,
	}
}

func acceptableEdge() contracts.EdgeDTO {
	return contracts.EdgeDTO{
		EventID:             "edge-1",
		TraceID:             "trace-validation-1",
		VersionID:           "v1",
		TokenLifecycleID:    "lc-1",
		TokenAddress:        "0xTOKEN",
		EdgeType:            "NEW_LAUNCH_EDGE",
		OpportunityWindowMs: 10_000,
	}
}

// ── 1. Producer commits within the join window — no spurious REJECT ──────────

// TestValidationWorker_ProducerCommitsWithinTimeout_UsesModelProbability
// proves the regression is fixed: when the probability worker commits its
// row before the bounded join window elapses, validation observes the real
// probability (no silent prior substitution) and the emitted DTO carries
// neither prob_join_timeout nor probability_unavailable.
func TestValidationWorker_ProducerCommitsWithinTimeout_UsesModelProbability(t *testing.T) {
	ad := &validationStubAdapter{
		prob: &contracts.ProbabilityEstimateDTO{
			Probability: 0.97005,
			Calibration: 0.9,
		},
		slip:      &contracts.SlippageEstimateDTO{ExpectedP95Bps: 100},
		probReady: time.Now().Add(40 * time.Millisecond),
		slipReady: time.Now().Add(20 * time.Millisecond),
	}

	cfg := validationCfgWithJoin(500, 5)
	w := NewValidationWorker(ad, cfg, nil)

	out, err := w.Process(context.Background(), makeEdgeEvent(t, acceptableEdge()))
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	vedge := ad.insertedVedge.Load()
	if vedge == nil {
		t.Fatalf("expected ValidatedEdgeDTO to be persisted")
	}

	// Probability used in EV must equal the producer's emitted value, NOT
	// the configured prior (0.35) — that was the production bug.
	if vedge.ProbabilityUsed != 0.97005 {
		t.Fatalf("ProbabilityUsed=%v; expected 0.97005 (producer value, not prior)", vedge.ProbabilityUsed)
	}

	// reject_reason must NOT carry the legacy prob_join_timeout fallback tag.
	if strings.Contains(vedge.RejectReason, "prob_join_timeout") {
		t.Fatalf("RejectReason must not carry prob_join_timeout; got %q", vedge.RejectReason)
	}
	if strings.Contains(vedge.RejectReason, "probability_unavailable") {
		t.Fatalf("RejectReason must not carry probability_unavailable when join succeeds; got %q", vedge.RejectReason)
	}

	// The bounded poll loop must have actually polled more than once
	// (the producer commit was staged 40ms in the future and pollInterval=5ms).
	// F-SEC-05: the hot path now uses the combined GetEstimatesByTrace.
	if ad.combinedReads.Load() < 2 {
		t.Fatalf("expected combined join to poll multiple times; got %d", ad.combinedReads.Load())
	}

	// EV must be ACCEPT (high probability, low slippage, gain >> loss).
	if vedge.Decision != "ACCEPT" {
		t.Fatalf("expected ACCEPT for valid high-probability edge; got %s reason=%q ev=%d",
			vedge.Decision, vedge.RejectReason, vedge.ExpectedValueBps)
	}

	// Worker emitted a downstream validated_edge_event — needed so
	// Layers 6–10 receive flow. Regression manifests as nil here.
	if out == nil {
		t.Fatalf("expected validated_edge_event to be emitted on ACCEPT")
	}
}

// ── 2. Producer never commits — REJECT with probability_unavailable ──────────

// TestValidationWorker_ProducerNeverCommits_RejectsProbabilityUnavailable
// proves that when the bounded join window elapses without a probability
// row, the worker emits an explicit REJECT with the new reject_reason.
// The legacy "prob_join_timeout" tag must not appear; the emitted DTO
// must not carry the prior in ProbabilityUsed.
func TestValidationWorker_ProducerNeverCommits_RejectsProbabilityUnavailable(t *testing.T) {
	ad := &validationStubAdapter{
		// nil prob → never available, regardless of clock
		slip:      &contracts.SlippageEstimateDTO{ExpectedP95Bps: 100},
		slipReady: time.Now().Add(5 * time.Millisecond),
	}

	// Tight bounded wait so the test runs fast.
	cfg := validationCfgWithJoin(60, 10)
	w := NewValidationWorker(ad, cfg, nil)

	if _, err := w.Process(context.Background(), makeEdgeEvent(t, acceptableEdge())); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	vedge := ad.insertedVedge.Load()
	if vedge == nil {
		t.Fatalf("expected ValidatedEdgeDTO to be persisted on REJECT")
	}

	if vedge.Decision != "REJECT" {
		t.Fatalf("expected REJECT on probability join timeout; got %s", vedge.Decision)
	}
	if !strings.Contains(vedge.RejectReason, "probability_unavailable") {
		t.Fatalf("expected reject_reason=probability_unavailable; got %q", vedge.RejectReason)
	}
	if strings.Contains(vedge.RejectReason, "prob_join_timeout") {
		t.Fatalf("legacy prob_join_timeout tag must not be emitted; got %q", vedge.RejectReason)
	}

	// ProbabilityUsed must be 0 — no silent prior substitution into EV.
	if vedge.ProbabilityUsed != 0 {
		t.Fatalf("ProbabilityUsed must be 0 on production-mode join failure; got %v", vedge.ProbabilityUsed)
	}

	// Lifecycle must transition EDGE_DETECTED → REJECTED.
	tr := ad.transitionLast.Load()
	if tr == nil || tr.NewState != "REJECTED" {
		t.Fatalf("expected REJECTED transition; got %+v", tr)
	}
}

// ── 3. F-SEC-05: combined join halves DB round-trips ─────────────────────────

// TestValidation_CombinedJoin_HalvesRoundtrips proves the validation worker
// uses the new GetEstimatesByTrace adapter method instead of the two
// per-side calls — a single combined call per poll iteration.
func TestValidation_CombinedJoin_HalvesRoundtrips(t *testing.T) {
	ad := &validationStubAdapter{
		prob: &contracts.ProbabilityEstimateDTO{
			Probability: 0.97,
			Confidence:  0.9,
		},
		slip: &contracts.SlippageEstimateDTO{ExpectedP95Bps: 100},
		// both ready immediately → exactly one poll
	}

	cfg := validationCfgWithJoin(500, 5)
	w := NewValidationWorker(ad, cfg, nil)

	if _, err := w.Process(context.Background(), makeEdgeEvent(t, acceptableEdge())); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	// Combined call hit exactly once (both rows ready on first poll).
	if got := ad.combinedReads.Load(); got != 1 {
		t.Fatalf("expected 1 combined adapter call; got %d", got)
	}
	// Per-side calls MUST NOT happen on the hot path anymore.
	if got := ad.probReads.Load(); got != 0 {
		t.Fatalf("expected 0 per-side prob reads on hot path; got %d", got)
	}
	if got := ad.slipReads.Load(); got != 0 {
		t.Fatalf("expected 0 per-side slip reads on hot path; got %d", got)
	}
}
