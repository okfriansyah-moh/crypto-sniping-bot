package learning

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/ai"
)

// fakeAIForLoss satisfies ai.AIClient for loss explainer tests.
type fakeAIForLoss struct {
	resp *ai.CompletionResponse
	err  error
}

func (f *fakeAIForLoss) Complete(_ context.Context, _ *ai.CompletionRequest) (*ai.CompletionResponse, error) {
	return f.resp, f.err
}

func buildLossRecord(outcome, cls string, pnl float64) contracts.LearningRecordDTO {
	return contracts.LearningRecordDTO{
		RecordID:       "rec-001",
		Outcome:        outcome,
		Classification: cls,
		PnlPct:         pnl,
		Cohort:         "med_liq:0-30m:new_launch",
		EdgeSnapshot: contracts.EdgeDTO{
			EdgeStrength:   0.6,
			EdgeConfidence: 0.55,
		},
		ValidatedSnapshot: contracts.ValidatedEdgeDTO{
			ProbabilityUsed: 0.4,
		},
		FeaturesSnapshot: contracts.FeatureDTO{
			NarrativeScore: 4.0,
		},
	}
}

func TestLossExplainer_NilClient(t *testing.T) {
	e := NewLossExplainer(nil, slog.Default())
	r := buildLossRecord("SL", "FP", -45.0)
	out, err := e.Explain(context.Background(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.AIExplanationKnown {
		t.Error("should not set AIExplanationKnown when client is nil")
	}
}

func TestLossExplainer_TPPassThrough(t *testing.T) {
	fake := &fakeAIForLoss{resp: &ai.CompletionResponse{Content: "should not be called"}}
	e := NewLossExplainer(fake, slog.Default())
	r := buildLossRecord("TP", "TP", 80.0)

	out, err := e.Explain(context.Background(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.AIExplanationKnown {
		t.Error("TP records should not get AI explanation")
	}
}

func TestLossExplainer_AlreadyKnown(t *testing.T) {
	calls := 0
	fake := &fakeAIForLoss{resp: &ai.CompletionResponse{Content: `{"cat":"timing","why":"slow"}`}}
	_ = fake
	// Override to count calls.
	counting := &countingAI{calls: &calls, resp: `{"cat":"timing","why":"slow"}`}
	e := NewLossExplainer(counting, slog.Default())

	r := buildLossRecord("SL", "FP", -30.0)
	r.AIExplanationKnown = true
	r.AILossCategory = "timing"

	out, _ := e.Explain(context.Background(), r)
	if calls != 0 {
		t.Error("should not call AI when AIExplanationKnown=true")
	}
	if out.AILossCategory != "timing" {
		t.Errorf("category should be preserved: %q", out.AILossCategory)
	}
}

func TestLossExplainer_SuccessfulExplanation(t *testing.T) {
	fake := &fakeAIForLoss{
		resp: &ai.CompletionResponse{
			Content: `{"cat":"momentum_fade","why":"volume dried up 5 min after launch, typical pump-dump pattern"}`,
		},
	}
	e := NewLossExplainer(fake, slog.Default())
	r := buildLossRecord("SL", "FP", -55.0)

	out, err := e.Explain(context.Background(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.AIExplanationKnown {
		t.Error("AIExplanationKnown should be true on success")
	}
	if out.AILossCategory != "momentum_fade" {
		t.Errorf("want category=momentum_fade, got %q", out.AILossCategory)
	}
	if out.AIExplanation == "" {
		t.Error("AIExplanation should be non-empty")
	}
}

func TestLossExplainer_AIErrorFailOpen(t *testing.T) {
	fake := &fakeAIForLoss{err: errors.New("context deadline exceeded")}
	e := NewLossExplainer(fake, slog.Default())
	r := buildLossRecord("RUG", "FP", -90.0)

	out, err := e.Explain(context.Background(), r)
	if err != nil {
		t.Fatalf("should be fail-open: %v", err)
	}
	if out.AIExplanationKnown {
		t.Error("AIExplanationKnown must be false on AI error")
	}
}

func TestLossExplainer_InvalidCategoryFallsBackToUnknown(t *testing.T) {
	fake := &fakeAIForLoss{
		resp: &ai.CompletionResponse{
			Content: `{"cat":"hallucinated_category","why":"random words"}`,
		},
	}
	e := NewLossExplainer(fake, slog.Default())
	r := buildLossRecord("MISSED_PUMP", "FN", 0.0)

	out, _ := e.Explain(context.Background(), r)
	if out.AILossCategory != "unknown" {
		t.Errorf("invalid category should fall back to 'unknown', got %q", out.AILossCategory)
	}
}

func TestExtractLossJSON(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`{"cat":"timing"}`, `{"cat":"timing"}`},
		{"```\n{\"cat\":\"timing\"}\n```", `{"cat":"timing"}`},
		{"no json", ""},
	}
	for _, c := range cases {
		got := extractLossJSON(c.in)
		if got != c.want {
			t.Errorf("extractLossJSON(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// countingAI is a minimal ai.AIClient that counts calls.
type countingAI struct {
	calls *int
	resp  string
}

func (c *countingAI) Complete(_ context.Context, _ *ai.CompletionRequest) (*ai.CompletionResponse, error) {
	*c.calls++
	return &ai.CompletionResponse{Content: c.resp}, nil
}
