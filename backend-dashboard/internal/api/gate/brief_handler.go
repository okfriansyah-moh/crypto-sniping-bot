package gate

import (
	"net/http"

	"crypto-sniping-bot/internal/operator"

	"crypto-sniping-bot/backend-dashboard/internal/httputil"
)

// BriefHandler serves GET /api/v1/gate/brief.
type BriefHandler struct {
	logsDir string
}

// NewBriefHandler wires the gate brief vertical slice.
func NewBriefHandler(logsDir string) *BriefHandler {
	return &BriefHandler{logsDir: logsDir}
}

// ServeHTTP returns the latest gate_brief file path and content snippet.
func (h *BriefHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteMethodNotAllowed(w)
		return
	}

	out, err := operator.BuildGateBrief(h.logsDir)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "gate brief unavailable")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, out)
}
