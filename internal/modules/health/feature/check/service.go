package check

import (
	"context"

	"crypto-sniping-bot/internal/modules/health"
)

// Service implements the health check business logic.
type Service struct {
	shadowGate func(ctx context.Context) (health.ShadowGateResult, error)
}

// ServiceOption configures the health check service.
type ServiceOption func(*Service)

// WithShadowGateEvaluator wires shadow live-flip metrics into GET /health.
func WithShadowGateEvaluator(ev *health.ShadowGateEvaluator) ServiceOption {
	return func(s *Service) {
		if ev == nil {
			return
		}
		s.shadowGate = ev.Evaluate
	}
}

// NewService creates a new health check service.
func NewService(opts ...ServiceOption) *Service {
	s := &Service{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Execute performs the health check.
func (s *Service) Execute(ctx context.Context) Response {
	resp := Response{
		Status:  "ok",
		Version: "1.0.0",
	}
	if s.shadowGate != nil && ctx != nil {
		gate, err := s.shadowGate(ctx)
		if err == nil {
			resp.ShadowGate = &ShadowGateResponse{
				Pass:               gate.Pass,
				TradeCount:         gate.TradeCount,
				AggregatePnlBps:    gate.AggregatePnlBps,
				AvgPnlBps:          gate.AvgPnlBps,
				MinTrades:          gate.MinTrades,
				MinWindowDays:      gate.MinWindowDays,
				MinAggregatePnlBps: gate.MinAggregatePnlBps,
				ExecutionMode:      gate.ExecutionMode,
				LiveFlipHint:       gate.LiveFlipHint,
			}
		}
	}
	return resp
}
