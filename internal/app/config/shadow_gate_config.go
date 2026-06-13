package config

// ShadowGateConfig defines the operator gate before flipping execution.mode to live.
// The bot never auto-promotes — operators review metrics and change YAML manually.
type ShadowGateConfig struct {
	MinTrades          int     `yaml:"min_trades"`
	MinWindowDays      int     `yaml:"min_window_days"`
	MinAggregatePnlBps float64 `yaml:"min_aggregate_pnl_bps"`
}

// DefaultShadowGateConfig returns production-gate defaults from docs/plans/2026-06-10-profit-restoration-plan.md Task 11.
func DefaultShadowGateConfig() ShadowGateConfig {
	return ShadowGateConfig{
		MinTrades:          30,
		MinWindowDays:      14,
		MinAggregatePnlBps: 0,
	}
}

func applyShadowGateDefaults(g *ShadowGateConfig) {
	def := DefaultShadowGateConfig()
	if g.MinTrades <= 0 {
		g.MinTrades = def.MinTrades
	}
	if g.MinWindowDays <= 0 {
		g.MinWindowDays = def.MinWindowDays
	}
	// MinAggregatePnlBps may be 0 intentionally (positive aggregate required at evaluate time).
}
