package selection

import (
	"context"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

func defaultSelCfg() *config.SelectionConfig {
	return &config.SelectionConfig{MaxOpenPositions: 1}
}

func acceptedEdge() contracts.ValidatedEdgeDTO {
	return contracts.ValidatedEdgeDTO{
		EventID:          "val-1",
		TraceID:          "trace-1",
		CorrelationID:    "corr-1",
		VersionID:        "v1",
		TokenLifecycleID: "lc-1",
		TokenAddress:     "0xTOKEN",
		Decision:         "ACCEPT",
		ExpectedValueBps: 50,
		ProbabilityUsed:  0.6,
		RejectReason:     "",
		ValidatedAt:      "2026-01-01T00:00:00Z",
	}
}

// ── New ──────────────────────────────────────────────────────────────────────

func TestNew_NilConfig_UsesDefaults(t *testing.T) {
	m := New(nil)
	if m == nil {
		t.Fatal("New returned nil")
	}
	if m.cfg.MaxOpenPositions == 0 {
		t.Error("expected non-zero MaxOpenPositions default")
	}
}

// ── Process: selected ─────────────────────────────────────────────────────────

func TestProcess_AcceptedEdge_NoOpenPositions_Selected(t *testing.T) {
	// Arrange
	m := New(defaultSelCfg())
	in := acceptedEdge()

	// Act
	out, err := m.Process(context.Background(), in, 0)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.Selected {
		t.Error("expected Selected=true")
	}
	if out.RejectReason != "" {
		t.Errorf("expected empty RejectReason for selected trade: %q", out.RejectReason)
	}
}

func TestProcess_Selected_TraceFieldsPropagated(t *testing.T) {
	m := New(defaultSelCfg())
	in := acceptedEdge()

	out, err := m.Process(context.Background(), in, 0)
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

func TestProcess_Selected_CombinedScoreCalculated(t *testing.T) {
	m := New(defaultSelCfg())
	in := acceptedEdge()
	// CombinedScore = ProbabilityUsed * ExpectedValueBps / 1000 = 0.6 * 50 / 1000 = 0.03
	out, err := m.Process(context.Background(), in, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := 0.6 * 50.0 / 1000.0
	if out.CombinedScore != expected {
		t.Errorf("expected CombinedScore=%f, got %f", expected, out.CombinedScore)
	}
}

func TestProcess_Selected_SelectedAtFromValidatedAt(t *testing.T) {
	m := New(defaultSelCfg())
	in := acceptedEdge()

	out, err := m.Process(context.Background(), in, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.SelectedAt != in.ValidatedAt {
		t.Errorf("SelectedAt should equal ValidatedAt: got %q", out.SelectedAt)
	}
}

func TestProcess_EventIDDeterministic(t *testing.T) {
	m := New(defaultSelCfg())
	in := acceptedEdge()

	out1, _ := m.Process(context.Background(), in, 0)
	out2, _ := m.Process(context.Background(), in, 0)

	if out1.EventID != out2.EventID {
		t.Errorf("EventID not deterministic: %q vs %q", out1.EventID, out2.EventID)
	}
}

// ── Process: rejected ─────────────────────────────────────────────────────────

func TestProcess_MaxOpenPositions_Rejected(t *testing.T) {
	m := New(defaultSelCfg()) // MaxOpenPositions=1
	in := acceptedEdge()

	out, err := m.Process(context.Background(), in, 1) // openCount = MaxOpenPositions
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Selected {
		t.Error("expected Selected=false when max positions reached")
	}
	if out.CombinedScore != 0 {
		t.Errorf("expected CombinedScore=0 for rejected trade, got %f", out.CombinedScore)
	}
	if out.RejectReason == "" {
		t.Error("expected non-empty RejectReason")
	}
}

func TestProcess_EdgeNotValidated_Rejected(t *testing.T) {
	m := New(defaultSelCfg())
	in := acceptedEdge()
	in.Decision = "REJECT"
	in.RejectReason = "ev_below_threshold"

	out, err := m.Process(context.Background(), in, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Selected {
		t.Error("expected Selected=false for REJECT decision")
	}
	if out.RejectReason == "" {
		t.Error("expected non-empty RejectReason for rejected edge")
	}
}

func TestProcessBatch_TopKSelectsHighestAmongCandidates(t *testing.T) {
	m := New(&config.SelectionConfig{MaxOpenPositions: 10, TopK: 1})
	thresholds := config.ModeThresholds{MaxPositions: 10, ExploreBudgetPct: 0}
	items := []BatchItem{
		edge("val-low", "token-z", 30, 0.5),
		edge("val-high", "token-a", 100, 0.8),
	}
	outs, err := m.ProcessBatch(context.Background(), items, 0, thresholds, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(outs))
	}
	byID := map[string]contracts.SelectionOutputDTO{}
	for _, o := range outs {
		byID[o.CausationID] = o
	}
	if !byID["val-high"].Selected {
		t.Fatal("expected val-high selected")
	}
	if byID["val-low"].Selected || byID["val-low"].RejectReason != RejectReasonBelowTopK {
		t.Fatalf("expected val-low below_top_k, got %+v", byID["val-low"])
	}
}

func TestProcessBatch_ExplorationModeMarksLowestPick(t *testing.T) {
	m := New(&config.SelectionConfig{MaxOpenPositions: 10, TopK: 3})
	thresholds := config.ModeThresholds{MaxPositions: 10, ExploreBudgetPct: 5.0}
	items := []BatchItem{
		edge("v1", "t-a", 100, 0.9),
		edge("v2", "t-b", 90, 0.8),
		edge("v3", "t-c", 80, 0.7),
	}
	outs, err := m.ProcessBatch(context.Background(), items, 0, thresholds, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var explore int
	for _, o := range outs {
		if o.Selected && o.IsExploration {
			explore++
		}
	}
	if explore != 1 {
		t.Fatalf("expected 1 exploration pick, got %d", explore)
	}
}

func TestProcess_RejectedEdge_EventIDDiffFromSelected(t *testing.T) {
	// EventID encodes selected=true vs false, so they differ.
	m := New(defaultSelCfg())
	inAccepted := acceptedEdge()
	inRejected := acceptedEdge()
	inRejected.Decision = "REJECT"

	outA, _ := m.Process(context.Background(), inAccepted, 0)
	outR, _ := m.Process(context.Background(), inRejected, 0)

	if outA.EventID == outR.EventID {
		t.Error("EventID for selected and rejected should differ")
	}
}
