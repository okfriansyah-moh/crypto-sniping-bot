package endpoint

import (
	"net/http"

	"crypto-sniping-bot/internal/modules/health/feature/check"
)

// Register wires the health module's HTTP routes.
// Each module owns its route registration — vertical slice pattern.
func Register(mux *http.ServeMux) {
	svc := check.NewService()
	handler := check.NewHandler(svc)

	mux.HandleFunc("GET /health", handler.Handle)
}
