// Safety-floor invariant tests for the validation module.
//
// These pin the safety-net contract that prevents the validator from
// trading on absent or fabricated feature data:
//
//  1. config/probability.yaml must keep min_model_confidence at ≥0.35.
//     The root cause of the constant-0.879 regression was fixed in F-1:
//     deriveConfidence(known=false, N=0) now returns 0.0, so absent
//     features are excluded from minFeatureConfidence entirely.
//     ContractSafety confidence is always ≥ 0.40 (DQ flags always present),
//     so real features produce minFeatureConfidence ≈ 0.40 which clears the
//     0.35 gate. Completely absent signal (all features zero) still returns
//     0.0 < 0.35 and falls back to prior.
//  2. With the fallback-mechanism gate set explicitly to 0.70, confidence
//     0.50 still falls back to prior — confirming the mechanism works.
//  3. With prior=0.35 against the prior gain/loss spread, EV is deeply
//     negative, so validation must REJECT.
//
// If assertion (1) fails, min_model_confidence was lowered below the
// ContractSafety confidence floor (0.40), which would cause completely
// absent signals to wrongly pass the confidence gate.
package validation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"

	"gopkg.in/yaml.v3"
)

// minSafetyNetConfidence is the YAML safety floor: min_model_confidence must
// stay above this value. Set to 0.35 — below ContractSafety's guaranteed
// minimum confidence (0.40), but above "all features absent" (0.0).
// Before F-1 fix this was 0.70; after fix the anchor is the 0.40
// ContractSafety floor rather than the prior stub-confidence of ~0.50.
const minSafetyNetConfidence = 0.35

// fallbackMechanismThreshold is used by TestStubFeatureConfidence to verify
// that the fallback mechanism itself works: confidence 0.50 falls back when
// the gate is 0.70. This is independent of the YAML floor above.
const fallbackMechanismThreshold = 0.70

func TestProbabilityYAML_MinModelConfidenceAboveSafetyFloor(t *testing.T) {
	// Locate config/probability.yaml relative to this test file.
	repoRoot := findRepoRoot(t)
	yamlPath := filepath.Join(repoRoot, "config", "probability.yaml")
	raw, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read %s: %v", yamlPath, err)
	}

	// Parse just the probability subtree.
	var doc struct {
		Probability config.ProbabilityRuntimeConfig `yaml:"probability"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", yamlPath, err)
	}

	if doc.Probability.MinModelConfidence < minSafetyNetConfidence {
		t.Fatalf(
			"SAFETY NET BREACH: probability.min_model_confidence=%.3f "+
				"is below safety floor %.2f. This threshold must remain above "+
				"0 and below the ContractSafety confidence floor (0.40) so that "+
				"fully-absent signal (minFeatureConfidence=0) still falls back to "+
				"prior. F-1 fix: deriveConfidence(known=false,N=0)=0.0 excludes "+
				"absent features; real features start at ~0.40 (ContractSafety).",
			doc.Probability.MinModelConfidence, minSafetyNetConfidence,
		)
	}
}

func TestStubFeatureConfidence_ForcesFallbackAndReject(t *testing.T) {
	// Simulate a typical stub-feature probability estimate the way it
	// appeared in the 2026-05-02 log: model output with low confidence.
	probCfg := &config.ProbabilityRuntimeConfig{
		UseModelOutput:     true,
		PriorProbability:   0.35,
		MinModelConfidence: fallbackMechanismThreshold, // 0.70: tests the fallback mechanism, not the YAML floor
		ProbJoinTimeoutMs:  200,
		RejectOutOfRange:   true,
		RejectNanOrInf:     true,
	}
	// Production validation config — gain=3000 / loss=4000 / prior_slip=200,
	// fixed_costs=150, ev_threshold=100. Mirrors config/pipeline.yaml.
	valCfg := &config.ValidationConfig{
		PriorProbability: 0.35,
		PriorGainBps:     3000,
		PriorLossBps:     4000,
		PriorSlippageBps: 200,
		EvThresholdBps:   100,
		FixedCostsBps:    150,
		BuildSubmitP95Ms: 500,
		TTLSeconds:       5,
	}
	mod := New(valCfg).WithProbabilityRuntime(probCfg)

	// Stub features → confidence well below 0.70 gate. Probability of 0.879
	// is the exact stub regression value.
	prob := &contracts.ProbabilityEstimateDTO{
		Probability: 0.879,
		Confidence:  0.50,
		Calibration: 0.50,
	}
	got, _ := mod.ProcessWithEstimates(context.Background(), goodEdge(), prob, nil, nil)

	// Contract: low confidence MUST fall back to prior. ProbabilityUsed
	// reverting to prior_probability is the observable proof.
	if got.ProbabilityUsed != probCfg.PriorProbability {
		t.Fatalf(
			"fallback regression: ProbabilityUsed=%.4f, expected prior=%.2f. "+
				"Stub features (confidence<%.2f) must fall back to prior, not use model output %.3f.",
			got.ProbabilityUsed, probCfg.PriorProbability,
			probCfg.MinModelConfidence, prob.Probability,
		)
	}

	// With prior=0.35 against gain=3000 / loss=4000 + 150 + 200,
	// EV = 0.35*3000 - 0.65*4000 - 150 - 200 = -1900 bps → REJECT.
	if got.Decision != "REJECT" {
		t.Fatalf(
			"safety regression: with stub features, validation MUST "+
				"REJECT (EV ≈ -1900 bps with prior fallback). got Decision=%s "+
				"reject_reason=%q expected_value_bps=%d probability_used=%.4f",
			got.Decision, got.RejectReason, got.ExpectedValueBps, got.ProbabilityUsed,
		)
	}
	if !strings.Contains(got.RejectReason, "ev") {
		t.Fatalf("expected ev-related reject reason; got %q", got.RejectReason)
	}
}

// findRepoRoot walks up from CWD to locate the repo root (the directory
// containing go.mod). Used so this test works whether `go test` is invoked
// from the package dir or the repo root.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repo root (go.mod) not found from %s", dir)
		}
		dir = parent
	}
}
