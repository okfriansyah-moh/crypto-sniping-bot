package web

import (
	"log/slog"
	"net/http"

	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/health"
	healthEndpoint "crypto-sniping-bot/internal/modules/health/endpoint"
)

// Server is the HTTP server that aggregates all module endpoints.
type Server struct {
	cfg        *config.Config
	logger     *slog.Logger
	adapter    database.Adapter
	shadowGate *health.ShadowGateEvaluator
}

// NewServer creates a new HTTP server.
func NewServer(cfg *config.Config, logger *slog.Logger, adapter database.Adapter) *Server {
	s := &Server{cfg: cfg, logger: logger, adapter: adapter}
	if cfg != nil && adapter != nil {
		s.shadowGate = health.NewShadowGateEvaluator(
			adapter,
			cfg.Execution.Mode,
			cfg.Execution.ShadowGate,
		)
	}
	return s
}

// Router returns the HTTP handler with all routes registered.
// All responses are wrapped with securityHeaders middleware.
func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()

	// Register module endpoints (vertical slice — each module owns its routes)
	var healthOpts *healthEndpoint.RegisterOpts
	if s.shadowGate != nil {
		healthOpts = &healthEndpoint.RegisterOpts{ShadowGate: s.shadowGate}
	}
	healthEndpoint.Register(mux, healthOpts)

	return securityHeaders(mux)
}

// securityHeaders is a middleware that attaches defensive HTTP response headers
// to every response, regardless of which handler produces it.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent MIME-type sniffing.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Disallow framing to mitigate clickjacking.
		w.Header().Set("X-Frame-Options", "DENY")
		// Instruct clients not to cache API responses.
		w.Header().Set("Cache-Control", "no-store")
		// Do not send the Referer header to third parties.
		w.Header().Set("Referrer-Policy", "no-referrer")
		// Minimal CSP — this server serves only JSON APIs, never HTML/scripts.
		w.Header().Set("Content-Security-Policy", "default-src 'none'")
		next.ServeHTTP(w, r)
	})
}
