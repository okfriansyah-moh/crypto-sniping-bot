// Package providers defines the external Data Quality provider interface
// and the parallel Aggregator that combines multiple provider scores into a
// single weighted ExternalRiskScore.
//
// Architecture rules:
//   - Providers are OPTIONAL enhancements to the existing internal detectors.
//   - If a provider fails or times out, the pipeline continues unaffected.
//   - shadow_mode: true → score recorded in DTO flags but does NOT affect RiskScore.
//   - All providers run in parallel within a shared budget.
package providers

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// DQSignalDTO carries a normalized risk signal from one external provider.
type DQSignalDTO struct {
	ProviderName string   // canonical lowercase name: "rugcheck", "social", …
	Score        float64  // [0.0, 1.0] — higher = riskier
	Flags        []string // provider-specific diagnostic codes
	Degraded     bool     // true when the provider returned partial/no data

	// Enrichment fields (P3 — BirdEye). Zero values indicate not populated.
	// CreatorRiskScore is the creator wallet's share of total supply as a risk
	// signal in [0, 1].  Zero when the provider did not populate it.
	CreatorRiskScore float64
	// LpLockPct is the percentage [0, 100] of LP tokens that are locked.
	// Zero when the provider did not populate it or the value is unknown.
	LpLockPct float64
}

// DataQualityProvider is the interface all external DQ signal sources must satisfy.
// Implementations MUST be:
//   - Timeout-safe (respect the parent context deadline).
//   - Fail-open (never block the pipeline on error — return degraded=true instead).
//   - Stateless within a single Evaluate call.
type DataQualityProvider interface {
	// Name returns the canonical lowercase provider name used in flags and logs.
	Name() string
	// Evaluate fetches/computes a risk signal for the given token.
	// MUST return within the parent context deadline.
	Evaluate(ctx context.Context, tokenAddress, chain string) (DQSignalDTO, error)
}

// ProviderEntry wires a DataQualityProvider with its configuration.
type ProviderEntry struct {
	Provider   DataQualityProvider
	Weight     float64 // relative weight within the aggregator's external score
	Enabled    bool
	ShadowMode bool // if true, flags are collected but Score does NOT contribute to the aggregate
}

// AggregateResult is the combined output from all providers for one token.
type AggregateResult struct {
	// ExternalRiskScore is the weighted-average risk score from all non-shadow
	// providers that returned a valid signal. Range [0.0, 1.0].
	ExternalRiskScore float64
	// Flags collects every provider-specific diagnostic code, prefixed with
	// the provider name (e.g. "rugcheck:FREEZE_AUTHORITY_ENABLED").
	Flags []string
	// Degraded is true when at least one enabled provider returned a partial
	// or no response (timeout, HTTP error, parse failure, etc.).
	Degraded bool

	// Enrichment fields (P3 — BirdEye). Taken from the first non-zero signal
	// across all providers that populate them.
	// CreatorRiskScore: creator wallet risk in [0, 1].
	CreatorRiskScore float64
	// LpLockPct: LP lock percentage [0, 100].
	LpLockPct float64
}

// Aggregator runs all enabled providers in parallel within a shared timeout
// budget and returns the weighted-average ExternalRiskScore.
type Aggregator struct {
	entries []ProviderEntry
	budget  time.Duration
	logger  *slog.Logger
}

// NewAggregator creates an Aggregator.
// budgetMs is the shared wall-clock budget for all providers in a single call.
// Individual providers must respect the context deadline; the Aggregator does
// not cancel individual providers early — the shared deadline covers all.
func NewAggregator(entries []ProviderEntry, budgetMs int, logger *slog.Logger) *Aggregator {
	if logger == nil {
		logger = slog.Default()
	}
	d := time.Duration(budgetMs) * time.Millisecond
	if d <= 0 {
		d = 300 * time.Millisecond
	}
	return &Aggregator{entries: entries, budget: d, logger: logger}
}

type providerResult struct {
	entry  ProviderEntry
	signal DQSignalDTO
	err    error
}

// Evaluate runs all enabled providers in parallel and returns the aggregate.
// Never returns an error — failures produce degraded signals.
func (a *Aggregator) Evaluate(ctx context.Context, tokenAddress, chain string) AggregateResult {
	ctx, cancel := context.WithTimeout(ctx, a.budget)
	defer cancel()

	active := make([]ProviderEntry, 0, len(a.entries))
	for _, e := range a.entries {
		if e.Enabled {
			active = append(active, e)
		}
	}
	if len(active) == 0 {
		return AggregateResult{}
	}

	results := make([]providerResult, len(active))
	var wg sync.WaitGroup
	for i, e := range active {
		i, e := i, e
		wg.Add(1)
		go func() {
			defer wg.Done()
			sig, err := e.Provider.Evaluate(ctx, tokenAddress, chain)
			results[i] = providerResult{entry: e, signal: sig, err: err}
		}()
	}
	wg.Wait()

	var totalWeight, weightedScore float64
	var flags []string
	degraded := false
	// Enrichment: first non-zero value wins (provider with richer data sets it).
	var creatorRiskScore, lpLockPct float64

	for _, r := range results {
		if r.err != nil {
			a.logger.Warn("dq_provider_degraded",
				"provider", r.entry.Provider.Name(),
				"token", tokenAddress,
				"chain", chain,
				"error", r.err.Error(),
			)
			flags = append(flags, "provider_degraded:"+r.entry.Provider.Name())
			degraded = true
			continue
		}
		if r.signal.Degraded {
			flags = append(flags, "provider_partial:"+r.entry.Provider.Name())
			degraded = true
		}
		flags = append(flags, r.signal.Flags...)

		if !r.entry.ShadowMode && !r.signal.Degraded {
			w := r.entry.Weight
			if w <= 0 {
				w = 1.0
			}
			totalWeight += w
			weightedScore += w * r.signal.Score
		}

		// Collect enrichment fields (first non-zero wins).
		if creatorRiskScore == 0 && r.signal.CreatorRiskScore > 0 {
			creatorRiskScore = r.signal.CreatorRiskScore
		}
		if lpLockPct == 0 && r.signal.LpLockPct > 0 {
			lpLockPct = r.signal.LpLockPct
		}
	}

	score := 0.0
	if totalWeight > 0 {
		score = weightedScore / totalWeight
	}
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	sort.Strings(flags)

	return AggregateResult{
		ExternalRiskScore: score,
		Flags:             flags,
		Degraded:          degraded,
		CreatorRiskScore:  creatorRiskScore,
		LpLockPct:         lpLockPct,
	}
}
