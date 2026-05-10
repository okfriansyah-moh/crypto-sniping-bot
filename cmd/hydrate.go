package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database/engines/postgres"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/app/logging"
)

// hydrate.go — Seed the historical_market_profiles table.
//
// Usage: crypto-sniping-bot hydrate [--seeds=<path>]
//
// Reads config/historical_seeds.yaml, computes per-cohort percentile
// statistics, and upserts them into the historical_market_profiles table
// via the database adapter. Idempotent: safe to re-run after adding
// more seed tokens.
//
// Architecture note: This is a CLI command, NOT a pipeline module.
// It calls the adapter directly (permitted for CLI tooling) and does not
// emit events to the event bus.

// ── Seed YAML schema ─────────────────────────────────────────────────────────

type seedsFile struct {
	Version     string               `yaml:"version"`
	GeneratedAt string               `yaml:"generated_at"`
	CohortDefs  map[string]cohortDef `yaml:"cohort_definitions"`
	Tokens      []seedToken          `yaml:"tokens"`
}

type cohortDef struct {
	PriorProbability float64 `yaml:"prior_probability"`
	LiquidityMinUsd  float64 `yaml:"liquidity_min_usd"`
	ATHMultipleP50   float64 `yaml:"ath_multiple_p50"`
	TimeToRugP10Sec  float64 `yaml:"time_to_rug_p10_sec"`
}

type seedToken struct {
	Symbol       string  `yaml:"symbol"`
	Name         string  `yaml:"name"`
	Tier         int     `yaml:"tier"`
	CohortKey    string  `yaml:"cohort_key"`
	Address      string  `yaml:"address"`
	Chain        string  `yaml:"chain"`
	MarketCapUsd float64 `yaml:"market_cap_usd"`
	LiquidityUsd float64 `yaml:"liquidity_usd"`
	Volume24hUsd float64 `yaml:"volume_24h_usd"`
	TxnsBuys     int     `yaml:"txns_24h_buys"`
	TxnsSells    int     `yaml:"txns_24h_sells"`
	SocialCount  int     `yaml:"social_count"`
	HasWebsite   bool    `yaml:"has_website"`
	PairAgeDays  float64 `yaml:"pair_age_days"`
	Legitimate   bool    `yaml:"legitimate"`
}

// ── Entry point ──────────────────────────────────────────────────────────────

func runHydrate() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfgPath, err := findConfigPath()
	if err != nil {
		logger.Error("config_not_found", "error", err)
		os.Exit(1)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		logger.Error("config_load_failed", "error", err)
		os.Exit(1)
	}

	log := logging.New(cfg.Logging.Level, cfg.Logging.Format)
	slog.SetDefault(log)

	// Resolve seeds file path: sibling to pipeline.yaml under config/.
	seedsPath := filepath.Join(filepath.Dir(cfgPath), "historical_seeds.yaml")
	// Allow override via env var.
	if v := os.Getenv("SEEDS_PATH"); v != "" {
		seedsPath = v
	}

	seeds, err := loadSeeds(seedsPath)
	if err != nil {
		log.Error("seeds_load_failed", "path", seedsPath, "error", err)
		os.Exit(1)
	}
	log.Info("seeds_loaded",
		"version", seeds.Version,
		"tokens", len(seeds.Tokens),
		"cohorts", len(seeds.CohortDefs),
	)

	ctx := context.Background()

	db := postgres.New(log)
	dbCfg := buildDBConfig(cfg)

	if err := db.Initialize(ctx, dbCfg); err != nil {
		log.Error("db_connect_failed", "error", err)
		os.Exit(1)
	}
	defer db.Close(ctx) //nolint:errcheck

	if err := db.RunMigrations(ctx); err != nil {
		log.Error("migrations_failed", "error", err)
		os.Exit(1)
	}

	profiles, err := computeProfiles(seeds)
	if err != nil {
		log.Error("profile_compute_failed", "error", err)
		os.Exit(1)
	}
	log.Info("profiles_computed", "count", len(profiles))

	var upserted int
	for _, p := range profiles {
		if err := db.UpsertHistoricalProfile(ctx, p); err != nil {
			log.Error("upsert_failed", "cohort_key", p.CohortKey, "error", err)
			os.Exit(1)
		}
		log.Info("profile_upserted",
			"cohort_key", p.CohortKey,
			"token_count", p.TokenCount,
			"prior_probability", p.PriorProbability,
		)
		upserted++
	}

	log.Info("hydrate_complete", "profiles_upserted", upserted)
}

// ── Seeds loader ─────────────────────────────────────────────────────────────

func loadSeeds(path string) (*seedsFile, error) {
	data, err := os.ReadFile(path) //nolint:gosec — path is config-controlled, not user input
	if err != nil {
		return nil, fmt.Errorf("read seeds file %q: %w", path, err)
	}
	var s seedsFile
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse seeds file %q: %w", path, err)
	}
	if len(s.CohortDefs) == 0 {
		return nil, fmt.Errorf("seeds file %q has no cohort_definitions", path)
	}
	if len(s.Tokens) == 0 {
		return nil, fmt.Errorf("seeds file %q has no tokens", path)
	}
	return &s, nil
}

// ── Profile computation ───────────────────────────────────────────────────────

// computeProfiles groups tokens by cohort_key, computes percentile stats for
// each cohort, and builds a HistoricalMarketProfileDTO per cohort.
//
// All arithmetic is deterministic: same input → same output (no randomness,
// no wall-clock reads beyond a single computedAt stamp).
func computeProfiles(seeds *seedsFile) ([]contracts.HistoricalMarketProfileDTO, error) {
	// Index cohort definitions by key (already keyed by cohort name in YAML).
	defsByKey := seeds.CohortDefs

	// Group tokens by cohort_key.
	type tokenGroup struct {
		liquidity    []float64
		volume24h    []float64
		txVelocity   []float64 // (buys + sells) / 24h in tx/hr
		buySellRatio []float64
		socialCount  []int
		count        int
	}
	groups := make(map[string]*tokenGroup)
	for _, t := range seeds.Tokens {
		if t.CohortKey == "" {
			continue
		}
		g, ok := groups[t.CohortKey]
		if !ok {
			g = &tokenGroup{}
			groups[t.CohortKey] = g
		}
		g.count++
		g.liquidity = append(g.liquidity, t.LiquidityUsd)
		g.volume24h = append(g.volume24h, t.Volume24hUsd)

		// tx velocity: total tx per hour over 24h window
		txPerHr := float64(t.TxnsBuys+t.TxnsSells) / 24.0
		g.txVelocity = append(g.txVelocity, txPerHr)

		// buy/sell ratio; guard divide-by-zero
		bsr := 1.0
		if t.TxnsSells > 0 {
			bsr = float64(t.TxnsBuys) / float64(t.TxnsSells)
		}
		g.buySellRatio = append(g.buySellRatio, bsr)
		g.socialCount = append(g.socialCount, t.SocialCount)
	}

	computedAt := time.Now().UTC()
	var profiles []contracts.HistoricalMarketProfileDTO

	for cohortKey, g := range groups {
		def, ok := defsByKey[cohortKey]
		if !ok {
			return nil, fmt.Errorf("token references unknown cohort_key %q; add it to cohort_definitions", cohortKey)
		}

		// Sort slices ascending for percentile computation.
		sort.Float64s(g.liquidity)
		sort.Float64s(g.volume24h)
		sort.Float64s(g.txVelocity)
		sort.Float64s(g.buySellRatio)

		liqP10, liqP50, liqP90 := percentiles(g.liquidity)
		volP10, volP50, volP90 := percentiles(g.volume24h)
		txP10, txP50, txP90 := percentiles(g.txVelocity)
		bsrP10, bsrMed, bsrP90 := percentiles(g.buySellRatio)

		// Social presence rate: fraction of tokens with social_count > 0.
		var socialWith int
		for _, sc := range g.socialCount {
			if sc > 0 {
				socialWith++
			}
		}
		socialPresenceRate := 0.0
		if g.count > 0 {
			socialPresenceRate = float64(socialWith) / float64(g.count)
		}

		// ATH multiple: use the cohort definition values.
		// P10 is estimated at 0.5× P50 (limited upside on low percentile).
		// P90 is estimated at 2× P50 (high-end outlier).
		athP50 := def.ATHMultipleP50
		athP10 := math.Max(1.0, athP50*0.5)
		athP90 := athP50 * 2.0

		// Time-to-rug from cohort definition (P10 = fastest rug, most dangerous).
		timeToRugP10 := def.TimeToRugP10Sec
		// P50 estimate: 2× P10 (rugs slow down at median compared to fastest).
		timeToRugP50 := 0.0
		if timeToRugP10 > 0 {
			timeToRugP50 = timeToRugP10 * 2.0
		}

		p := contracts.HistoricalMarketProfileDTO{
			CohortKey:          cohortKey,
			TokenCount:         g.count,
			LiquidityUsdP10:    liqP10,
			LiquidityUsdP50:    liqP50,
			LiquidityUsdP90:    liqP90,
			Volume24hP10:       volP10,
			Volume24hP50:       volP50,
			Volume24hP90:       volP90,
			TxVelocityP10:      txP10,
			TxVelocityP50:      txP50,
			TxVelocityP90:      txP90,
			BuySellRatioP10:    bsrP10,
			BuySellRatioMedian: bsrMed,
			BuySellRatioP90:    bsrP90,
			ATHMultipleP10:     athP10,
			ATHMultipleP50:     athP50,
			ATHMultipleP90:     athP90,
			TimeToRugP10Sec:    timeToRugP10,
			TimeToRugP50Sec:    timeToRugP50,
			LiquidityMinUsd:    def.LiquidityMinUsd,
			PriorProbability:   def.PriorProbability,
			SocialPresenceRate: socialPresenceRate,
			ProfileVersion:     "seed_v0",
			ComputedAt:         computedAt,
		}
		profiles = append(profiles, p)
	}

	// Sort deterministically by cohort_key so upsert order is stable.
	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].CohortKey < profiles[j].CohortKey
	})
	return profiles, nil
}

// percentiles returns the P10, P50, and P90 values of a pre-sorted ascending
// slice. Returns (0, 0, 0) on an empty slice.
//
// Index formula: floor(n * fraction), clamped to [0, n-1].
// Deterministic: same input → same output.
func percentiles(sorted []float64) (p10, p50, p90 float64) {
	n := len(sorted)
	if n == 0 {
		return 0, 0, 0
	}
	idx := func(frac float64) int {
		i := int(float64(n) * frac)
		if i >= n {
			i = n - 1
		}
		return i
	}
	return sorted[idx(0.10)], sorted[idx(0.50)], sorted[idx(0.90)]
}
