package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-sniping-bot/internal/modules/health/endpoint"
)

func TestHealthCheck(t *testing.T) {
	mux := http.NewServeMux()
	endpoint.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got '%s'", resp["status"])
	}
}
