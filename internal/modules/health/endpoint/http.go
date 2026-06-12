package endpoint

import (
	"net/http"

	"crypto-sniping-bot/internal/modules/health"
	"crypto-sniping-bot/internal/modules/health/feature/check"
)

// RegisterOpts optional dependencies for health routes.
type RegisterOpts struct {
	ShadowGate *health.ShadowGateEvaluator
}

// Register wires the health module's HTTP routes.
// Each module owns its route registration — vertical slice pattern.
func Register(mux *http.ServeMux, opts *RegisterOpts) {
	var svcOpts []check.ServiceOption
	if opts != nil && opts.ShadowGate != nil {
		svcOpts = append(svcOpts, check.WithShadowGateEvaluator(opts.ShadowGate))
	}
	svc := check.NewService(svcOpts...)
	handler := check.NewHandler(svc)

	mux.HandleFunc("GET /health", handler.Handle)
}
