package gate

import (
	"net/http"

	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/operator"

	"crypto-sniping-bot/backend-dashboard/internal/httputil"
)

// Handler serves GET /api/v1/gate/evidence.
type Handler struct {
	db          database.Adapter
	evidenceDir string
}

// NewHandler wires the gate evidence vertical slice.
func NewHandler(db database.Adapter, evidenceDir string) *Handler {
	return &Handler{db: db, evidenceDir: evidenceDir}
}

// ServeHTTP returns gate review evidence JSON including throughput_verdict.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteMethodNotAllowed(w)
		return
	}

	out, err := operator.BuildGateEvidence(r.Context(), h.db, h.evidenceDir)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "gate evidence unavailable")
		return
	}

	httputil.WriteJSON(w, http.StatusOK, out)
}
