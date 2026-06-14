package positions

import (
	"net/http"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/operator"

	"crypto-sniping-bot/backend-dashboard/internal/httputil"
)

// Handler serves GET /api/v1/positions.
type Handler struct {
	db database.Adapter
}

// NewHandler wires the positions vertical slice.
func NewHandler(db database.Adapter) *Handler {
	return &Handler{db: db}
}

// ServeHTTP returns open position rows as a JSON array.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteMethodNotAllowed(w)
		return
	}

	chain := r.URL.Query().Get("chain")
	_ = r.URL.Query().Get("market") // reserved for per-market drill-down in later tasks

	rows, err := operator.BuildPositionRows(r.Context(), h.db, chain)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "positions unavailable")
		return
	}
	if rows == nil {
		rows = []contracts.PositionRowDTO{}
	}

	httputil.WriteJSON(w, http.StatusOK, rows)
}
