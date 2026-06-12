package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const validatePipelineProofScript = "scripts/validate_pipeline_proof.sh"

func runValidatePipelineProof(t *testing.T, root, evidencePath string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command("bash", filepath.Join(root, validatePipelineProofScript), evidencePath)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	combined := string(out)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return combined, combined, exitErr.ExitCode()
		}
		t.Fatalf("validate_pipeline_proof.sh: %v\n%s", err, combined)
	}
	return combined, "", 0
}

func TestValidatePipelineProof_June20260612Fixture_Fails(t *testing.T) {
	root := findRepoRoot(t)
	evidence := filepath.Join(root, june12GateEvidence)
	if _, err := os.Stat(evidence); err != nil {
		t.Skipf("June 12 evidence missing (%s) — run gate_review_collect --analyze first", june12GateEvidence)
	}

	stdout, _, code := runValidatePipelineProof(t, root, evidence)
	if code == 0 {
		t.Fatalf("expected exit 1 for pre-fix capture, got 0: %s", stdout)
	}
	if !strings.Contains(stdout, "traces_completed=0") {
		t.Errorf("expected fail reason traces_completed=0, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "PRODUCTION_DECISION: NOT_READY") {
		t.Error("missing PRODUCTION_DECISION: NOT_READY")
	}
}

func TestValidatePipelineProof_SyntheticEvidence_Passes(t *testing.T) {
	root := findRepoRoot(t)
	evidence := filepath.Join(root, "tests/fixtures/gate_pipeline_proof_pass_evidence.json")

	stdout, _, code := runValidatePipelineProof(t, root, evidence)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, stdout)
	}
	if !strings.Contains(stdout, "PRODUCTION_DECISION: SHADOW_READY") {
		t.Errorf("expected SHADOW_READY, got:\n%s", stdout)
	}
}

func TestValidatePipelineProof_SyntheticLog_EndToEnd_Passes(t *testing.T) {
	root := findRepoRoot(t)
	rawLog := filepath.Join(root, "tests/fixtures/gate_pipeline_proof_pass.log")

	cmd := exec.Command("bash", filepath.Join(root, gateReviewScript), "--analyze", rawLog)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gate_review_collect --analyze fixture: %v\n%s", err, out)
	}

	// gate_review_collect derives timestamp from filename gate_raw_*; fixture uses plain name.
	// Re-analyze writes gate_evidence_reanalysis_*.json — pick newest evidence under output/logs.
	evidenceDir := filepath.Join(root, "output/logs")
	entries, err := filepath.Glob(filepath.Join(evidenceDir, "gate_evidence_*.json"))
	if err != nil || len(entries) == 0 {
		t.Fatal("no gate evidence written after fixture analyze")
	}
	newest := entries[0]
	newestMtime := int64(0)
	for _, e := range entries {
		info, statErr := os.Stat(e)
		if statErr != nil {
			continue
		}
		if info.ModTime().UnixNano() >= newestMtime {
			newestMtime = info.ModTime().UnixNano()
			newest = e
		}
	}

	stdout, _, code := runValidatePipelineProof(t, root, newest)
	if code != 0 {
		t.Fatalf("expected exit 0 after synthetic full trace, got %d: %s", code, stdout)
	}
	if !strings.Contains(stdout, "PRODUCTION_DECISION: SHADOW_READY") {
		t.Errorf("expected SHADOW_READY, got:\n%s", stdout)
	}
}
