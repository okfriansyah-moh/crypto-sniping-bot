package activity

import (
	"net/http"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/operator"

	"crypto-sniping-bot/backend-dashboard/internal/httputil"
)

// Handler serves GET /api/v1/activity.
type Handler struct {
	db              database.Adapter
	defaultEventCap int
}

// NewHandler wires the activity feed vertical slice.
func NewHandler(db database.Adapter, defaultEventCap int) *Handler {
	return &Handler{db: db, defaultEventCap: defaultEventCap}
}

// ServeHTTP returns recent event bus rows as a JSON array.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteMethodNotAllowed(w)
		return
	}

	chain := r.URL.Query().Get("chain")
	limit := httputil.LimitFromRequest(r, h.defaultEventCap)
	_ = r.URL.Query().Get("market") // reserved for per-market drill-down in later tasks

	rows, err := operator.BuildActivityFeed(r.Context(), h.db, chain, limit)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "activity feed unavailable")
		return
	}
	if rows == nil {
		rows = []contracts.ActivityEventDTO{}
	}

	httputil.WriteJSON(w, http.StatusOK, rows)
}
