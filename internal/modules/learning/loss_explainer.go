package learning

// loss_explainer.go — AI-powered loss explanation for Layer 10 (Learning Engine).
//
// LossExplainer enriches LearningRecord DTOs for false-positive (FP) and
// false-negative (FN) records with a natural-language explanation and a
// canonical loss-category label. True-positive (TP) records pass through
// unchanged.
//
// # Design constraints
//
//   - Fail-open: any error returns the original record with AIExplanationKnown=false.
//   - Non-blocking: the caller is expected to call Explain from a goroutine or
//     after-the-fact batch — never on the hot execution path.
//   - Idempotent: if AIExplanationKnown=true already, Explain returns immediately.
//   - Deterministic: Temperature=0 ensures same input always produces same output.
//
// # Prompt design
//
//   - System message: caveman-mode compressed analyst persona (~50 tokens).
//   - User message: structured compact summary of the losing trade (~80 tokens).
//   - MaxTokens=60: category label + one-sentence reason is under 60 output tokens.
//   - User content truncation is handled by the AIClient.MaxPromptChars guard.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/ai"
)

// validLossCategories are the canonical root-cause buckets for a losing trade.
// The LLM is constrained to output one of these.
var validLossCategories = map[string]bool{
	"timing":        true,
	"scam":          true,
	"momentum_fade": true,
	"execution":     true,
	"data_quality":  true,
	"narrative":     true,
	"unknown":       true,
}

// lossExplainerResponse is the compact JSON structure expected from the LLM.
type lossExplainerResponse struct {
	Cat string `json:"cat"` // loss category (canonical bucket)
	Why string `json:"why"` // one-sentence reason, max ~180 chars
}

// LossExplainer enriches LearningRecord DTOs with AI-generated explanations.
// Constructed with NewLossExplainer; Explain is safe for concurrent use.
type LossExplainer struct {
	client ai.AIClient
	logger *slog.Logger
}

// NewLossExplainer creates a LossExplainer. client may be nil when AI
// enrichment is disabled — Explain returns records unchanged.
func NewLossExplainer(client ai.AIClient, logger *slog.Logger) *LossExplainer {
	if logger == nil {
		logger = slog.Default()
	}
	return &LossExplainer{client: client, logger: logger}
}

// Explain enriches a LearningRecord with an AI loss explanation.
//
// Short-circuit conditions (return original record unchanged):
//   - client is nil (AI disabled)
//   - record.Outcome is "TP" — winners don't need explanation
//   - record.AIExplanationKnown=true — idempotent
//   - any AI call or parse error — fail-open, log warning
func (e *LossExplainer) Explain(ctx context.Context, record contracts.LearningRecordDTO) (contracts.LearningRecordDTO, error) {
	if e.client == nil {
		return record, nil
	}
	// Only explain losses and missed pumps.
	if record.Outcome == "TP" {
		return record, nil
	}
	// Idempotent.
	if record.AIExplanationKnown {
		return record, nil
	}

	req := buildLossRequest(record)
	resp, err := e.client.Complete(ctx, req)
	if err != nil {
		e.logger.Warn("loss_explainer_ai_failed",
			"record_id", record.RecordID,
			"outcome", record.Outcome,
			"error", truncateLossMsg(err.Error()),
		)
		return record, nil
	}

	parsed, parseErr := parseLossResponse(resp.Content)
	if parseErr != nil {
		e.logger.Warn("loss_explainer_parse_failed",
			"record_id", record.RecordID,
			"error", parseErr.Error(),
			"raw", truncateLossMsg(resp.Content),
		)
		return record, nil
	}

	out := record
	out.AIExplanationKnown = true
	out.AILossCategory = parsed.Cat
	out.AIExplanation = parsed.Why
	e.logger.Debug("loss_explainer_enriched",
		"record_id", record.RecordID,
		"category", parsed.Cat,
	)
	return out, nil
}

// — internal ——————————————————————————————————————————————————————————————

func buildLossRequest(r contracts.LearningRecordDTO) *ai.CompletionRequest {
	system := "Crypto trade post-mortem analyst. Output JSON only. " +
		"Categories: timing|scam|momentum_fade|execution|data_quality|narrative|unknown. " +
		"Be terse. Max 180 chars for why."

	// Compact structured context — only the fields that are diagnostic.
	user := fmt.Sprintf(
		"Trade: pnl=%.1f%% outcome=%s cls=%s cohort=%s\n"+
			"Scores: edge_strength=%.2f edge_conf=%.2f probability=%.2f narrative=%.1f\n"+
			`JSON: {"cat":"<category>","why":"<reason max180chars>"}`,
		r.PnlPct, r.Outcome, r.Classification, r.Cohort,
		r.EdgeSnapshot.EdgeStrength, r.EdgeSnapshot.EdgeConfidence,
		r.ValidatedSnapshot.ProbabilityUsed,
		r.FeaturesSnapshot.NarrativeScore,
	)

	return &ai.CompletionRequest{
		Messages: []ai.Message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		MaxTokens:   60,
		Temperature: 0,
	}
}

func parseLossResponse(content string) (*lossExplainerResponse, error) {
	jsonStr := extractLossJSON(content)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}
	var r lossExplainerResponse
	if err := json.Unmarshal([]byte(jsonStr), &r); err != nil {
		return nil, fmt.Errorf("unmarshal loss response: %w", err)
	}
	r.Cat = strings.ToLower(strings.TrimSpace(r.Cat))
	if !validLossCategories[r.Cat] {
		r.Cat = "unknown"
	}
	if len(r.Why) > 200 {
		r.Why = r.Why[:200]
	}
	return &r, nil
}

// extractLossJSON strips markdown fences and returns the first JSON object.
func extractLossJSON(s string) string {
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

// truncateLossMsg caps a string at 200 chars — security invariant.
func truncateLossMsg(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}
