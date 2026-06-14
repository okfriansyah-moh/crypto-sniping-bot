package configs_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/backend-dashboard/internal/api/configs"
)

func TestHandler_ReturnsManifestWithoutSecrets(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pipeline.yaml"), []byte("execution:\n  mode: shadow\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets.env"), []byte("API_KEY=supersecret"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := configs.NewHandler(dir)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/configs", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.Bytes()
	if strings.Contains(string(body), "supersecret") {
		t.Fatal("response must not contain raw secret values")
	}

	var entries []contracts.ConfigManifestEntryDTO
	if err := json.Unmarshal(body, &entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 1 || entries[0].Filename != "pipeline.yaml" {
		t.Fatalf("got %+v, want single pipeline.yaml entry", entries)
	}
	if len(entries[0].TopLevelKeys) == 0 || entries[0].SHA256Prefix == "" {
		t.Fatalf("manifest metadata missing: %+v", entries[0])
	}
}

func TestHandler_EmptyDirReturnsJSONArray(t *testing.T) {
	h := configs.NewHandler(t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/configs", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var entries []contracts.ConfigManifestEntryDTO
	if err := json.NewDecoder(rec.Body).Decode(&entries); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("want empty array, got %d", len(entries))
	}
}
