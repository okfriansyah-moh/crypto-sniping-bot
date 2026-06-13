package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	scenarioManifest      = "tests/fixtures/scenarios/manifest.json"
	validateScenariosScript = "scripts/validate_pipeline_scenarios.sh"
)

func TestBattleTest_AllScenariosPass(t *testing.T) {
	root := findRepoRoot(t)
	manifest := filepath.Join(root, scenarioManifest)
	if _, err := os.Stat(manifest); err != nil {
		t.Fatalf("scenario manifest missing: %v", err)
	}

	cmd := exec.Command("bash", filepath.Join(root, validateScenariosScript))
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	combined := string(out)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("battle-test failed (exit %d):\n%s", exitErr.ExitCode(), combined)
		}
		t.Fatalf("validate_pipeline_scenarios.sh: %v\n%s", err, combined)
	}
	if !strings.Contains(combined, "BATTLE_TEST_CERTIFICATION: READY") {
		t.Errorf("expected BATTLE_TEST_CERTIFICATION: READY, got:\n%s", combined)
	}
}

func TestBattleTest_Manifest_HasElevenScenarios(t *testing.T) {
	root := findRepoRoot(t)
	manifest := filepath.Join(root, scenarioManifest)
	cmd := exec.Command("jq", "-r", ".scenarios | length", manifest)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("jq manifest: %v", err)
	}
	if strings.TrimSpace(string(out)) != "11" {
		t.Errorf("expected 11 scenarios, got %q", strings.TrimSpace(string(out)))
	}
}
