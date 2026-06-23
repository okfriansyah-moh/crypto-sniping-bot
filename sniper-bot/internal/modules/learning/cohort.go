// Package learning implements Layer 10: Learning Engine.
// All functions are pure — no database access, no side effects.
// Workers in internal/workers/ orchestrate adapter calls around these functions.
package learning

import "strings"

// CohortLabel derives a cohort string from feature signals.
// Format: "liquidity_bucket:age_bucket:source"
// where liquidity_bucket ∈ {low,mid,high}, age_bucket ∈ {new,young,mature}.
func CohortLabel(liquidityScore float64, tokenAgeSeconds int64, source string) string {
	return liquidityBucket(liquidityScore) + ":" + ageBucket(tokenAgeSeconds) + ":" + sanitizeSource(source)
}

func liquidityBucket(score float64) string {
	switch {
	case score >= 0.7:
		return "high"
	case score >= 0.35:
		return "mid"
	default:
		return "low"
	}
}

func ageBucket(ageSeconds int64) string {
	switch {
	case ageSeconds < 300:
		return "new" // < 5 min
	case ageSeconds < 3600:
		return "young" // 5 min – 1 h
	default:
		return "mature"
	}
}

func sanitizeSource(s string) string {
	if s == "" {
		return "unknown"
	}
	// Replace non-alphanumeric/hyphen/underscore chars.
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}
