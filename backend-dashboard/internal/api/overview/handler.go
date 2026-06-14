package overview

import (
	"net/http"
	"strings"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/operator"

	"crypto-sniping-bot/backend-dashboard/internal/httputil"
)

// Handler serves GET /api/v1/overview.
type Handler struct {
	db        database.Adapter
	cfg       *config.Config
	startTime time.Time
}

// NewHandler wires the overview vertical slice.
func NewHandler(db database.Adapter, cfg *config.Config, startTime time.Time) *Handler {
	return &Handler{db: db, cfg: cfg, startTime: startTime}
}

// ServeHTTP returns overview KPIs as JSON.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteMethodNotAllowed(w)
		return
	}

	out, err := operator.BuildOverview(r.Context(), h.db, h.cfg, h.startTime)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "overview unavailable")
		return
	}

	chain := r.URL.Query().Get("chain")
	_ = r.URL.Query().Get("market") // reserved for per-market drill-down in later tasks
	out.ChainStatuses = filterChainStatuses(out.ChainStatuses, chain)

	httputil.WriteJSON(w, http.StatusOK, out)
}

func filterChainStatuses(statuses []contracts.ChainStatusDTO, chain string) []contracts.ChainStatusDTO {
	c := strings.ToLower(strings.TrimSpace(chain))
	if c == "" || c == "all" {
		return statuses
	}
	filtered := make([]contracts.ChainStatusDTO, 0, len(statuses))
	for _, s := range statuses {
		if strings.EqualFold(s.Chain, c) {
			filtered = append(filtered, s)
		}
	}
	return filtered
}
