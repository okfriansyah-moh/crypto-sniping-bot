package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-sniping-bot/backend-dashboard/internal/auth"
	"crypto-sniping-bot/internal/app/config"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
}

func TestAPIKeyMiddleware_UnauthorizedWithoutKey(t *testing.T) {
	t.Setenv("DASHBOARD_API_KEY", "secret-key")

	h := auth.APIKeyMiddleware(auth.APIKeyConfig{
		APIKey:     "secret-key",
		FailClosed: false,
	}, nil)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAPIKeyMiddleware_AuthorizedWithDashboardHeader(t *testing.T) {
	h := auth.APIKeyMiddleware(auth.APIKeyConfig{
		APIKey:     "secret-key",
		FailClosed: false,
	}, nil)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	req.Header.Set("X-Dashboard-Key", "secret-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIKeyMiddleware_AuthorizedWithBearerToken(t *testing.T) {
	h := auth.APIKeyMiddleware(auth.APIKeyConfig{
		APIKey:     "secret-key",
		FailClosed: false,
	}, nil)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	req.Header.Set("Authorization", "Bearer secret-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestAPIKeyMiddleware_FailClosedWhenKeyUnset(t *testing.T) {
	h := auth.APIKeyMiddleware(auth.APIKeyConfig{
		APIKey:     "",
		FailClosed: true,
	}, nil)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	req.Header.Set("X-Dashboard-Key", "anything")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAPIKeyMiddleware_HealthPathExemptWithoutKey(t *testing.T) {
	h := auth.APIKeyMiddleware(auth.APIKeyConfig{
		APIKey:     "secret-key",
		FailClosed: false,
	}, auth.HealthPathExempt)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 without API key on /api/v1/health", rec.Code)
	}
}

func TestAPIKeyMiddleware_FailClosedHealthStillAllowed(t *testing.T) {
	h := auth.APIKeyMiddleware(auth.APIKeyConfig{
		APIKey:     "",
		FailClosed: true,
	}, auth.HealthPathExempt)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for exempt health when fail-closed", rec.Code)
	}
}

func TestCORSMiddleware_PreflightAllowedOrigin(t *testing.T) {
	h := auth.CORSMiddleware([]string{"http://localhost:5173"})(okHandler())

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/overview", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("Allow-Origin = %q", got)
	}
}

func TestWrapHandler_EnforcesAuthAndCacheControl(t *testing.T) {
	t.Setenv("DASHBOARD_API_KEY", "wrap-secret")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	h := auth.WrapHandler(mux, &config.DashboardConfig{
		CorsAllowedOrigins: []string{"http://localhost:5173"},
	})

	// unauthorized
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status = %d", rec.Code)
	}

	// authorized
	req = httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	req.Header.Set("X-Dashboard-Key", "wrap-secret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth status = %d", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", rec.Header().Get("Cache-Control"))
	}
}
