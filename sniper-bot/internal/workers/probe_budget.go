package workers

import (
	"strings"
	"sync"
	"time"

	"crypto-sniping-bot/internal/app/config"
)

// probeBudget tracks rolling-hour token and credit consumption for market probes.
type probeBudget struct {
	mu sync.Mutex

	maxTokensPerHour  int
	maxCreditsPerHour int
	creditCosts       map[string]int

	freshTokenCap  int
	rescanTokenCap int

	windowStart time.Time
	tokenCount  int
	creditCount int
	freshCount  int
	rescanCount int
}

func newProbeBudget(cfg config.ProbesConfig) *probeBudget {
	costs := cfg.ProbeCreditCosts
	if len(costs) == 0 {
		costs = defaultProbeCreditCosts()
	}
	return &probeBudget{
		maxTokensPerHour:  cfg.MaxProbesPerHour,
		maxCreditsPerHour: cfg.MaxProbeCreditsPerHour,
		creditCosts:       costs,
		freshTokenCap:     cfg.RateLimitBuckets.FreshTokensPerHour,
		rescanTokenCap:    cfg.RateLimitBuckets.RescanTokensPerHour,
		windowStart:       time.Now().UTC(),
	}
}

func defaultProbeCreditCosts() map[string]int {
	return map[string]int{
		"solana_authorities":        1,
		"solana_pumpfun_lp":           1,
		"solana_holder_dist":          1,
		"solana_metadata":             0,
		"solana_creator_reputation":     0,
		"evm_pair_reserves":           1,
		"honeypot_sim":                1,
	}
}

func (b *probeBudget) resetIfNeeded(now time.Time) {
	if now.Sub(b.windowStart) >= time.Hour {
		b.windowStart = now
		b.tokenCount = 0
		b.creditCount = 0
		b.freshCount = 0
		b.rescanCount = 0
	}
}

func isRescanTransport(transport string) bool {
	return strings.HasPrefix(transport, "rescan_")
}

// tryConsume returns false when the token must be deferred (budget exhausted).
func (b *probeBudget) tryConsume(transport string, probeNames []string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now().UTC()
	b.resetIfNeeded(now)

	rescan := isRescanTransport(transport)
	if rescan && b.rescanTokenCap > 0 && b.rescanCount >= b.rescanTokenCap {
		return false
	}
	if !rescan && b.freshTokenCap > 0 && b.freshCount >= b.freshTokenCap {
		return false
	}

	estCredits := 0
	for _, name := range probeNames {
		if c, ok := b.creditCosts[name]; ok {
			estCredits += c
		}
	}
	if b.maxCreditsPerHour > 0 && b.creditCount+estCredits > b.maxCreditsPerHour {
		return false
	}
	if b.maxCreditsPerHour == 0 && b.maxTokensPerHour > 0 && b.tokenCount >= b.maxTokensPerHour {
		return false
	}

	b.tokenCount++
	b.creditCount += estCredits
	if rescan {
		b.rescanCount++
	} else {
		b.freshCount++
	}
	return true
}

// nextHourBoundary returns the UTC instant when the current rolling window resets.
func (b *probeBudget) nextHourBoundary() time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.windowStart.Add(time.Hour)
}
