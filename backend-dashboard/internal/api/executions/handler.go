package executions

import (
	"net/http"

	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/operator"

	"crypto-sniping-bot/backend-dashboard/internal/httputil"
)

// Handler serves GET /api/v1/executions.
type Handler struct {
	db  database.Adapter
	cfg *config.Config
}

// NewHandler wires the executions trail vertical slice.
func NewHandler(db database.Adapter, cfg *config.Config) *Handler {
	return &Handler{db: db, cfg: cfg}
}

// ServeHTTP returns recent execution log rows as JSON.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteMethodNotAllowed(w)
		return
	}

	limit := httputil.LimitFromRequest(r, 20)
	out, err := operator.BuildExecutionLog(r.Context(), h.db, h.cfg, limit)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "executions unavailable")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, out)
}
