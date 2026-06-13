package config

import "strings"

// PriorityConfig holds operational-mode thresholds from config/priority.yaml.
// Consumed by validation (L5), edge (L3), and selection (L6) workers via
// Config.ResolveModeThresholds — see docs/plans/2026-06-10-profit-restoration-plan.md Task 2–4.
type PriorityConfig struct {
	Modes PriorityModesConfig `yaml:"modes"`

	// ActiveMode is the YAML default; runtime mode comes from system state
	// (adaptive controller or /mode Telegram command).
	ActiveMode string `yaml:"active_mode"`

	StarvationThresholdTradesPerHour int `yaml:"starvation_threshold_trades_per_hour"`
	ModeTransitionCooldownMinutes    int `yaml:"mode_transition_cooldown_minutes"`

	Allocation PriorityAllocationConfig `yaml:"allocation"`
}

// PriorityModesConfig maps each operational mode to its threshold profile.
type PriorityModesConfig struct {
	Strict          ModeThresholdProfile `yaml:"strict"`
	Balanced        ModeThresholdProfile `yaml:"balanced"`
	Exploration     ModeThresholdProfile `yaml:"exploration"`
	VeryExploration ModeThresholdProfile `yaml:"very_exploration"`
}

// ModeThresholdProfile is one row from priority.modes.* in priority.yaml.
type ModeThresholdProfile struct {
	ExploreBudgetPct float64 `yaml:"explore_budget_pct"`
	EdgeStrengthMin  float64 `yaml:"edge_strength_min"`
	EvThresholdBps   int32   `yaml:"ev_threshold_bps"`
	MaxPositions     int     `yaml:"max_positions"`
}

// ModeThresholds is the resolved runtime view for a single operational mode.
type ModeThresholds struct {
	Mode             string
	ExploreBudgetPct float64
	EdgeStrengthMin  float64
	EvThresholdBps   int32
	MaxPositions     int
}

// PriorityAllocationConfig holds capital-allocation weights from priority.yaml.
// Fixed $5 entry sizing (docs/plans/2026-06-10-profit-restoration-plan.md) is unchanged; these weights apply to
// portfolio-level exposure caps in the capital engine.
type PriorityAllocationConfig struct {
	BaseSizeUSD            float64 `yaml:"base_size_usd"`
	MaxPortfolioExposurePct float64 `yaml:"max_portfolio_exposure_pct"`
	MaxSinglePositionPct   float64 `yaml:"max_single_position_pct"`
	AbsoluteMaxPositionPct float64 `yaml:"absolute_max_position_pct"`
	MaxConcurrentPositions int     `yaml:"max_concurrent_positions"`
}

// ResolveModeThresholds maps an operational mode string to its threshold profile.
// Unknown or empty modes fail-closed to STRICT (docs/plans/2026-06-10-profit-restoration-plan.md §7.1).
func (c *Config) ResolveModeThresholds(mode string) ModeThresholds {
	if c == nil {
		return canonicalStrictThresholds()
	}
	normalized := normalizeOperationalMode(mode)
	switch normalized {
	case modeStrict, modeBalanced, modeExploration, modeVeryExploration:
		prof := c.profileForMode(normalized)
		if prof.EvThresholdBps == 0 {
			return c.strictModeThresholds()
		}
		return ModeThresholds{
			Mode:             normalized,
			ExploreBudgetPct: prof.ExploreBudgetPct,
			EdgeStrengthMin:  prof.EdgeStrengthMin,
			EvThresholdBps:   prof.EvThresholdBps,
			MaxPositions:     prof.MaxPositions,
		}
	default:
		return c.strictModeThresholds()
	}
}

// ResolveActiveModeThresholds resolves thresholds for Priority.ActiveMode.
func (c *Config) ResolveActiveModeThresholds() ModeThresholds {
	if c == nil {
		return canonicalStrictThresholds()
	}
	mode := c.Priority.ActiveMode
	if mode == "" {
		mode = modeBalanced
	}
	return c.ResolveModeThresholds(mode)
}

func (c *Config) profileForMode(mode string) ModeThresholdProfile {
	switch mode {
	case modeStrict:
		return c.Priority.Modes.Strict
	case modeBalanced:
		return c.Priority.Modes.Balanced
	case modeExploration:
		return c.Priority.Modes.Exploration
	case modeVeryExploration:
		return c.Priority.Modes.VeryExploration
	default:
		return ModeThresholdProfile{}
	}
}

func (c *Config) strictModeThresholds() ModeThresholds {
	prof := c.Priority.Modes.Strict
	if prof.EvThresholdBps == 0 {
		return canonicalStrictThresholds()
	}
	return ModeThresholds{
		Mode:             modeStrict,
		ExploreBudgetPct: prof.ExploreBudgetPct,
		EdgeStrengthMin:  prof.EdgeStrengthMin,
		EvThresholdBps:   prof.EvThresholdBps,
		MaxPositions:     prof.MaxPositions,
	}
}

func canonicalStrictThresholds() ModeThresholds {
	return ModeThresholds{
		Mode:             modeStrict,
		ExploreBudgetPct: 1.0,
		EdgeStrengthMin:  0.75,
		EvThresholdBps:   150,
		MaxPositions:     5,
	}
}

const (
	modeStrict          = "STRICT"
	modeBalanced        = "BALANCED"
	modeExploration     = "EXPLORATION"
	modeVeryExploration = "VERY_EXPLORATION"
)

// normalizeOperationalMode uppercases and maps YAML-style names to runtime constants.
func normalizeOperationalMode(mode string) string {
	m := strings.TrimSpace(strings.ToUpper(mode))
	m = strings.ReplaceAll(m, "-", "_")
	m = strings.ReplaceAll(m, " ", "_")
	switch m {
	case "VERY_EXPLORATION", "VERYEXPLORATION":
		return modeVeryExploration
	case "STRICT", "BALANCED", "EXPLORATION":
		return m
	default:
		return m
	}
}

// applyPriorityDefaults fills zero-value priority fields with canonical defaults
// from docs/reference/architecture.md §7 / config/priority.yaml.
func applyPriorityDefaults(p *PriorityConfig) {
	if p == nil {
		return
	}
	if p.Modes.Strict.EvThresholdBps == 0 {
		p.Modes.Strict = ModeThresholdProfile{
			ExploreBudgetPct: 1.0,
			EdgeStrengthMin:  0.75,
			EvThresholdBps:   150,
			MaxPositions:     5,
		}
	}
	if p.Modes.Balanced.EvThresholdBps == 0 {
		p.Modes.Balanced = ModeThresholdProfile{
			ExploreBudgetPct: 2.0,
			EdgeStrengthMin:  0.60,
			EvThresholdBps:   100,
			MaxPositions:     15,
		}
	}
	if p.Modes.Exploration.EvThresholdBps == 0 {
		p.Modes.Exploration = ModeThresholdProfile{
			ExploreBudgetPct: 5.0,
			EdgeStrengthMin:  0.45,
			EvThresholdBps:   60,
			MaxPositions:     20,
		}
	}
	if p.Modes.VeryExploration.EvThresholdBps == 0 {
		p.Modes.VeryExploration = ModeThresholdProfile{
			ExploreBudgetPct: 8.0,
			EdgeStrengthMin:  0.30,
			EvThresholdBps:   30,
			MaxPositions:     25,
		}
	}
	if p.ActiveMode == "" {
		p.ActiveMode = "balanced"
	}
}
