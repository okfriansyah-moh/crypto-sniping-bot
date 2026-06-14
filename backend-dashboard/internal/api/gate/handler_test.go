package gate_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/backend-dashboard/internal/api/gate"
)

type gateStubDB struct {
	database.Adapter
	pipeline *database.PipelineStats
	dq       *database.DQBreakdown
}

func (s *gateStubDB) GetPipelineStats(context.Context, int) (*database.PipelineStats, error) {
	return s.pipeline, nil
}

func (s *gateStubDB) GetDQBreakdown(context.Context, int, string) (*database.DQBreakdown, error) {
	return s.dq, nil
}

func TestHandler_ReturnsThroughputVerdict(t *testing.T) {
	root := repoRoot(t)
	fixture := filepath.Join(root, "tests/fixtures/gate_phase2_pass_evidence.json")
	evidenceDir := t.TempDir()
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(evidenceDir, "gate_evidence_test.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	stub := &gateStubDB{
		pipeline: &database.PipelineStats{Detected: 20, Evaluated: 2},
		dq:       &database.DQBreakdown{PassCount: 2, RiskyPassCount: 2},
	}
	h := gate.NewHandler(stub, evidenceDir)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/gate/evidence", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var out contracts.GateEvidenceResponseDTO
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.ThroughputVerdict == "" {
		t.Fatal("throughput_verdict must be present")
	}
	if out.ThroughputVerdict != "HEALTHY" {
		t.Errorf("ThroughputVerdict = %q, want HEALTHY", out.ThroughputVerdict)
	}
	if out.DetectedMode != "PIPELINE_PROOF" {
		t.Errorf("DetectedMode = %q", out.DetectedMode)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
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
