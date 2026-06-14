// Package web provides the sniper-bot HTTP surface.
//
// The trading process exposes exactly one route: GET /health (plus 404 for all
// other paths). Operator dashboard REST lives exclusively on backend-dashboard
// :8090 — see docs/plans/2026-06-13-operator-dashboard-plan.md §7.10.
package web

import (
	"log/slog"
	"net/http"

	"crypto-sniping-bot/shared/database"
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
// Sniper-bot registers GET /health only; dashboard /api/v1/* routes are not mounted here.
// All responses are wrapped with securityHeaders middleware.
func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()

	// Health probe + shadow_gate JSON block (Docker / orchestrator liveness).
	var healthOpts *healthEndpoint.RegisterOpts
	if s.shadowGate != nil {
		healthOpts = &healthEndpoint.RegisterOpts{ShadowGate: s.shadowGate}
	}
	healthEndpoint.Register(mux, healthOpts)

	return SecurityHeaders(mux)
}
