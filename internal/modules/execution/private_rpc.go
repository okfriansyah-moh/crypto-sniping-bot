package execution

import "crypto-sniping-bot/internal/app/config"

// PrivateRPCRouter selects between public and private RPC routes based on
// allocation size.  Phase 4 stub — actual relay wiring (Flashbots, Beaverbuild)
// is implemented in the execution worker once private endpoints are live.
type PrivateRPCRouter struct {
	thresholdUsd float64
	endpoints    []string
}

// NewPrivateRPCRouter constructs a router from the execution config.
func NewPrivateRPCRouter(cfg *config.ExecutionConfig) *PrivateRPCRouter {
	if cfg == nil {
		return &PrivateRPCRouter{}
	}
	return &PrivateRPCRouter{
		thresholdUsd: cfg.PrivateRouteThresholdUsd,
		endpoints:    cfg.PrivateEndpoints,
	}
}

// Route returns true when the allocation should use the private mempool.
// Returns false when no private endpoints are configured (i.e., Phase 4 default).
func (r *PrivateRPCRouter) Route(sizeUsd float64) bool {
	if r == nil || len(r.endpoints) == 0 {
		return false
	}
	if r.thresholdUsd <= 0 {
		return false
	}
	return sizeUsd >= r.thresholdUsd
}

// Endpoints returns the configured private endpoints in declaration order.
func (r *PrivateRPCRouter) Endpoints() []string {
	if r == nil {
		return nil
	}
	out := make([]string, len(r.endpoints))
	copy(out, r.endpoints)
	return out
}
