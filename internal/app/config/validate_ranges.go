// F-SEC-06: bounded-range YAML validation.
//
// validateRanges walks the newly-added bounded keys in the loaded
// configuration and:
//
//   - returns a hard error for type-level violations (negative time
//     values, intervals that would cause a tight loop or infinite wait);
//   - emits a slog.Warn for first-tier soft violations (values out of
//     the documented [low, high] band but still numerically usable —
//     e.g. unknown_factor > 2 widens DQ exposure; reject_above > 1 is
//     ineffective; risk weight > 1 over-amplifies the aggregate).
//
// Called from Load() after a successful YAML parse + Validate(). Pure:
// the only side effect is structured logging — no globals are mutated.
package config

import (
	"fmt"
	"log/slog"
)

// validateRanges enforces additive bounded-range checks against `c`.
// `logger` MUST be non-nil; pass slog.Default() in production.
func (c *Config) validateRanges(logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	// ── feature.stability.min_consistency ∈ [0, 1] ────────────────
	if v := c.Feature.Stability.MinConsistency; v < 0 || v > 1 {
		logger.Warn("config_range_warning",
			"key", "feature.stability.min_consistency",
			"value", v,
			"expected_range", "[0, 1]",
		)
	}

	// ── data_quality.mode_profiles.* ──────────────────────────────
	for mode, prof := range c.DataQualityRuntime.ModeProfiles {
		// unknown_factor: hard-error if negative; warn if > 2.
		if prof.UnknownFactor < 0 {
			return fmt.Errorf(
				"config: data_quality.mode_profiles.%s.unknown_factor=%v must be >= 0",
				mode, prof.UnknownFactor,
			)
		}
		if prof.UnknownFactor > 2 {
			logger.Warn("config_range_warning",
				"key", fmt.Sprintf("data_quality.mode_profiles.%s.unknown_factor", mode),
				"value", prof.UnknownFactor,
				"expected_range", "[0, 2]",
			)
		}

		// reject_above: warn outside [0, 1] (>1 is ineffective).
		if prof.RejectAbove < 0 || prof.RejectAbove > 1 {
			logger.Warn("config_range_warning",
				"key", fmt.Sprintf("data_quality.mode_profiles.%s.reject_above", mode),
				"value", prof.RejectAbove,
				"expected_range", "[0, 1]",
			)
		}
	}

	// ── data_quality.risk_weights.* ∈ [0, 1] ──────────────────────
	// (Spec phrasing references `mode_profiles.*.risk_weights.*` but the
	// actual schema has risk_weights at the data_quality level — see
	// config/data_quality.yaml. Validate the canonical location.)
	rw := c.DataQualityRuntime.RiskWeights
	weights := map[string]float64{
		"honeypot":            rw.Honeypot,
		"tax_anomaly":         rw.TaxAnomaly,
		"rug_authority":       rw.RugAuthority,
		"lp_lock_missing":     rw.LpLockMissing,
		"wash_trading":        rw.WashTrading,
		"contract_unverified": rw.ContractUnverified,
		"fake_liquidity":      rw.FakeLiquidity,
	}
	for name, w := range weights {
		if w < 0 || w > 1 {
			logger.Warn("config_range_warning",
				"key", fmt.Sprintf("data_quality.risk_weights.%s", name),
				"value", w,
				"expected_range", "[0, 1]",
			)
		}
	}

	// ── validation.join_timeout_ms ∈ [0, 5000] ────────────────────
	// Hard-error on negative — that is a type-level violation
	// (would cause time.Duration to be negative).
	if c.Validation.JoinTimeoutMs < 0 {
		return fmt.Errorf(
			"config: validation.join_timeout_ms=%d must be >= 0",
			c.Validation.JoinTimeoutMs,
		)
	}
	if c.Validation.JoinTimeoutMs > 5000 {
		logger.Warn("config_range_warning",
			"key", "validation.join_timeout_ms",
			"value", c.Validation.JoinTimeoutMs,
			"expected_range", "[0, 5000]",
		)
	}

	// ── validation.join_poll_interval_ms ∈ [1, 1000] ──────────────
	// Hard-error on < 1: zero or negative would tight-loop the poller
	// (the worker clamps to 1ms but that defeats the bounded-wait
	// semantics — surface it instead of silently fixing it).
	if c.Validation.JoinPollIntervalMs < 1 {
		// Allow 0 only if the join wait itself is disabled (timeout=0).
		if !(c.Validation.JoinPollIntervalMs == 0 && c.Validation.JoinTimeoutMs == 0) {
			return fmt.Errorf(
				"config: validation.join_poll_interval_ms=%d must be >= 1 (or 0 with join_timeout_ms=0)",
				c.Validation.JoinPollIntervalMs,
			)
		}
	}
	if c.Validation.JoinPollIntervalMs > 1000 {
		logger.Warn("config_range_warning",
			"key", "validation.join_poll_interval_ms",
			"value", c.Validation.JoinPollIntervalMs,
			"expected_range", "[1, 1000]",
		)
	}

	// ── mode_adaptive.* — adaptive risk-appetite controller bounds ────
	// (operational-modes skill).
	ma := c.ModeAdaptive
	if ma.RugRateAutoDowngrade < 0 || ma.RugRateAutoDowngrade > 1 {
		return fmt.Errorf(
			"config: mode_adaptive.rug_rate_auto_downgrade=%v must be in [0, 1]",
			ma.RugRateAutoDowngrade,
		)
	}
	if ma.FPRateAutoDowngrade < 0 || ma.FPRateAutoDowngrade > 1 {
		return fmt.Errorf(
			"config: mode_adaptive.fp_rate_auto_downgrade=%v must be in [0, 1]",
			ma.FPRateAutoDowngrade,
		)
	}
	// Only enforce the timing/mode bounds when the controller is enabled
	// or any of the keys are explicitly set (>0). Zero-valued YAML for a
	// disabled controller is the documented "off" state and must not error.
	maConfigured := ma.Enabled ||
		ma.AdaptiveWindowSec != 0 ||
		ma.StarvationTriggerSec != 0 ||
		ma.TransitionWindowSec != 0 ||
		ma.DefaultStartupMode != ""
	if maConfigured {
		if ma.StarvationTriggerSec < 60 {
			return fmt.Errorf(
				"config: mode_adaptive.starvation_trigger_sec=%d must be >= 60",
				ma.StarvationTriggerSec,
			)
		}
		if ma.TransitionWindowSec < 60 {
			return fmt.Errorf(
				"config: mode_adaptive.transition_window_sec=%d must be >= 60",
				ma.TransitionWindowSec,
			)
		}
		switch ma.DefaultStartupMode {
		case "BALANCED", "STRICT", "EXPLORATION":
			// ok
		default:
			return fmt.Errorf(
				"config: mode_adaptive.default_startup_mode=%q must be one of {BALANCED, STRICT, EXPLORATION}",
				ma.DefaultStartupMode,
			)
		}
	}

	// ── baseline persistence (residual-risk #1) ───────────────────────
	// Both modules use the same minimums. 0 means "unset" → workers
	// substitute defaults (30s / 100 writes). Any explicit positive
	// value below the minimum is rejected to avoid tight-loop flushes
	// or vacuous bounded throughput.
	if v := c.Feature.BaselineFlushIntervalSec; v != 0 && v < 5 {
		return fmt.Errorf(
			"config: feature.baseline_flush_interval_sec=%d must be >= 5 (or 0 for default)",
			v,
		)
	}
	if v := c.Feature.BaselineFlushMaxWrites; v != 0 && v < 1 {
		return fmt.Errorf(
			"config: feature.baseline_flush_max_writes=%d must be >= 1 (or 0 for default)",
			v,
		)
	}
	if v := c.Edge.BaselineFlushIntervalSec; v != 0 && v < 5 {
		return fmt.Errorf(
			"config: edge.baseline_flush_interval_sec=%d must be >= 5 (or 0 for default)",
			v,
		)
	}
	if v := c.Edge.BaselineFlushMaxWrites; v != 0 && v < 1 {
		return fmt.Errorf(
			"config: edge.baseline_flush_max_writes=%d must be >= 1 (or 0 for default)",
			v,
		)
	}

	// ── learning.sybil_suspect_* (residual risk #5 / F-SEC-08) ────────
	// Hard-error on negatives; warn on out-of-band values that would
	// silently disable the flag.
	if v := c.Learning.SybilSuspectMinWallets; v < 0 {
		return fmt.Errorf(
			"config: learning.sybil_suspect_min_wallets=%d must be >= 0",
			v,
		)
	}
	if v := c.Learning.SybilSuspectMaxWashScore; v < 0 || v > 1 {
		return fmt.Errorf(
			"config: learning.sybil_suspect_max_wash_score=%v must be in [0, 1]",
			v,
		)
	}

	// ── execution_quality.alpha (residual risk #3) ────────────────────
	// Zero values are interpreted as "unset → use module defaults" so we
	// only validate explicitly-set positive values. AlphaMin/AlphaMax
	// share the relation AlphaMin < AlphaMax.
	a := c.ExecutionQuality.Alpha
	if a.AlphaMin < 0 {
		return fmt.Errorf(
			"config: execution_quality.alpha.alpha_min=%v must be >= 0",
			a.AlphaMin,
		)
	}
	if a.AlphaMax < 0 {
		return fmt.Errorf(
			"config: execution_quality.alpha.alpha_max=%v must be >= 0",
			a.AlphaMax,
		)
	}
	if a.AlphaMin > 0 && (a.AlphaMin < 0.1 || a.AlphaMin > 1.0) {
		logger.Warn("config_range_warning",
			"key", "execution_quality.alpha.alpha_min",
			"value", a.AlphaMin,
			"expected_range", "[0.1, 1.0]",
		)
	}
	if a.AlphaMax > 0 && (a.AlphaMax < 1.0 || a.AlphaMax > 10.0) {
		logger.Warn("config_range_warning",
			"key", "execution_quality.alpha.alpha_max",
			"value", a.AlphaMax,
			"expected_range", "[1.0, 10.0]",
		)
	}
	if a.AlphaMin > 0 && a.AlphaMax > 0 && a.AlphaMin >= a.AlphaMax {
		return fmt.Errorf(
			"config: execution_quality.alpha.alpha_min=%v must be < alpha_max=%v",
			a.AlphaMin, a.AlphaMax,
		)
	}
	if a.MinSampleCount < 0 {
		return fmt.Errorf(
			"config: execution_quality.alpha.min_sample_count=%d must be >= 0",
			a.MinSampleCount,
		)
	}
	if a.EwmaHalflifeSec < 0 {
		return fmt.Errorf(
			"config: execution_quality.alpha.ewma_halflife_sec=%d must be >= 0",
			a.EwmaHalflifeSec,
		)
	}
	if a.UpdateIntervalSec < 0 {
		return fmt.Errorf(
			"config: execution_quality.alpha.update_interval_sec=%d must be >= 0",
			a.UpdateIntervalSec,
		)
	}

	// ── probes.honeypot_sim.timeout_ms ∈ [100, 30000] (residual #4) ───
	// Hard-error on any explicitly-set value outside the band. Zero is
	// allowed and means "use the probe default" (2000ms).
	if v := c.Probes.HoneypotSim.TimeoutMs; v != 0 && (v < 100 || v > 30000) {
		return fmt.Errorf(
			"config: probes.honeypot_sim.timeout_ms=%d must be in [100, 30000] (or 0 for default)",
			v,
		)
	}

	// ── Phase 10: rescan structural invariants ─────────────────────
	if c.Rescan.Enabled {
		if err := validateRescan(c.Rescan); err != nil {
			return err
		}
	}

	return nil
}
