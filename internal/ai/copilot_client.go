// Package ai provides a minimal GitHub Copilot API client for on-demand
// LLM enrichment of pipeline signals.
//
// # 1-shot autonomous design
//
// All calls are fully autonomous — fire-and-return with no human approval gate.
// Complete() issues one HTTP request (plus one retry on 429/5xx) and returns
// immediately. There is no interaction loop, no prompt for confirmation, and no
// blocking on human input. Callers are always fail-open: on any error the
// pipeline continues with NarrativeKnown=false / AIExplanationKnown=false.
//
// # Model configuration
//
// Model priority (highest → lowest):
//  1. AI_ENRICH_MODEL env var (set at runtime — same pattern as MODEL_HEAVY in run_parallel.sh)
//  2. ai_enrichment.model in config/pipeline.yaml
//  3. Built-in default: gpt-4o-mini
//
// # Security invariants (never relax)
//
//   - Auth token read exclusively from env GITHUB_COPILOT_TOKEN — never YAML.
//   - All requests use HTTPS only; non-HTTPS endpoint is rejected at construction.
//   - Response body bounded to MaxResponseBytes (default 4 KiB).
//   - User content truncated to MaxPromptChars before sending (context limit guard).
//   - RPC error messages truncated to 200 chars before logging/surfacing.
//
// # Resilience invariants
//
//   - Token bucket rate limiter (RateLimitPerMin tokens, non-blocking: fail-open).
//   - Single retry on HTTP 429 or 5xx with 200 ms fixed backoff.
//   - All callers receive (nil, error) on any failure — callers MUST degrade gracefully.
//   - Enabled=false returns (nil, ErrDisabled) immediately — zero network traffic.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ErrDisabled is returned when the client is configured with Enabled=false.
var ErrDisabled = errors.New("ai/copilot: client disabled")

// ErrRateLimit is returned when the token bucket is empty (non-blocking check).
var ErrRateLimit = errors.New("ai/copilot: rate limit reached — try later")

// Config configures the GitHub Copilot API client.
// The auth token is NOT a field — read exclusively from GITHUB_COPILOT_TOKEN env var.
type Config struct {
	Enabled          bool   `yaml:"enabled"`
	Endpoint         string `yaml:"endpoint"`           // default: https://api.githubcopilot.com/chat/completions
	Model            string `yaml:"model"`              // default: gpt-4o
	TimeoutMs        int    `yaml:"timeout_ms"`         // default: 8000
	MaxRetries       int    `yaml:"max_retries"`        // default: 1 (1 retry = 2 total attempts)
	MaxResponseBytes int64  `yaml:"max_response_bytes"` // default: 4096
	RateLimitPerMin  int    `yaml:"rate_limit_per_min"` // default: 8
	MaxPromptChars   int    `yaml:"max_prompt_chars"`   // default: 600 — context limit guard
}

// Message is a single OpenAI-style chat message.
type Message struct {
	Role    string `json:"role"` // system | user | assistant
	Content string `json:"content"`
}

// CompletionRequest is the minimal OpenAI chat completions payload.
type CompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature"`
}

// CompletionResponse contains the first choice's text content.
type CompletionResponse struct {
	Content      string
	InputTokens  int
	OutputTokens int
}

// AIClient is the interface all callers depend on. A single method makes
// test fakes trivial and prevents callers from depending on implementation details.
type AIClient interface {
	Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
}

// httpDoer is the minimal HTTP interface needed by CopilotClient.
// *http.Client satisfies it; tests inject a fake.
type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// CopilotClient calls the GitHub Copilot chat completions endpoint.
// Construct with NewCopilotClient; call StartRateLimiter once at startup.
type CopilotClient struct {
	cfg    Config
	token  string
	client httpDoer
	logger *slog.Logger

	// Token bucket: capacity = RateLimitPerMin, refilled per interval by
	// StartRateLimiter. Non-blocking consume: if empty → ErrRateLimit (fail-open).
	bucket chan struct{}
	once   sync.Once
	stopCh chan struct{}
}

// NewCopilotClient creates a ready-to-use CopilotClient.
//
// Returns an error when:
//   - Enabled=true and GITHUB_COPILOT_TOKEN is not set.
//   - Endpoint does not begin with "https://".
func NewCopilotClient(cfg Config, logger *slog.Logger) (*CopilotClient, error) {
	if logger == nil {
		logger = slog.Default()
	}
	applyConfigDefaults(&cfg)

	token := os.Getenv("GITHUB_COPILOT_TOKEN")
	if cfg.Enabled && token == "" {
		return nil, fmt.Errorf("ai/copilot: GITHUB_COPILOT_TOKEN env var required when ai_enrichment.enabled=true")
	}

	// Enforce HTTPS — security invariant, never relax.
	if !strings.HasPrefix(cfg.Endpoint, "https://") {
		return nil, fmt.Errorf("ai/copilot: endpoint must use HTTPS, got %q", cfg.Endpoint)
	}

	timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond
	c := &CopilotClient{
		cfg:    cfg,
		token:  token,
		client: &http.Client{Timeout: timeout},
		logger: logger,
		bucket: make(chan struct{}, cfg.RateLimitPerMin),
		stopCh: make(chan struct{}),
	}

	// Pre-fill the bucket so the first N calls (N=RateLimitPerMin) are
	// immediately available without waiting for the refill goroutine.
	for i := 0; i < cfg.RateLimitPerMin; i++ {
		c.bucket <- struct{}{}
	}

	logger.Info("ai_copilot_client_initialized",
		"enabled", cfg.Enabled,
		"model", cfg.Model,
		"endpoint", cfg.Endpoint,
		"rate_limit_per_min", cfg.RateLimitPerMin,
		"timeout_ms", cfg.TimeoutMs,
		"max_retries", cfg.MaxRetries,
		"max_response_bytes", cfg.MaxResponseBytes,
	)
	return c, nil
}

// WithHTTPClient replaces the internal HTTP client. Used in tests to inject a fake.
func (c *CopilotClient) WithHTTPClient(h httpDoer) { c.client = h }

// StartRateLimiter launches the background token-bucket refill goroutine.
// Safe to call multiple times — only the first call starts the goroutine.
// Call Stop() at shutdown to release resources.
func (c *CopilotClient) StartRateLimiter() {
	c.once.Do(func() {
		interval := time.Minute / time.Duration(c.cfg.RateLimitPerMin)
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					// Non-blocking put: if the bucket is already full (unused
					// tokens), silently discard the refill token.
					select {
					case c.bucket <- struct{}{}:
					default:
					}
				case <-c.stopCh:
					return
				}
			}
		}()
	})
}

// Stop shuts down the background rate-limiter goroutine.
func (c *CopilotClient) Stop() {
	select {
	case <-c.stopCh: // already closed
	default:
		close(c.stopCh)
	}
}

// Complete sends a chat completion request to GitHub Copilot and returns the
// first choice's content. Implements AIClient.
//
// Failure modes (all fail-open — caller receives non-nil error):
//   - Disabled=true → ErrDisabled
//   - Rate limit bucket empty → ErrRateLimit
//   - Context cancelled → ctx.Err()
//   - HTTP 429 / 5xx → retried once, then error
//   - Parse failure → error
func (c *CopilotClient) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	if !c.cfg.Enabled {
		return nil, ErrDisabled
	}

	// Non-blocking token bucket consume.
	select {
	case <-c.bucket:
		// token acquired — proceed
	default:
		c.logger.Warn("ai_copilot_rate_limited",
			"model", c.cfg.Model,
			"rate_limit_per_min", c.cfg.RateLimitPerMin,
		)
		return nil, ErrRateLimit
	}

	// Truncate user messages to guard against context limit errors (400s).
	guardedMessages := cloneMessages(req.Messages)
	truncateUserContent(guardedMessages, c.cfg.MaxPromptChars)

	body, err := json.Marshal(&CompletionRequest{
		Model:       c.cfg.Model,
		Messages:    guardedMessages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	})
	if err != nil {
		return nil, fmt.Errorf("ai/copilot: marshal request: %w", err)
	}

	attempts := c.cfg.MaxRetries + 1
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			// Fixed 200 ms backoff before retry.
			select {
			case <-time.After(200 * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		resp, err := c.doRequest(ctx, body)
		if err == nil {
			c.logger.Debug("ai_copilot_complete",
				"model", c.cfg.Model,
				"input_tokens", resp.InputTokens,
				"output_tokens", resp.OutputTokens,
			)
			return resp, nil
		}
		lastErr = err
		if !isRetryableErr(err) {
			break
		}
	}
	return nil, lastErr
}

// — internal helpers ———————————————————————————————————————————

type copilotRespBody struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *CopilotClient) doRequest(ctx context.Context, body []byte) (*CompletionResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ai/copilot: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Copilot-Integration-Id", "vscode-chat")
	req.Header.Set("User-Agent", "crypto-sniping-bot/ai-probe")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ai/copilot: http: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, c.cfg.MaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("ai/copilot: read response body: %w", err)
	}

	// Retryable: rate-limited or server error.
	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		return nil, &retryableError{fmt.Sprintf("ai/copilot: HTTP %d", resp.StatusCode)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ai/copilot: HTTP %d: %s",
			resp.StatusCode, truncateMsg(string(respBytes), 200))
	}

	var parsed copilotRespBody
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return nil, fmt.Errorf("ai/copilot: parse response JSON: %w", err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("ai/copilot: API error %s: %s",
			parsed.Error.Type, truncateMsg(parsed.Error.Message, 200))
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("ai/copilot: empty choices in response")
	}

	return &CompletionResponse{
		Content:      parsed.Choices[0].Message.Content,
		InputTokens:  parsed.Usage.PromptTokens,
		OutputTokens: parsed.Usage.CompletionTokens,
	}, nil
}

// retryableError marks errors that warrant a retry attempt.
type retryableError struct{ msg string }

func (e *retryableError) Error() string { return e.msg }

func isRetryableErr(err error) bool {
	var re *retryableError
	return errors.As(err, &re)
}

// truncateMsg caps a string at n bytes — security invariant per architecture.
func truncateMsg(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// cloneMessages returns a shallow copy of the slice so truncation never
// mutates the caller's original request.
func cloneMessages(msgs []Message) []Message {
	out := make([]Message, len(msgs))
	copy(out, msgs)
	return out
}

// truncateUserContent caps user-role message content at maxChars.
// System messages are preserved intact (they contain the skill instructions).
func truncateUserContent(msgs []Message, maxChars int) {
	for i := range msgs {
		if msgs[i].Role == "user" && len(msgs[i].Content) > maxChars {
			msgs[i].Content = msgs[i].Content[:maxChars]
		}
	}
}

func applyConfigDefaults(cfg *Config) {
	if cfg.Endpoint == "" {
		cfg.Endpoint = "https://api.githubcopilot.com/chat/completions"
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-4o-mini"
	}
	// AI_ENRICH_MODEL env var overrides config YAML — same pattern as MODEL_HEAVY
	// in scripts/run_parallel.sh: MODEL_HEAVY="${MODEL_HEAVY:-claude-opus-4.7}".
	if override := os.Getenv("AI_ENRICH_MODEL"); override != "" {
		cfg.Model = override
	}
	if cfg.TimeoutMs <= 0 {
		cfg.TimeoutMs = 8000
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 1
	}
	if cfg.MaxResponseBytes <= 0 {
		cfg.MaxResponseBytes = 4096
	}
	if cfg.RateLimitPerMin <= 0 {
		cfg.RateLimitPerMin = 8
	}
	if cfg.MaxPromptChars <= 0 {
		cfg.MaxPromptChars = 600
	}
}
