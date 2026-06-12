// run_market_probes.go — optional pipeline stage that enriches
// market_data events with on-chain ground truth produced by the probes
// module.
//
// Wiring decision (residual-risk #4 — option 1, separate event type):
//
// We use a SEPARATE event type ("market_data_enriched") rather than
// re-publishing under the original "market_data_event" type. Reasons:
//
//   - Idempotency: the original event keeps its content-addressable
//     EventID; the enriched event derives a different EventID so the
//     event log is unambiguous about which DTO each consumer processed.
//   - Replay: bit-for-bit determinism is preserved because the enriched
//     DTO only ever appears under its own type, and replays fan in/out
//     through the same routing.
//   - Optionality: when probes.enabled=false the worker is not
//     registered; DQ continues consuming "market_data_event" directly.
//     When probes.enabled=true the orchestrator MUST route DQ on
//     "market_data_enriched" (wiring done in cmd/server.go).
//
// TODO: when additional probes land (tax, lp_lock, owner_privileges,
// holder_dist, wash_stats), register them in the same probe slice. The
// worker iterates probes in registration order; each probe enriches the
// DTO produced by the previous one. Errors from one probe MUST NOT abort
// the pipeline — the partially-enriched DTO is still emitted so the DQ
// layer's *Known-flag-driven degradation runs.

package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/modules/probes"
)

// MarketDataEnrichedEventType is the event type emitted by the probes
// worker. Downstream stages (data_quality) subscribe to this type when
// probes are enabled. When probes are disabled the worker is not
// registered and DQ continues consuming the raw market_data_event.
const MarketDataEnrichedEventType = "market_data_enriched"

// MarketProbesWorker runs each registered MarketProbe in sequence on
// every inbound market_data_event and emits a market_data_enriched
// event. With zero probes registered it acts as pass-through.
type MarketProbesWorker struct {
	adapter          database.Adapter
	probes           []probes.MarketProbe
	logger           *slog.Logger
	nameDedupEnabled bool                // true only when WithNameDedup has been called
	knownTokens      map[string]struct{} // copycat detection list (lowercase + trimmed)
	seenNames        sync.Map            // in-process cache: "chain:normalizedName" → struct{}

	// Probe rate limiter — caps Helius RPC HTTP calls per rolling hour.
	// Tokens over the cap are emitted with Known=false flags; DQ's
	// fail-closed rules (reject_unknown_social_links etc.) handle them.
	maxProbesPerHour int        // 0 = unlimited
	probedThisHour   int64      // atomic counter; reset each hour window
	hourMu           sync.Mutex // guards hourWindowStart reset
	hourWindowStart  time.Time  // start of the current one-hour window

	// Batch account fetch (Task 8): one getMultipleAccounts for authorities +
	// pumpfun_lp on new-token events.
	batchAccounts             bool
	rescanSkipPumpfunLpPhase2 bool
	batchRPC                  probes.SolanaProbeRPCClient
	batchSolUsd               probes.SolUsdSource
	batchAuthoritiesEnabled   bool
	batchPumpfunLpEnabled     bool
	batchAuthoritiesTimeoutMs int
	batchPumpfunLpTimeoutMs   int
	batchCommitment           string
}

// NewMarketProbesWorker constructs a worker. Pass nil/empty `probeList`
// for pass-through mode. The adapter is used to persist the enriched
// MarketDataDTO so that downstream workers (e.g. FeaturesWorker) can
// retrieve it by event ID via GetMarketData.
func NewMarketProbesWorker(adapter database.Adapter, probeList []probes.MarketProbe, logger *slog.Logger) *MarketProbesWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &MarketProbesWorker{
		adapter: adapter,
		probes:  probeList,
		logger:  logger,
	}
}

// WithNameDedup configures the pre-probe name-deduplication and copycat guard.
// knownTokens is a list of famous/established token names (any case); entries
// are normalized (lowercased, trimmed) at load time. A new token whose
// normalized name matches any entry is flagged IsCopycat=true and all RPC
// probes are skipped, saving Helius credits.
//
// Call this after NewMarketProbesWorker and before registering the worker.
// When not called, the entire name-dedup block (session cache, copycat list,
// and DB check) is skipped — no Helius credits consumed for name checks.
func (w *MarketProbesWorker) WithNameDedup(knownTokens []string) *MarketProbesWorker {
	w.nameDedupEnabled = true
	w.knownTokens = make(map[string]struct{}, len(knownTokens))
	for _, n := range knownTokens {
		normalized := strings.ToLower(strings.TrimSpace(n))
		if normalized != "" {
			w.knownTokens[normalized] = struct{}{}
		}
	}
	return w
}

// WithProbeRateLimit sets a hard ceiling on the number of tokens that trigger
// Helius RPC probe calls per rolling one-hour window. When maxPerHour is
// reached within an hour, additional tokens bypass all probes and are emitted
// with Known=false flags. The DQ layer's fail-closed rules then reject them
// without consuming any further Helius credits.
//
// Credit math at maxPerHour=350 (Helius free tier, 1M credits/month):
//
//	350 × 3 credits × 720 hr = 756k credits/month (probes only)
//
// Set maxPerHour=0 to disable rate limiting (unlimited probes).
// Call this after NewMarketProbesWorker and before registering the worker.
func (w *MarketProbesWorker) WithProbeRateLimit(maxPerHour int) *MarketProbesWorker {
	w.maxProbesPerHour = maxPerHour
	w.hourWindowStart = time.Now()
	return w
}

// BatchAccountsConfig wires optional getMultipleAccounts batching for the
// solana_authorities + solana_pumpfun_lp probes on new-token ingest events.
type BatchAccountsConfig struct {
	RescanSkipPumpfunLpPhase2 bool
	AuthoritiesEnabled        bool
	PumpfunLpEnabled          bool
	AuthoritiesTimeoutMs      int
	PumpfunLpTimeoutMs        int
	Commitment                string
}

// WithBatchAccounts enables a single getMultipleAccounts call for mint +
// bonding-curve accounts before the per-probe loop. rpc must implement
// GetMultipleAccounts (the *rpc.SolanaClient adapter in cmd/server.go does).
func (w *MarketProbesWorker) WithBatchAccounts(
	enabled bool,
	rpc probes.SolanaProbeRPCClient,
	solUsd probes.SolUsdSource,
	cfg BatchAccountsConfig,
) *MarketProbesWorker {
	w.batchAccounts = enabled
	w.batchRPC = rpc
	w.batchSolUsd = solUsd
	w.rescanSkipPumpfunLpPhase2 = cfg.RescanSkipPumpfunLpPhase2
	w.batchAuthoritiesEnabled = cfg.AuthoritiesEnabled
	w.batchPumpfunLpEnabled = cfg.PumpfunLpEnabled
	w.batchAuthoritiesTimeoutMs = cfg.AuthoritiesTimeoutMs
	w.batchPumpfunLpTimeoutMs = cfg.PumpfunLpTimeoutMs
	w.batchCommitment = cfg.Commitment
	return w
}

// Process decodes the inbound market_data_event, runs every registered
// probe in order, and emits a market_data_enriched event carrying the
// (possibly enriched) DTO.
//
// Probe failures degrade gracefully: a failing probe logs and is
// skipped, but the worker still emits the partially-enriched DTO so
// the DQ layer's degradation contract (the *Known flags) drives the
// behaviour.
func (w *MarketProbesWorker) Process(ctx context.Context, evt *database.Event) (*database.Event, error) {
	var md contracts.MarketDataDTO
	if err := json.Unmarshal(evt.Payload, &md); err != nil {
		return nil, fmt.Errorf("market_probes_worker: unmarshal: %w", err)
	}

	enriched := md

	// ── Pre-probe name-dedup guard ────────────────────────────────────────
	// Skip ALL probes (saving Helius credits) when the token's name is a
	// duplicate of a previously ingested token, or matches a well-known/
	// famous token in the configured copycat list.
	//
	// The guard runs only when Name is non-empty (pump.fun Solana tokens).
	// EVM tokens have empty Name and are passed through to probes unchanged.
	//
	// Order of checks (fastest-first):
	//   1. In-process session cache — zero DB cost for repeat names.
	//   2. Copycat list — O(1) map lookup against famous token names.
	//   3. DB query — covers cross-restart duplicate detection.
	if w.nameDedupEnabled && md.Name != "" {
		normalizedName := strings.ToLower(strings.TrimSpace(md.Name))
		if normalizedName != "" {
			cacheKey := md.Chain + ":" + normalizedName

			// 1. In-process session cache: if we've seen this name on this
			// chain before, flag as duplicate immediately (no DB hit needed).
			if _, inCache := w.seenNames.Load(cacheKey); inCache {
				enriched.IsNameDuplicate = true
				w.logger.Info("pre_probe_name_dedup_cache_hit",
					"name", md.Name,
					"chain", md.Chain,
					"token", md.TokenAddress,
					"event_id", evt.EventID,
				)
			}

			// 2. Copycat list: O(1) lookup against famous token names.
			if !enriched.IsNameDuplicate && !enriched.IsCopycat && len(w.knownTokens) > 0 {
				if _, isCopycat := w.knownTokens[normalizedName]; isCopycat {
					enriched.IsCopycat = true
					w.logger.Info("pre_probe_copycat_detected",
						"name", md.Name,
						"normalized", normalizedName,
						"token", md.TokenAddress,
						"chain", md.Chain,
						"event_id", evt.EventID,
					)
				}
			}

			// 3. DB check: covers cross-restart duplicates not yet in session cache.
			if !enriched.IsNameDuplicate && !enriched.IsCopycat {
				seen, err := w.adapter.CheckTokenNameSeen(ctx, normalizedName, md.Chain, md.TokenAddress)
				if err != nil {
					// Fail-open: log and proceed with probes; do not reject on DB error.
					w.logger.Warn("pre_probe_name_check_error",
						"name", md.Name,
						"chain", md.Chain,
						"event_id", evt.EventID,
						"error", err,
					)
				} else if seen {
					enriched.IsNameDuplicate = true
					w.logger.Info("pre_probe_name_dedup_db_hit",
						"name", md.Name,
						"normalized", normalizedName,
						"token", md.TokenAddress,
						"chain", md.Chain,
						"event_id", evt.EventID,
					)
				}
			}

			// Add to session cache so the next token with this name is caught
			// by the fast path, regardless of whether it was a duplicate.
			w.seenNames.Store(cacheKey, struct{}{})

			// If either flag is set, skip all probes and emit immediately.
			if enriched.IsNameDuplicate || enriched.IsCopycat {
				enriched.EventID = contracts.ContentIDFromString(fmt.Sprintf("md_enriched:%s", md.EventID))
				if err := w.adapter.InsertMarketData(ctx, enriched); err != nil {
					w.logger.Warn("market_probes_persist_failed",
						"event_id", enriched.EventID,
						"trace_id", evt.TraceID,
						"error", err,
					)
				}
				w.logger.Info("pre_probe_guard_skipped_all_probes",
					"event_id", evt.EventID,
					"token", md.TokenAddress,
					"name", md.Name,
					"is_name_duplicate", enriched.IsNameDuplicate,
					"is_copycat", enriched.IsCopycat,
					"probes_saved", len(w.probes),
				)
				return makeOutputEvent(
					enriched.EventID, enriched, MarketDataEnrichedEventType,
					evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
				)
			}
		}
	}

	var results []probes.ProbeResult
	// ── Probe rate limiter ────────────────────────────────────────────────
	// When maxProbesPerHour > 0, enforce a hard ceiling on Helius RPC HTTP
	// calls per rolling one-hour window. Tokens over the cap skip all probes
	// and proceed with Known=false flags — DQ's fail-closed structural rejects
	// handle them without spending any additional Helius credits.
	if w.maxProbesPerHour > 0 {
		// Reset counter at the start of each new hour window.
		w.hourMu.Lock()
		if time.Since(w.hourWindowStart) >= time.Hour {
			w.hourWindowStart = time.Now()
			atomic.StoreInt64(&w.probedThisHour, 0)
		}
		w.hourMu.Unlock()

		count := atomic.AddInt64(&w.probedThisHour, 1)
		if count > int64(w.maxProbesPerHour) {
			// Over the hourly cap — emit immediately with Known=false flags.
			enriched.EventID = contracts.ContentIDFromString(fmt.Sprintf("md_enriched:%s", md.EventID))
			if err := w.adapter.InsertMarketData(ctx, enriched); err != nil {
				w.logger.Warn("market_probes_persist_failed",
					"event_id", enriched.EventID,
					"trace_id", evt.TraceID,
					"error", err,
				)
			}
			w.logger.Info("pre_probe_rate_limit_skipped",
				"event_id", evt.EventID,
				"token", md.TokenAddress,
				"probed_this_hour", count,
				"max_probes_per_hour", w.maxProbesPerHour,
				"probes_saved", len(w.probes),
			)
			return makeOutputEvent(
				enriched.EventID, enriched, MarketDataEnrichedEventType,
				evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
			)
		}
	}

	// ── Rescan-aware probe skip ───────────────────────────────────────────
	// Rescan events (transport: "rescan_*") re-emit tokens that have already
	// been fully probed at ingest time. Several probe results are immutable or
	// change so slowly that re-probing wastes Helius credits:
	//
	//   solana_authorities: Mint/freeze authority is immutable once set.
	//     After the first probe the Known flag is true and the result never
	//     changes. Skipping saves 1 getAccountInfo per rescan event.
	//
	//   solana_holder_dist: Phase 1 bands (15m–8h) skip this probe — holder
	//     distribution changes slowly in the first 8h and getTokenLargestAccounts
	//     is the most expensive Helius call per request. Phase 2 bands (12h–48h)
	//     re-fetch to catch post-launch whale accumulation that materially changes
	//     the DQ risk profile over longer timeframes.
	//
	//   solana_pumpfun_lp: Phase 1 bands (15m–8h) re-fetch bonding-curve
	//     reserves. Phase 2 bands (12h–48h) skip when
	//     rescan_skip_pumpfun_lp_phase2 is enabled — reserves change slowly
	//     and ingest-time LP data is sufficient for DQ at those ages.
	//
	// All other probes (metadata, creator_reputation) are HTTP-only (no Helius
	// credits) so they are not skipped for rescan events.
	isRescan := strings.HasPrefix(enriched.Transport, "rescan_")
	// Phase 2 late-rescan bands re-fetch holder distribution for fresh DQ data.
	isLateRescan := isRescan && (enriched.Transport == "rescan_12h" ||
		enriched.Transport == "rescan_24h" ||
		enriched.Transport == "rescan_36h" ||
		enriched.Transport == "rescan_48h")
	rescanSkipProbes := map[string]bool{}
	if isRescan {
		// Only skip authority probe when the result is already known (immutable).
		if enriched.SolanaAuthoritiesKnown {
			rescanSkipProbes["solana_authorities"] = true
		}
		// Skip holder distribution for Phase 1 bands (15m–8h) only.
		// Phase 2 bands (12h–48h) re-fetch for fresh whale accumulation data.
		if !isLateRescan {
			rescanSkipProbes["solana_holder_dist"] = true
		}
		if w.rescanSkipPumpfunLpPhase2 && isLateRescan {
			rescanSkipProbes["solana_pumpfun_lp"] = true
		}
	}

	batchSkipProbes := map[string]bool{}
	if w.batchAccounts && !isRescan && w.batchRPC != nil {
		req := probes.BatchAccountRequestFor(
			enriched,
			w.batchAuthoritiesEnabled,
			w.batchPumpfunLpEnabled,
			w.batchCommitment,
		)
		if req.NeedsFetch() {
			timeout := probes.BatchFetchTimeout(w.batchAuthoritiesTimeoutMs, w.batchPumpfunLpTimeoutMs)
			bctx, cancel := context.WithTimeout(ctx, timeout)
			res, err := probes.FetchBatchAccounts(bctx, w.batchRPC, req)
			cancel()
			if err != nil {
				w.logger.Warn("market_probe_batch_fetch_failed",
					"event_id", evt.EventID,
					"token", enriched.TokenAddress,
					"error", err,
				)
			} else {
				enriched = probes.ApplyBatchAccounts(
					ctx, enriched, res,
					w.batchSolUsd,
					w.batchAuthoritiesEnabled,
					w.batchPumpfunLpEnabled,
				)
				if w.batchAuthoritiesEnabled && req.Mint != "" {
					if res.Mint != nil && enriched.SolanaAuthoritiesKnown {
						batchSkipProbes["solana_authorities"] = true
					}
				}
				if w.batchPumpfunLpEnabled && req.Pool != "" {
					if res.Pool != nil && enriched.ReserveBaseRaw != md.ReserveBaseRaw {
						batchSkipProbes["solana_pumpfun_lp"] = true
					}
				}
				w.logger.Debug("market_probe_batch_fetch",
					"event_id", evt.EventID,
					"token", enriched.TokenAddress,
					"authorities_known", enriched.SolanaAuthoritiesKnown,
					"lp_stats_known", enriched.LpStatsKnown,
					"skip_authorities", batchSkipProbes["solana_authorities"],
					"skip_pumpfun_lp", batchSkipProbes["solana_pumpfun_lp"],
				)
			}
		}
	}

	for _, p := range w.probes {
		if rescanSkipProbes[p.Name()] || batchSkipProbes[p.Name()] {
			w.logger.Debug("market_probe_skip",
				"probe", p.Name(),
				"transport", enriched.Transport,
				"token", enriched.TokenAddress,
				"event_id", evt.EventID,
				"rescan_skip", rescanSkipProbes[p.Name()],
				"batch_skip", batchSkipProbes[p.Name()],
			)
			continue
		}
		start := time.Now()
		out, err := p.Probe(ctx, enriched)
		dur := time.Since(start).Milliseconds()
		res := probes.ProbeResult{
			ProbeName:  p.Name(),
			Success:    err == nil,
			DurationMs: dur,
		}
		if err != nil {
			res.Error = err.Error()
			w.logger.Warn("market_probe_failed",
				"probe", p.Name(),
				"event_id", evt.EventID,
				"trace_id", evt.TraceID,
				"version_id", evt.VersionID,
				"error", err,
			)
		} else {
			enriched = out
		}
		results = append(results, res)
	}

	// Log aggregate probe results so timing data is observable.
	probeAttrs := make([]any, 0, 2+len(results)*2)
	probeAttrs = append(probeAttrs,
		"event_id", evt.EventID,
		"trace_id", evt.TraceID,
		"version_id", evt.VersionID,
		"probe_count", len(w.probes),
		"honeypot_sim_known", enriched.HoneypotSimKnown,
		"social_links_known", enriched.SocialLinksKnown,
		"has_social_links", enriched.HasSocialLinks,
		"creator_count_known", enriched.CreatorPrevTokenCountKnown,
		"total_supply_known", enriched.TotalSupplyKnown,
	)
	for _, r := range results {
		probeAttrs = append(probeAttrs,
			"probe."+r.ProbeName+".ok", r.Success,
			"probe."+r.ProbeName+".ms", r.DurationMs,
		)
	}
	w.logger.Info("market_probes_completed", probeAttrs...)

	// Re-derive the EventID for the enriched DTO so it is distinct from
	// the upstream raw event (idempotency / event-log clarity). Use a
	// content-addressable derivation rooted in the upstream EventID so
	// replays produce identical IDs.
	enriched.EventID = contracts.ContentIDFromString(fmt.Sprintf("md_enriched:%s", md.EventID))

	// Persist the enriched MarketDataDTO so that FeaturesWorker can
	// retrieve it by event ID via GetMarketData. Without this, the
	// features module degrades to cold-start (LiquidityUsdRaw=0, all
	// confidences=0.1) which cascades to a probability_used fallback and
	// 100% validation rejection. InsertMarketData is idempotent
	// (ON CONFLICT DO NOTHING) so retries are safe.
	if err := w.adapter.InsertMarketData(ctx, enriched); err != nil {
		w.logger.Warn("market_probes_persist_failed",
			"event_id", enriched.EventID,
			"trace_id", evt.TraceID,
			"error", err,
		)
		// Non-fatal: proceed with event emission so the pipeline is not
		// blocked. FeaturesWorker will degrade to cold-start for this
		// token but the DQ decision is still emitted.
	}

	return makeOutputEvent(
		enriched.EventID, enriched, MarketDataEnrichedEventType,
		evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
	)
}
