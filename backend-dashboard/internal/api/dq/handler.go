package dq

import (
	"net/http"

	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/operator"

	"crypto-sniping-bot/backend-dashboard/internal/httputil"
)

// Handler serves GET /api/v1/dq.
type Handler struct {
	db database.Adapter
}

// NewHandler wires the DQ breakdown vertical slice.
func NewHandler(db database.Adapter) *Handler {
	return &Handler{db: db}
}

// ServeHTTP returns DQ decision breakdown JSON.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteMethodNotAllowed(w)
		return
	}

	windowHours := httputil.WindowHoursFromRequest(r)
	chain := r.URL.Query().Get("chain")
	_ = r.URL.Query().Get("market") // reserved for per-market drill-down in later tasks

	out, err := operator.BuildDQBreakdown(r.Context(), h.db, windowHours, chain)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "dq breakdown unavailable")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, out)
}
