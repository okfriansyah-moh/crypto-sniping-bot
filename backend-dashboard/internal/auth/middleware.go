package auth

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

const (
	headerDashboardKey = "X-Dashboard-Key"
	healthAPIPath      = "/api/v1/health"
)

// HealthPathExempt skips API-key auth for GET /api/v1/health (Docker healthchecks).
func HealthPathExempt(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == healthAPIPath
}

// APIKeyConfig configures dashboard API-key authentication.
type APIKeyConfig struct {
	APIKey     string
	FailClosed bool
}

// APIKeyMiddleware rejects requests without a valid dashboard API key.
// Accepts X-Dashboard-Key or Authorization: Bearer <token>.
// When exempt returns true the request bypasses auth (e.g. GET /api/v1/health).
func APIKeyMiddleware(cfg APIKeyConfig, exempt func(*http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if exempt != nil && exempt(r) {
				next.ServeHTTP(w, r)
				return
			}

			if cfg.FailClosed || cfg.APIKey == "" {
				writeAuthError(w, http.StatusUnauthorized, "dashboard API key not configured")
				return
			}

			provided := extractAPIKey(r)
			if provided == "" || !secureCompare(provided, cfg.APIKey) {
				writeAuthError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func extractAPIKey(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get(headerDashboardKey)); v != "" {
		return v
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if strings.HasPrefix(auth, prefix) {
		return strings.TrimSpace(auth[len(prefix):])
	}
	return ""
}

func secureCompare(provided, expected string) bool {
	if provided == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func writeAuthError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
