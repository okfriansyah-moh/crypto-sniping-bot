// Package integration — gate-review collector regression tests.
//
// TestGateReviewCollect_June20260601Fixture exercises scripts/gate_review_collect.sh
// against the captured production gate-review dataset from 2026-06-01. That log is
// a real operational artifact (not a synthetic sample); it lives under output/logs/
// and is gitignored — the test skips when the fixture is absent locally.
package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	juneGateRawLog      = "output/logs/gate_raw_20260601_161344.log"
	juneGateEvidence    = "output/logs/gate_evidence_20260601_161344.json"
	juneGateBrief       = "output/logs/gate_brief_20260601_161344.txt"
	gateReviewScript    = "scripts/gate_review_collect.sh"
	legacyDeadWorkerL2 = "features_extracted"
	legacyDeadWorkerL3 = "edge_decision"
	legacyDeadWorkerL4 = "probability_scored"
	legacyDeadWorkerL5 = "validation_decision"
)

type gateEvidenceSnapshot struct {
	DetectedMode       string `json:"detected_mode"`
	ProductionDecision string `json:"production_decision"`
	BlockerCount       int    `json:"blocker_count"`
	OperationalEvidence struct {
		TracesCompleted int `json:"traces_completed"`
		LearningRecords int `json:"learning_records"`
	} `json:"operational_evidence"`
}

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
			t.Fatal("go.mod not found while walking up from cwd")
		}
		dir = parent
	}
}

func runGateReviewAnalyze(t *testing.T, root string) (evidence gateEvidenceSnapshot, brief string) {
	t.Helper()

	rawLog := filepath.Join(root, juneGateRawLog)
	if _, err := os.Stat(rawLog); err != nil {
		t.Skipf("June 1 gate fixture missing (%s) — copy production capture to output/logs/ to run this regression", juneGateRawLog)
	}

	script := filepath.Join(root, gateReviewScript)
	cmd := exec.Command("bash", script, "--analyze", rawLog)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gate_review_collect.sh --analyze failed: %v\n%s", err, out)
	}

	evidencePath := filepath.Join(root, juneGateEvidence)
	evidenceBytes, err := os.ReadFile(evidencePath)
	if err != nil {
		t.Fatalf("read evidence snapshot %s: %v", juneGateEvidence, err)
	}
	if err := json.Unmarshal(evidenceBytes, &evidence); err != nil {
		t.Fatalf("parse evidence JSON: %v", err)
	}

	briefPath := filepath.Join(root, juneGateBrief)
	briefBytes, err := os.ReadFile(briefPath)
	if err != nil {
		t.Fatalf("read gate brief %s: %v", juneGateBrief, err)
	}
	return evidence, string(briefBytes)
}

// TestGateReviewCollect_June20260601Fixture locks corrected gate-review evidence
// semantics for the 2026-06-01 production capture after Tasks 1–3.
func TestGateReviewCollect_June20260601Fixture(t *testing.T) {
	root := findRepoRoot(t)
	evidence, brief := runGateReviewAnalyze(t, root)

	t.Run("no_fake_L2_L5_dead_worker_blockers", func(t *testing.T) {
		if evidence.BlockerCount != 0 {
			t.Fatalf("blocker_count = %d, want 0 (DQ filtered-only window must not flag downstream workers dead)", evidence.BlockerCount)
		}
		blockersSection := extractBriefSection(brief, "2. BLOCKERS", "3. SAFE_TO_IGNORE_FOR_NOW")
		for _, legacy := range []string{legacyDeadWorkerL2, legacyDeadWorkerL3, legacyDeadWorkerL4, legacyDeadWorkerL5} {
			if strings.Contains(blockersSection, legacy) {
				t.Errorf("BLOCKERS section must not reference legacy msg %q", legacy)
			}
		}
		if strings.Contains(blockersSection, "Feature worker may be dead") ||
			strings.Contains(blockersSection, "Edge worker may be dead") ||
			strings.Contains(blockersSection, "Probability worker may be dead") ||
			strings.Contains(blockersSection, "Validation worker may be dead") {
			t.Error("BLOCKERS section must not report fake L2–L5 dead workers when upstream DQ emitted=0")
		}
	})

	t.Run("no_downstream_inflation_when_dq_emitted_zero", func(t *testing.T) {
		opsSection := extractBriefSection(brief, "5. OPERATIONAL EVIDENCE", "6. PRODUCTION CONFIDENCE MODEL")
		if !strings.Contains(opsSection, "dq_worker:") {
			t.Fatal("brief missing dq_worker stage_completed line")
		}
		if !strings.Contains(opsSection, "emitted=0") {
			t.Error("June 1 fixture expects dq_worker emitted=0 (all filtered/skipped)")
		}
		if !strings.Contains(opsSection, "features_worker:       0") {
			t.Errorf("features_worker count must stay 0 when DQ emitted=0; ops section:\n%s", opsSection)
		}
		if !strings.Contains(opsSection, "edge_worker:           0") {
			t.Errorf("edge_worker count must stay 0 when DQ emitted=0")
		}
	})

	t.Run("traces_completed_uses_L10_learning_record_emitted", func(t *testing.T) {
		if evidence.OperationalEvidence.TracesCompleted != 0 {
			t.Fatalf("traces_completed = %d, want 0 (no learning_record_emitted in fixture window)", evidence.OperationalEvidence.TracesCompleted)
		}
		if evidence.OperationalEvidence.LearningRecords != 0 {
			t.Fatalf("learning_records = %d, want 0", evidence.OperationalEvidence.LearningRecords)
		}
	})

	t.Run("production_not_advanced_on_false_evidence", func(t *testing.T) {
		// Corrected collector: PIPELINE_PROOF + zero L10 traces → still not shadow/live ready.
		// (Plan "NOT_READY" maps to this — auto-suggestion is PIPELINE_PROOF_READY, not SHADOW_READY.)
		if evidence.DetectedMode != "PIPELINE_PROOF" {
			t.Errorf("detected_mode = %q, want PIPELINE_PROOF", evidence.DetectedMode)
		}
		for _, advanced := range []string{"SHADOW_READY", "MICRO_CAPITAL_READY", "LIMITED_PRODUCTION_READY"} {
			if evidence.ProductionDecision == advanced {
				t.Errorf("production_decision = %q, must not advance on false lifecycle evidence", advanced)
			}
		}
		if evidence.ProductionDecision != "PIPELINE_PROOF_READY" && evidence.ProductionDecision != "NOT_READY" {
			t.Errorf("production_decision = %q, want PIPELINE_PROOF_READY or NOT_READY for this fixture", evidence.ProductionDecision)
		}
	})
}

func extractBriefSection(brief, startMarker, endMarker string) string {
	start := strings.Index(brief, startMarker)
	if start < 0 {
		return brief
	}
	end := strings.Index(brief[start:], endMarker)
	if end < 0 {
		return brief[start:]
	}
	return brief[start : start+end]
}
