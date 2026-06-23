package posture

import (
	"net/http"

	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/operator"

	"crypto-sniping-bot/backend-dashboard/internal/httputil"
)

// Handler serves GET /api/v1/posture.
type Handler struct {
	db          database.Adapter
	cfg         *config.Config
	evidenceDir string
}

// NewHandler wires the fortress posture vertical slice.
func NewHandler(db database.Adapter, cfg *config.Config, evidenceDir string) *Handler {
	return &Handler{db: db, cfg: cfg, evidenceDir: evidenceDir}
}

// ServeHTTP returns fortress posture JSON.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteMethodNotAllowed(w)
		return
	}

	out, err := operator.BuildFortressPosture(r.Context(), h.db, h.cfg, h.evidenceDir)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "posture unavailable")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, out)
}
