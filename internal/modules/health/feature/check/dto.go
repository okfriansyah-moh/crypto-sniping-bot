package check

// DTO defines the data transfer objects for the health check feature.

// ShadowGateResponse is the shadow live-flip readiness block on GET /health.
type ShadowGateResponse struct {
	Pass               bool    `json:"pass"`
	TradeCount         int     `json:"trade_count"`
	AggregatePnlBps    float64 `json:"aggregate_pnl_bps"`
	AvgPnlBps          float64 `json:"avg_pnl_bps"`
	MinTrades          int     `json:"min_trades"`
	MinWindowDays      int     `json:"min_window_days"`
	MinAggregatePnlBps float64 `json:"min_aggregate_pnl_bps"`
	ExecutionMode      string  `json:"execution_mode"`
	LiveFlipHint       string  `json:"live_flip_hint"`
}

// Response is the health check response DTO.
type Response struct {
	Status     string              `json:"status"`
	Version    string              `json:"version"`
	ShadowGate *ShadowGateResponse `json:"shadow_gate,omitempty"`
}
