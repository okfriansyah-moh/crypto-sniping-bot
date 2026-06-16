package probepending

import (
	"net/http"

	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/operator"

	"crypto-sniping-bot/backend-dashboard/internal/httputil"
)

// Handler serves GET /api/v1/probes/pending.
type Handler struct {
	db database.Adapter
}

// NewHandler wires the probe pending stats endpoint.
func NewHandler(db database.Adapter) *Handler {
	return &Handler{db: db}
}

// ServeHTTP returns probe pending queue depth as JSON.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteMethodNotAllowed(w)
		return
	}

	out, err := operator.BuildProbePendingStats(r.Context(), h.db)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "probe pending stats unavailable")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, out)
}
