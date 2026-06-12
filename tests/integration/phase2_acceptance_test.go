package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const validatePhase2AcceptanceScript = "scripts/validate_phase2_acceptance.sh"

func runPhase2Acceptance(t *testing.T, root, evidencePath string) (output string, exitCode int) {
	t.Helper()
	cmd := exec.Command("bash", filepath.Join(root, validatePhase2AcceptanceScript), evidencePath)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	combined := string(out)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return combined, exitErr.ExitCode()
		}
		t.Fatalf("validate_phase2_acceptance.sh: %v\n%s", err, combined)
	}
	return combined, 0
}

func TestPhase2Acceptance_June20260612_PreFixBaseline_Fails(t *testing.T) {
	root := findRepoRoot(t)
	evidence := filepath.Join(root, june12GateEvidence)
	if _, err := os.Stat(evidence); err != nil {
		t.Skipf("June 12 evidence missing (%s)", june12GateEvidence)
	}

	out, code := runPhase2Acceptance(t, root, evidence)
	if code == 0 {
		t.Fatalf("pre-fix baseline must not pass Phase 2 acceptance, got: %s", out)
	}
	if !strings.Contains(out, "PHASE2_ACCEPTANCE: FAIL") {
		t.Errorf("expected PHASE2_ACCEPTANCE: FAIL, got:\n%s", out)
	}
}

func TestPhase2Acceptance_SyntheticFixture_Passes(t *testing.T) {
	root := findRepoRoot(t)
	evidence := filepath.Join(root, "tests/fixtures/gate_phase2_pass_evidence.json")

	out, code := runPhase2Acceptance(t, root, evidence)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, out)
	}
	if !strings.Contains(out, "PHASE2_ACCEPTANCE: PASS") {
		t.Errorf("expected PHASE2_ACCEPTANCE: PASS, got:\n%s", out)
	}
	if !strings.Contains(out, "PRODUCTION_DECISION: SHADOW_READY") {
		t.Error("missing PRODUCTION_DECISION: SHADOW_READY")
	}
}
