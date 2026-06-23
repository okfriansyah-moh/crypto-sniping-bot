package probes

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/sniper-bot/internal/ai"
)

// fakeAIClient records calls and returns canned responses.
type fakeAIClient struct {
	response *ai.CompletionResponse
	err      error
	calls    int
}

func (f *fakeAIClient) Complete(_ context.Context, _ *ai.CompletionRequest) (*ai.CompletionResponse, error) {
	f.calls++
	return f.response, f.err
}

func buildDTO(name, sym, desc string) contracts.MarketDataDTO {
	return contracts.MarketDataDTO{
		TokenAddress:        "mint1111",
		Name:                name,
		Symbol:              sym,
		MetadataDescription: desc,
	}
}

func TestAINarrativeProbe_Disabled(t *testing.T) {
	p := NewAINarrativeProbe(nil, AINarrativeConfig{Enabled: false}, slog.Default())
	in := buildDTO("RIBBIT", "RIBB", "a frog meme coin")
	out, err := p.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.NarrativeKnown {
		t.Error("NarrativeKnown should be false when probe is disabled")
	}
}

func TestAINarrativeProbe_AlreadyKnown(t *testing.T) {
	fake := &fakeAIClient{}
	p := NewAINarrativeProbe(fake, AINarrativeConfig{Enabled: true}, slog.Default())
	in := buildDTO("RIBBIT", "RIBB", "frog")
	in.NarrativeKnown = true
	in.NarrativeScore = 7.5

	out, err := p.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.calls != 0 {
		t.Error("should not call AI when NarrativeKnown=true")
	}
	if out.NarrativeScore != 7.5 {
		t.Errorf("NarrativeScore changed unexpectedly: %v", out.NarrativeScore)
	}
}

func TestAINarrativeProbe_EmptyDescription_NeutralDefaults(t *testing.T) {
	fake := &fakeAIClient{}
	p := NewAINarrativeProbe(fake, AINarrativeConfig{Enabled: true}, slog.Default())
	in := buildDTO("TOKEN", "TKN", "")

	out, err := p.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.calls != 0 {
		t.Error("should not call AI for empty description")
	}
	if !out.NarrativeKnown {
		t.Error("NarrativeKnown should be true for empty description")
	}
	if out.NarrativeScore != 5.0 {
		t.Errorf("expected neutral NarrativeScore=5.0, got %v", out.NarrativeScore)
	}
	if out.NarrativeType != "generic" {
		t.Errorf("expected NarrativeType=generic, got %q", out.NarrativeType)
	}
}

func TestAINarrativeProbe_SuccessfulScore(t *testing.T) {
	fake := &fakeAIClient{
		response: &ai.CompletionResponse{
			Content: `{"ns":8.5,"sp":1.0,"cp":false,"imp":false,"nt":"ai","r":"strong AI agent narrative"}`,
		},
	}
	p := NewAINarrativeProbe(fake, AINarrativeConfig{
		Enabled:             true,
		MaxDescriptionChars: 300,
		TrendingNarratives:  []string{"AI agents", "DePIN"},
	}, slog.Default())
	in := buildDTO("AGENTBOT", "ABOT", "AI-powered autonomous trading agent on Solana")

	out, err := p.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.NarrativeKnown {
		t.Error("NarrativeKnown should be true on success")
	}
	if out.NarrativeScore != 8.5 {
		t.Errorf("NarrativeScore: want 8.5, got %v", out.NarrativeScore)
	}
	if out.ScamProbabilityScore != 1.0 {
		t.Errorf("ScamProbabilityScore: want 1.0, got %v", out.ScamProbabilityScore)
	}
	if out.NarrativeType != "ai" {
		t.Errorf("NarrativeType: want ai, got %q", out.NarrativeType)
	}
	if out.NarrativeReason != "strong AI agent narrative" {
		t.Errorf("NarrativeReason: %q", out.NarrativeReason)
	}
}

func TestAINarrativeProbe_MarkdownFences(t *testing.T) {
	fake := &fakeAIClient{
		response: &ai.CompletionResponse{
			Content: "```json\n{\"ns\":6,\"sp\":2,\"cp\":true,\"imp\":false,\"nt\":\"meme\",\"r\":\"boilerplate desc\"}\n```",
		},
	}
	p := NewAINarrativeProbe(fake, AINarrativeConfig{Enabled: true}, slog.Default())
	in := buildDTO("DOGE2", "DOGE2", "this is the next doge to the moon buy now 1000x")

	out, err := p.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.IsCopyPasteDesc {
		t.Error("IsCopyPasteDesc should be true")
	}
	if out.NarrativeType != "meme" {
		t.Errorf("NarrativeType: want meme, got %q", out.NarrativeType)
	}
}

func TestAINarrativeProbe_AIErrorFailOpen(t *testing.T) {
	fake := &fakeAIClient{err: errors.New("rate limited")}
	p := NewAINarrativeProbe(fake, AINarrativeConfig{Enabled: true}, slog.Default())
	in := buildDTO("RUG", "RUG", "definitely not a rug trust me")

	out, err := p.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("probe should be fail-open, got error: %v", err)
	}
	if out.NarrativeKnown {
		t.Error("NarrativeKnown should be false on AI error (fail-open)")
	}
	// Original fields unchanged.
	if out.MetadataDescription != in.MetadataDescription {
		t.Error("MetadataDescription should be unchanged on error")
	}
}

func TestAINarrativeProbe_ScoreClamped(t *testing.T) {
	fake := &fakeAIClient{
		response: &ai.CompletionResponse{
			Content: `{"ns":99,"sp":-5,"cp":false,"imp":false,"nt":"other","r":"out of bounds test"}`,
		},
	}
	p := NewAINarrativeProbe(fake, AINarrativeConfig{Enabled: true}, slog.Default())
	in := buildDTO("TK", "TK", "some description")

	out, _ := p.Probe(context.Background(), in)
	if out.NarrativeScore != 10.0 {
		t.Errorf("NarrativeScore should be clamped to 10, got %v", out.NarrativeScore)
	}
	if out.ScamProbabilityScore != 0.0 {
		t.Errorf("ScamProbabilityScore should be clamped to 0, got %v", out.ScamProbabilityScore)
	}
}

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{`{"ns":5}`, `{"ns":5}`},
		{"```json\n{\"ns\":5}\n```", `{"ns":5}`},
		{"some text {\"ns\":5} more text", `{"ns":5}`},
		{"no json here", ""},
	}
	for _, c := range cases {
		got := extractJSON(c.input)
		if got != c.want {
			t.Errorf("extractJSON(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}
