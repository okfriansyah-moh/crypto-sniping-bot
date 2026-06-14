package pnl

import (
	"net/http"

	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/operator"

	"crypto-sniping-bot/backend-dashboard/internal/httputil"
)

// Handler serves GET /api/v1/pnl.
type Handler struct {
	db database.Adapter
}

// NewHandler wires the PnL vertical slice.
func NewHandler(db database.Adapter) *Handler {
	return &Handler{db: db}
}

// ServeHTTP returns PnL summary JSON.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteMethodNotAllowed(w)
		return
	}

	windowHours := httputil.WindowHoursFromRequest(r)
	_ = r.URL.Query().Get("chain")  // reserved — PnL is portfolio-wide today
	_ = r.URL.Query().Get("market") // reserved for per-market drill-down in later tasks

	out, err := operator.BuildPnLSummary(r.Context(), h.db, windowHours)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "pnl unavailable")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, out)
}
