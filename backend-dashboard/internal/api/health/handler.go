package health

import (
	"net/http"

	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/health/feature/check"
	"crypto-sniping-bot/internal/operator"

	"crypto-sniping-bot/backend-dashboard/internal/httputil"
)

// Handler serves GET /api/v1/health — reuses the sniper health check service + shadow gate JSON.
type Handler struct {
	inner *check.Handler
}

// NewHandler wires the health vertical slice with optional shadow-gate evaluation.
func NewHandler(db database.Adapter, cfg *config.Config) *Handler {
	var svcOpts []check.ServiceOption
	if eval := operator.NewShadowGateEvaluator(db, cfg); eval != nil {
		svcOpts = append(svcOpts, check.WithShadowGateEvaluator(eval))
	}
	svc := check.NewService(svcOpts...)
	return &Handler{inner: check.NewHandler(svc)}
}

// ServeHTTP returns health status JSON (auth optional per §7.11).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httputil.WriteMethodNotAllowed(w)
		return
	}
	h.inner.Handle(w, r)
}
