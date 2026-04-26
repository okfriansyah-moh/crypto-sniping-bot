package web_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/app/web"
)

func newTestServer(t *testing.T) *web.Server {
	t.Helper()
	cfg := &config.Config{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	return web.NewServer(cfg, logger)
}

func TestNewServer_ReturnsNonNil(t *testing.T) {
	// Arrange / Act
	s := newTestServer(t)

	// Assert
	if s == nil {
		t.Fatal("expected non-nil Server")
	}
}

func TestRouter_ReturnsNonNilHandler(t *testing.T) {
	// Arrange
	s := newTestServer(t)

	// Act
	h := s.Router()

	// Assert
	if h == nil {
		t.Fatal("expected non-nil http.Handler from Router()")
	}
}

func TestRouter_HealthRoute_Returns200(t *testing.T) {
	// Arrange
	s := newTestServer(t)
	h := s.Router()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	// Act
	h.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Errorf("GET /health: got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRouter_UnknownRoute_Returns404(t *testing.T) {
	// Arrange
	s := newTestServer(t)
	h := s.Router()
	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	rec := httptest.NewRecorder()

	// Act
	h.ServeHTTP(rec, req)

	// Assert
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /nonexistent: got status %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestSecurityHeaders_AllHeadersPresent(t *testing.T) {
	// Arrange
	s := newTestServer(t)
	h := s.Router()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	// Act
	h.ServeHTTP(rec, req)

	// Assert
	headers := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Cache-Control":           "no-store",
		"Referrer-Policy":         "no-referrer",
		"Content-Security-Policy": "default-src 'none'",
	}
	for name, want := range headers {
		got := rec.Header().Get(name)
		if got != want {
			t.Errorf("header %q: got %q, want %q", name, got, want)
		}
	}
}

func TestSecurityHeaders_AppliedToAllRoutes(t *testing.T) {
	// Arrange — use an unknown path (404 response) to confirm middleware wraps all paths
	s := newTestServer(t)
	h := s.Router()
	req := httptest.NewRequest(http.MethodGet, "/unknown-path", nil)
	rec := httptest.NewRecorder()

	// Act
	h.ServeHTTP(rec, req)

	// Assert
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("security headers not applied to 404 responses")
	}
}
