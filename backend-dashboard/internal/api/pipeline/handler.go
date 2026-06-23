package pipeline

import (
	"net/http"

	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/operator"

	"crypto-sniping-bot/backend-dashboard/internal/httputil"
)

// Handler serves GET /api/v1/pipeline.
type Handler struct {
	db database.Adapter
}

// NewHandler wires the pipeline vertical slice.
func NewHandler(db database.Adapter) *Handler {
	return &Handler{db: db}
}

// ServeHTTP returns pipeline funnel stats as JSON.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteMethodNotAllowed(w)
		return
	}

	windowHours := httputil.WindowHoursFromRequest(r)
	chain := r.URL.Query().Get("chain")
	_ = r.URL.Query().Get("market") // reserved for per-market drill-down in later tasks

	out, err := operator.BuildPipelineStats(r.Context(), h.db, windowHours, chain)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "pipeline stats unavailable")
		return
	}
	if ps, psErr := operator.BuildProbePendingStats(r.Context(), h.db); psErr == nil {
		out.ProbePending = ps
	}

	httputil.WriteJSON(w, http.StatusOK, out)
}
