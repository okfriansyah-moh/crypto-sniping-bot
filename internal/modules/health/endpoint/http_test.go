package endpoint_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-sniping-bot/internal/modules/health/endpoint"
)

func TestRegister_HealthRoute_Returns200(t *testing.T) {
	// Arrange
	mux := http.NewServeMux()
	endpoint.Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	// Act
	mux.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Errorf("GET /health: got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRegister_PostMethod_NotAllowed(t *testing.T) {
	// Arrange — route is registered as GET only
	mux := http.NewServeMux()
	endpoint.Register(mux)
	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rec := httptest.NewRecorder()

	// Act
	mux.ServeHTTP(rec, req)

	// Assert — Go's ServeMux returns 405 for wrong method on a pattern-matched route
	if rec.Code == http.StatusOK {
		t.Errorf("POST /health should not return 200")
	}
}

func TestRegister_ResponseBody_ContainsStatus(t *testing.T) {
	// Arrange
	mux := http.NewServeMux()
	endpoint.Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	// Act
	mux.ServeHTTP(rec, req)

	// Assert — body must be non-empty
	body := rec.Body.String()
	if len(body) == 0 {
		t.Error("expected non-empty response body from /health")
	}
}
