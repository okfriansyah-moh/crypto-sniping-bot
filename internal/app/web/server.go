package web

import (
	"log/slog"
	"net/http"

	"crypto-sniping-bot/internal/app/config"
	healthEndpoint "crypto-sniping-bot/internal/modules/health/endpoint"
)

// Server is the HTTP server that aggregates all module endpoints.
type Server struct {
	cfg    *config.Config
	logger *slog.Logger
}

// NewServer creates a new HTTP server.
func NewServer(cfg *config.Config, logger *slog.Logger) *Server {
	return &Server{cfg: cfg, logger: logger}
}

// Router returns the HTTP handler with all routes registered.
func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()

	// Register module endpoints (vertical slice — each module owns its routes)
	healthEndpoint.Register(mux)

	// Future modules register here:
	// templateEndpoint.Register(mux)

	return mux
}
