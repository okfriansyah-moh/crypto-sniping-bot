// Phase 9 (Profitability Restoration § 9.4) — cohort multiplier defaults.
package capital

import "crypto-sniping-bot/internal/app/config"

// CohortMultiplier returns the size multiplier for the supplied cohort
// label. The cohort table itself is owned by the learning engine and
// updated out-of-band; this helper simply applies the default until a
// per-cohort lookup is wired through. Falls back to cfg.Cohort.DefaultMultiplier.
func CohortMultiplier(_ string, cfg *config.CapitalConfig) float64 {
	if cfg == nil {
		return 1.0
	}
	m := cfg.Cohort.DefaultMultiplier
	if m <= 0 {
		m = 1.0
	}
	if cfg.Cohort.MinMultiplier > 0 && m < cfg.Cohort.MinMultiplier {
		m = cfg.Cohort.MinMultiplier
	}
	if cfg.Cohort.MaxMultiplier > 0 && m > cfg.Cohort.MaxMultiplier {
		m = cfg.Cohort.MaxMultiplier
	}
	return m
}
