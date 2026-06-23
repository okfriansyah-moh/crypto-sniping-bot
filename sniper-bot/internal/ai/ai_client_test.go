package ai

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

// fakeHTTP satisfies httpDoer and returns a pre-canned response.
type fakeHTTP struct {
	statusCode int
	body       string
	err        error
}

func (f *fakeHTTP) Do(_ *http.Request) (*http.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{
		StatusCode: f.statusCode,
		Body:       io.NopCloser(bytes.NewBufferString(f.body)),
	}, nil
}

// newTestClient creates a GroqClient with a fake HTTP transport and
// Enabled=true. Token validation is bypassed by setting the env before calling.
func newTestClient(t *testing.T, fake *fakeHTTP) *GroqClient {
	t.Helper()
	t.Setenv("GROQ_API_KEY", "test-token")
	c, err := NewGroqClient(Config{
		Enabled:          true,
		Endpoint:         "https://api.groq.com/openai/v1/chat/completions",
		Model:            "llama-3.3-70b-versatile",
		MaxResponseBytes: 4096,
		RateLimitPerMin:  10,
		MaxPromptChars:   600,
	}, slog.Default())
	if err != nil {
		t.Fatalf("NewGroqClient: %v", err)
	}
	c.WithHTTPClient(fake)
	return c
}

func TestGroqClient_Disabled(t *testing.T) {
	c, err := NewGroqClient(Config{Enabled: false}, slog.Default())
	if err != nil {
		t.Fatalf("unexpected error for disabled client: %v", err)
	}
	_, err = c.Complete(context.Background(), &CompletionRequest{})
	if err != ErrDisabled {
		t.Errorf("want ErrDisabled, got %v", err)
	}
}

func TestGroqClient_NonHTTPSEndpointRejected(t *testing.T) {
	t.Setenv("GROQ_API_KEY", "tok")
	_, err := NewGroqClient(Config{
		Enabled:  true,
		Endpoint: "http://api.groq.com/openai/v1/chat/completions",
	}, slog.Default())
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Errorf("expected HTTPS enforcement error, got %v", err)
	}
}

func TestGroqClient_MissingTokenErrors(t *testing.T) {
	t.Setenv("GROQ_API_KEY", "")
	_, err := NewGroqClient(Config{
		Enabled:  true,
		Endpoint: "https://api.groq.com/openai/v1/chat/completions",
	}, slog.Default())
	if err == nil || !strings.Contains(err.Error(), "GROQ_API_KEY") {
		t.Errorf("expected token missing error, got %v", err)
	}
}

func TestGroqClient_SuccessfulCompletion(t *testing.T) {
	fake := &fakeHTTP{
		statusCode: 200,
		body: `{
			"choices":[{"message":{"content":"{\"ns\":7,\"sp\":2,\"cp\":false,\"imp\":false,\"nt\":\"ai\",\"r\":\"narrative aligned\"}"}}],
			"usage":{"prompt_tokens":50,"completion_tokens":30}
		}`,
	}
	c := newTestClient(t, fake)

	resp, err := c.Complete(context.Background(), &CompletionRequest{
		Messages:    []Message{{Role: "user", Content: "test prompt"}},
		MaxTokens:   100,
		Temperature: 0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.InputTokens != 50 || resp.OutputTokens != 30 {
		t.Errorf("unexpected token counts: %+v", resp)
	}
	if !strings.Contains(resp.Content, "narrative aligned") {
		t.Errorf("unexpected content: %q", resp.Content)
	}
}

func TestGroqClient_RateLimitFail(t *testing.T) {
	t.Setenv("GROQ_API_KEY", "tok")
	// RateLimitPerMin=1 means bucket capacity=1. After one call, bucket empty.
	c, err := NewGroqClient(Config{
		Enabled:         true,
		Endpoint:        "https://api.groq.com/openai/v1/chat/completions",
		RateLimitPerMin: 1,
		MaxRetries:      0,
	}, slog.Default())
	if err != nil {
		t.Fatalf("NewGroqClient: %v", err)
	}
	c.WithHTTPClient(&fakeHTTP{statusCode: 200, body: `{"choices":[{"message":{"content":"x"}}],"usage":{}}`})

	// First call consumes the single bucket token.
	_, _ = c.Complete(context.Background(), &CompletionRequest{})

	// Second call must fail with ErrRateLimit.
	_, err = c.Complete(context.Background(), &CompletionRequest{})
	if err != ErrRateLimit {
		t.Errorf("want ErrRateLimit, got %v", err)
	}
}

func TestGroqClient_HTTP429Retried(t *testing.T) {
	calls := 0
	fake := &fakeHTTP{}
	c := newTestClient(t, &fakeHTTP{})
	// Override with a counting fake that returns 429 on first call, 200 on second.
	var counting httpDoer = &countingHTTP{
		responses: []fakeHTTP{
			{statusCode: 429, body: "rate limited"},
			{statusCode: 200, body: `{"choices":[{"message":{"content":"ok"}}],"usage":{}}`},
		},
		calls: &calls,
	}
	_ = fake
	c.WithHTTPClient(counting)

	resp, err := c.Complete(context.Background(), &CompletionRequest{MaxTokens: 10})
	if err != nil {
		t.Fatalf("expected retry to succeed, got: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("unexpected content: %q", resp.Content)
	}
	if calls != 2 {
		t.Errorf("want 2 HTTP calls (1 retry), got %d", calls)
	}
}

// countingHTTP serves a pre-ordered list of responses and counts calls.
type countingHTTP struct {
	responses []fakeHTTP
	calls     *int
}

func (ch *countingHTTP) Do(req *http.Request) (*http.Response, error) {
	i := *ch.calls
	*ch.calls++
	if i >= len(ch.responses) {
		return nil, io.EOF
	}
	f := ch.responses[i]
	return &http.Response{
		StatusCode: f.statusCode,
		Body:       io.NopCloser(bytes.NewBufferString(f.body)),
	}, nil
}

func TestTruncateMsg(t *testing.T) {
	if got := truncateMsg("hello", 3); got != "hel" {
		t.Errorf("want %q got %q", "hel", got)
	}
	if got := truncateMsg("hi", 10); got != "hi" {
		t.Errorf("want %q got %q", "hi", got)
	}
}

func TestTruncateUserContent(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: strings.Repeat("s", 1000)},
		{Role: "user", Content: strings.Repeat("u", 1000)},
	}
	truncateUserContent(msgs, 100)
	if len(msgs[0].Content) != 1000 {
		t.Error("system message should not be truncated")
	}
	if len(msgs[1].Content) != 100 {
		t.Errorf("user message should be truncated to 100, got %d", len(msgs[1].Content))
	}
}
