package gate_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/backend-dashboard/internal/api/gate"
)

func TestBriefHandler_ReturnsSnippet(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gate_brief_test.txt"), []byte("THROUGHPUT_VERDICT: HEALTHY"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := gate.NewBriefHandler(dir)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/gate/brief", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out contracts.GateBriefDTO
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Path == "" || out.Content == "" {
		t.Fatalf("brief = %+v", out)
	}
}
