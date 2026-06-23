package rescan

import (
	"net/http"

	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/operator"

	"crypto-sniping-bot/backend-dashboard/internal/httputil"
)

// Handler serves GET /api/v1/rescan.
type Handler struct {
	db  database.Adapter
	cfg *config.Config
}

// NewHandler wires the rescan stats vertical slice.
func NewHandler(db database.Adapter, cfg *config.Config) *Handler {
	return &Handler{db: db, cfg: cfg}
}

// ServeHTTP returns rescan band emission stats as JSON.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteMethodNotAllowed(w)
		return
	}

	out, err := operator.BuildRescanStats(r.Context(), h.db, h.cfg)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "rescan stats unavailable")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, out)
}
