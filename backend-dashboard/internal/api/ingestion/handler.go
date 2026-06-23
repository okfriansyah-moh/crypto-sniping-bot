package ingestion

import (
	"net/http"

	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/operator"

	"crypto-sniping-bot/backend-dashboard/internal/httputil"
)

// Handler serves GET /api/v1/ingestion.
type Handler struct {
	cfg *config.Config
}

// NewHandler wires the ingestion status vertical slice.
func NewHandler(cfg *config.Config) *Handler {
	return &Handler{cfg: cfg}
}

// ServeHTTP returns Solana ingestion config status as JSON.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteMethodNotAllowed(w)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, operator.BuildIngestionStatus(h.cfg))
}
