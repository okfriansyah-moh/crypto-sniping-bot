package probes

// ai_narrative_probe.go — async LLM narrative scorer (Layer 0.5 enrichment).
//
// Adds narrative-quality signals to a MarketDataDTO using the GitHub Copilot
// API. All calls are async, fail-open: if the probe fails for any reason,
// NarrativeKnown stays false and the token continues through the pipeline with
// unmodified scores.
//
// Prompt design:
//   - System message uses caveman-mode compression (terse, no filler) to stay
//     under context limits and minimize token usage on the PAT rate limit.
//   - User message is a one-shot JSON-output prompt — no multi-turn, no CoT.
//   - Trending narratives injected from config so the model stays current
//     without internet access per request (static context injection).
//   - User content truncated to MaxDescriptionChars before prompt construction
//     to satisfy the AIClient.MaxPromptChars guard.
//
// Security invariants inherited from AIClient:
//   - Token from GITHUB_COPILOT_TOKEN env var only.
//   - HTTPS-only endpoint, 4 KiB response bound.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/ai"
)

// AINarrativeConfig configures the ai_narrative probe.
// Lives under config/pipeline.yaml → ai_enrichment.narrative_probe.
type AINarrativeConfig struct {
	// Enabled gates the probe. When false, Probe returns (in, nil) immediately.
	Enabled bool `yaml:"enabled"`

	// MaxDescriptionChars caps the description before sending to LLM.
	// Keeps the prompt within AIClient.MaxPromptChars. Default 300.
	MaxDescriptionChars int `yaml:"max_description_chars"`

	// TrendingNarratives lists active meta-narratives injected as static
	// context into the system message. Update via pipeline.yaml — no code
	// change required.
	TrendingNarratives []string `yaml:"trending_narratives"`
}

// AINarrativeProbe queries the GitHub Copilot API to score a token's narrative
// quality. Implements the MarketProbe interface.
type AINarrativeProbe struct {
	client ai.AIClient
	cfg    AINarrativeConfig
	logger *slog.Logger
}

// NewAINarrativeProbe creates an AINarrativeProbe.
// client may be nil when Enabled=false — the probe returns immediately without
// calling the AI client.
func NewAINarrativeProbe(client ai.AIClient, cfg AINarrativeConfig, logger *slog.Logger) *AINarrativeProbe {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.MaxDescriptionChars <= 0 {
		cfg.MaxDescriptionChars = 300
	}
	return &AINarrativeProbe{client: client, cfg: cfg, logger: logger}
}

// Name implements MarketProbe.
func (p *AINarrativeProbe) Name() string { return "ai_narrative" }

// Probe enriches in with AI narrative scores. Implements MarketProbe.
//
// Short-circuit conditions (return immediately with NarrativeKnown unchanged):
//   - cfg.Enabled=false
//   - in.NarrativeKnown=true (worker-cached result; skip duplicate call)
//   - in.MetadataDescription=="" (no text to score; set neutral defaults, Known=true)
func (p *AINarrativeProbe) Probe(ctx context.Context, in contracts.MarketDataDTO) (contracts.MarketDataDTO, error) {
	if !p.cfg.Enabled {
		return in, nil
	}
	// Already enriched — idempotent.
	if in.NarrativeKnown {
		return in, nil
	}
	// No description to score — set neutral defaults so downstream code
	// doesn't treat this as "unknown".
	if strings.TrimSpace(in.MetadataDescription) == "" {
		out := in
		out.NarrativeKnown = true
		out.NarrativeScore = 5.0 // neutral
		out.ScamProbabilityScore = 0.0
		out.NarrativeType = "generic"
		out.NarrativeReason = "no description"
		return out, nil
	}

	req := p.buildRequest(in)
	resp, err := p.client.Complete(ctx, req)
	if err != nil {
		// Fail-open: probe error must never block the pipeline.
		p.logger.Warn("ai_narrative_probe_failed",
			"token", in.TokenAddress,
			"error", truncateAIError(err.Error()),
		)
		return in, nil
	}

	out, parseErr := p.parseResponse(in, resp.Content)
	if parseErr != nil {
		p.logger.Warn("ai_narrative_probe_parse_failed",
			"token", in.TokenAddress,
			"error", parseErr.Error(),
			"raw", truncateAIError(resp.Content),
		)
		return in, nil
	}

	p.logger.Info("ai_narrative_probe",
		"token", in.TokenAddress,
		"name", in.Name,
		"symbol", in.Symbol,
		"narrative_score", out.NarrativeScore,
		"scam_score", out.ScamProbabilityScore,
		"type", out.NarrativeType,
		"copy_paste", out.IsCopyPasteDesc,
		"impersonation", out.IsImpersonation,
		"reason", out.NarrativeReason,
	)
	return out, nil
}

// — internal ——————————————————————————————————————————————————————————————

// narrativeResponse is the expected JSON structure from the LLM.
// Field names are kept terse to minimise output tokens (caveman protocol).
type narrativeResponse struct {
	NS  float64 `json:"ns"`  // narrative_score  0–10
	SP  float64 `json:"sp"`  // scam_probability 0–10
	CP  bool    `json:"cp"`  // is_copy_paste_desc
	Imp bool    `json:"imp"` // is_impersonation
	NT  string  `json:"nt"`  // narrative_type
	R   string  `json:"r"`   // reason (max ~40 chars)
}

func (p *AINarrativeProbe) buildRequest(in contracts.MarketDataDTO) *ai.CompletionRequest {
	// System message: caveman-mode, token-efficient trader-analyst persona.
	// Trending narratives injected as static context (avoids HTTP round-trip
	// to get current meta).
	trending := strings.Join(p.cfg.TrendingNarratives, ", ")
	if trending == "" {
		trending = "AI agents, DePIN, gaming, RWA, memecoins"
	}
	system := fmt.Sprintf(
		"Crypto trader analyst. Solana meme token scorer. Output JSON only, no prose. "+
			"Trending now: %s. "+
			"Score 0=bad 10=good. cp=copy-paste boilerplate reuse across rugs. imp=known project name mimic.",
		trending,
	)

	// User message: compact single-shot JSON-output prompt.
	desc := in.MetadataDescription
	if len(desc) > p.cfg.MaxDescriptionChars {
		desc = desc[:p.cfg.MaxDescriptionChars]
	}
	user := fmt.Sprintf(
		"Token: NAME=%s SYM=%s\nDesc: %s\n"+
			`JSON: {"ns":0-10,"sp":0-10,"cp":true/false,"imp":true/false,"nt":"ai|defi|gaming|meme|generic|scam|other","r":"max40chars"}`,
		in.Name, in.Symbol, desc,
	)

	return &ai.CompletionRequest{
		Messages: []ai.Message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		MaxTokens:   80,
		Temperature: 0, // deterministic: same input → same output
	}
}

// parseResponse extracts the JSON payload from the LLM response.
// The model sometimes wraps its JSON in markdown code fences — we strip them.
func (p *AINarrativeProbe) parseResponse(in contracts.MarketDataDTO, content string) (contracts.MarketDataDTO, error) {
	jsonStr := extractJSON(content)
	if jsonStr == "" {
		return in, fmt.Errorf("no JSON object found in response")
	}

	var nr narrativeResponse
	if err := json.Unmarshal([]byte(jsonStr), &nr); err != nil {
		return in, fmt.Errorf("unmarshal narrative response: %w", err)
	}

	// Clamp scores to valid range to guard against model hallucination.
	nr.NS = clampScore(nr.NS)
	nr.SP = clampScore(nr.SP)

	reason := nr.R
	if len(reason) > 50 {
		reason = reason[:50]
	}

	out := in
	out.NarrativeKnown = true
	out.NarrativeScore = nr.NS
	out.ScamProbabilityScore = nr.SP
	out.IsCopyPasteDesc = nr.CP
	out.IsImpersonation = nr.Imp
	out.NarrativeType = sanitizeNarrativeType(nr.NT)
	out.NarrativeReason = reason
	return out, nil
}

// extractJSON finds the first JSON object in s, stripping markdown fences.
func extractJSON(s string) string {
	// Strip markdown code fences if present.
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		lines := strings.SplitN(s, "\n", 2)
		if len(lines) == 2 {
			s = lines[1]
		}
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}

// sanitizeNarrativeType guards against model hallucinating invalid types.
func sanitizeNarrativeType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "ai", "defi", "gaming", "meme", "generic", "scam", "other":
		return strings.ToLower(t)
	}
	return "other"
}

// clampScore bounds a float to [0, 10].
func clampScore(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 10 {
		return 10
	}
	return v
}

// truncateAIError caps an error/content string at 200 chars — security invariant.
func truncateAIError(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}
