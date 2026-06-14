package operator_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/operator"
)

func TestBuildGateEvidence_ParsesFixture(t *testing.T) {
	t.Parallel()
	root := findRepoRoot(t)
	fixture := filepath.Join(root, "tests/fixtures/gate_phase2_pass_evidence.json")
	evidenceDir := t.TempDir()
	dest := filepath.Join(evidenceDir, "gate_evidence_fixture.json")
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		t.Fatalf("write temp evidence: %v", err)
	}

	stub := &overviewStubDB{
		pipeline: &database.PipelineStats{Detected: 20, Evaluated: 2},
		dq: &database.DQBreakdown{
			PassCount:      2,
			RiskyPassCount: 2,
		},
	}

	got, err := operator.BuildGateEvidence(context.Background(), stub, evidenceDir)
	if err != nil {
		t.Fatalf("BuildGateEvidence: %v", err)
	}
	if got.DetectedMode != "PIPELINE_PROOF" {
		t.Fatalf("DetectedMode = %q", got.DetectedMode)
	}
	if got.WSOLTokenAddressEmitted != 0 {
		t.Fatalf("WSOL = %d, want 0", got.WSOLTokenAddressEmitted)
	}
	if got.TracesCompleted < 1 {
		t.Fatalf("TracesCompleted = %d, want >=1", got.TracesCompleted)
	}
	if got.DQPassOrRiskyPass < 3 {
		t.Fatalf("DQPassOrRiskyPass = %d, want >=3", got.DQPassOrRiskyPass)
	}
	if got.ThroughputVerdict != "HEALTHY" {
		t.Fatalf("ThroughputVerdict = %q, want HEALTHY", got.ThroughputVerdict)
	}
	if len(got.Criteria) < 5 {
		t.Fatalf("expected criteria rows, got %d", len(got.Criteria))
	}
}

func TestBuildGateEvidence_MissingFileGraceful(t *testing.T) {
	t.Parallel()
	got, err := operator.BuildGateEvidence(context.Background(), &overviewStubDB{}, t.TempDir())
	if err != nil {
		t.Fatalf("BuildGateEvidence: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil empty response")
	}
	if got.ThroughputVerdict != "MARKET_QUIET" {
		t.Fatalf("ThroughputVerdict = %q, want MARKET_QUIET for empty state", got.ThroughputVerdict)
	}
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
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
