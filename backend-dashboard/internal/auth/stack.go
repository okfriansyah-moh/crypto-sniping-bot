package auth

import (
	"net/http"

	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/app/web"
)

// WrapHandler applies dashboard security, CORS, and API-key middleware.
// Route registration (Task 10+) mounts on the inner mux before calling this.
func WrapHandler(inner http.Handler, dashCfg *config.DashboardConfig) http.Handler {
	if inner == nil {
		inner = http.NewServeMux()
	}

	h := inner
	h = APIKeyMiddleware(APIKeyConfig{
		APIKey:     config.DashboardAPIKey(),
		FailClosed: config.DashboardAuthFailClosed(),
	}, HealthPathExempt)(h)
	if dashCfg != nil {
		h = CORSMiddleware(dashCfg.CorsAllowedOrigins)(h)
	}
	return web.APIResponseHeaders(h)
}
