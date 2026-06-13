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
	scenarioManifest        = "tests/fixtures/scenarios/manifest.json"
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
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var decoded struct {
		Scenarios []json.RawMessage `json:"scenarios"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if got := len(decoded.Scenarios); got != 11 {
		t.Errorf("expected 11 scenarios, got %d", got)
	}
}
