package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/sniper-bot/internal/modules/data_quality"
	dqproviders "crypto-sniping-bot/sniper-bot/internal/modules/data_quality/providers"
	"crypto-sniping-bot/sniper-bot/internal/orchestrator"
)

// DataQualityWorker implements orchestrator.StageHandler for Layer 1.
// Consumes: market_data_event → emits: data_quality_event (PASS/RISKY_PASS only)
type DataQualityWorker struct {
	adapter database.Adapter
	mod     *data_quality.Module
	logger  *slog.Logger
}

// NewDataQualityWorker returns a new DataQualityWorker.
func NewDataQualityWorker(adapter database.Adapter, cfg *config.Config, logger *slog.Logger) *DataQualityWorker {
	if logger == nil {
		logger = slog.Default()
	}
	mod := data_quality.New(data_quality.DefaultConfig(cfg), logger).
		WithRuntimeConfig(&cfg.DataQualityRuntime).
		WithCreatorProfileReader(newAdapterCreatorProfileReader(adapter))

	// ── External providers (P1) ─────────────────────────────────────────
	// Build the optional provider aggregator when providers are enabled.
	// All providers boot shadow_mode: true — they observe but never affect
	// the pipeline decision until shadow validation is complete.
	if cfg.DataQualityRuntime.Providers.Enabled {
		pcfg := cfg.DataQualityRuntime.Providers
		entries := make([]dqproviders.ProviderEntry, 0, 1)

		if pcfg.RugCheck.Enabled {
			shadowMode := pcfg.ShadowMode || pcfg.RugCheck.ShadowMode
			entries = append(entries, dqproviders.ProviderEntry{
				Provider:   dqproviders.NewRugCheckProvider(logger),
				Weight:     pcfg.RugCheck.Weight,
				Enabled:    true,
				ShadowMode: shadowMode,
			})
		}

		if pcfg.SocialGate.Enabled {
			shadowMode := pcfg.ShadowMode || pcfg.SocialGate.ShadowMode
			entries = append(entries, dqproviders.ProviderEntry{
				Provider:   dqproviders.NewSocialGateProvider(logger),
				Weight:     pcfg.SocialGate.Weight,
				Enabled:    true,
				ShadowMode: shadowMode,
			})
		}

		if pcfg.BirdEye.Enabled {
			shadowMode := pcfg.ShadowMode || pcfg.BirdEye.ShadowMode
			entries = append(entries, dqproviders.ProviderEntry{
				Provider:   dqproviders.NewBirdEyeProvider(logger),
				Weight:     pcfg.BirdEye.Weight,
				Enabled:    true,
				ShadowMode: shadowMode,
			})
		}

		if pcfg.CopyTrade.Enabled {
			shadowMode := pcfg.ShadowMode || pcfg.CopyTrade.ShadowMode
			entries = append(entries, dqproviders.ProviderEntry{
				Provider:   dqproviders.NewCopyTradeProvider(logger),
				Weight:     pcfg.CopyTrade.Weight,
				Enabled:    true,
				ShadowMode: shadowMode,
			})
		}

		if len(entries) > 0 {
			agg := dqproviders.NewAggregator(entries, pcfg.BudgetMs, logger)
			mod = mod.WithProviders(agg)
			logger.Info("dq_providers_wired",
				"count", len(entries),
				"shadow_mode", pcfg.ShadowMode,
				"budget_ms", pcfg.BudgetMs,
			)
		}
	}

	return &DataQualityWorker{
		adapter: adapter,
		mod:     mod,
		logger:  logger,
	}
}

// Process decodes a market_data_event, runs data quality checks, persists the result,
// and emits a data_quality_event if the token passes.
func (w *DataQualityWorker) Process(ctx context.Context, evt *database.Event) (*database.Event, error) {
	var dto contracts.MarketDataDTO
	if err := json.Unmarshal(evt.Payload, &dto); err != nil {
		return nil, fmt.Errorf("dq_worker: unmarshal: %w", err)
	}

	// Validate token address before creating lifecycle.
	// An empty address cannot be uniquely keyed in the lifecycle table and would
	// cause unrelated invalid events to share the same lifecycle row.
	if dto.TokenAddress == "" {
		return nil, fmt.Errorf("dq_worker: empty token address in market_data_event %s", evt.EventID)
	}

	// Ensure lifecycle exists.
	lifecycleID, err := w.adapter.StartLifecycle(ctx, dto)
	if err != nil {
		return nil, fmt.Errorf("dq_worker: start_lifecycle: %w", err)
	}

	// Run data quality module (pure function).
	// Read active operational mode from SystemState — STRICT/BALANCED/
	// VERY_EXPLORATION select the per-mode threshold profile inside the module.
	// On error or absent state, the module collapses unknown values onto
	// STRICT (conservative).
	sysMode := "BALANCED"
	if state, stateErr := w.adapter.GetSystemState(ctx); stateErr == nil && state != nil && state.Mode != "" {
		sysMode = state.Mode
	}

	// Enrich DTO with creator token count before the pure module call.
	// The DQ module is a pure function — DB lookups happen here in the worker.
	//
	// Priority: the MarketProbesWorker runs BEFORE the DQ worker and populates
	// CreatorPrevTokenCountKnown=true via SolanaCreatorReputationProbe (which
	// queries the pump.fun API for ground-truth launch history). Only fall back
	// to the local DB if the probe did not run or failed.
	//
	// Cold-start safety: if neither the external probe NOR the local DB has
	// seen this creator (both return 0), leave CreatorPrevTokenCountKnown=false.
	// This triggers DEV_UNKNOWN_HISTORY (Score=1.0, fail-closed) in the DQ
	// module instead of silently treating an unknown creator as "new dev".
	if !dto.CreatorPrevTokenCountKnown && dto.CreatorAddress != "" {
		count, countErr := w.adapter.CountTokensByCreator(ctx, dto.CreatorAddress, dto.TokenAddress)
		if countErr != nil {
			w.logger.Warn("dq_worker_creator_count_failed",
				"creator", dto.CreatorAddress, "error", countErr)
			// Leave CreatorPrevTokenCountKnown=false; DQ degrades per profile.
		} else if count > 0 {
			// Only trust the local DB count when we have actually seen this
			// creator before. count=0 on a cold DB is indistinguishable from
			// a new creator — leave Known=false so DQ fails closed.
			dto.CreatorPrevTokenCount = count
			dto.CreatorPrevTokenCountKnown = true
		}
		// count==0: leave CreatorPrevTokenCountKnown=false (fail-closed).
	}

	dqDTO, err := w.mod.ProcessForMode(ctx, dto, sysMode)
	if err != nil {
		return nil, fmt.Errorf("dq_worker: module: %w", err)
	}

	w.logger.Info("dq_decision",
		"token", dqDTO.TokenAddress,
		"decision", dqDTO.Decision,
		"risk_score", dqDTO.RiskScore,
		"profile", dqDTO.Profile,
		"reject_reasons", dqDTO.RejectReasons,
		"flags", dqDTO.Flags,
		"trace_id", dqDTO.TraceID,
		"version_id", dqDTO.VersionID,
		"social_links_known", dto.SocialLinksKnown,
		"creator_count_known", dto.CreatorPrevTokenCountKnown,
		"total_supply_known", dto.TotalSupplyKnown,
		"holder_dist_known", dto.HolderDistKnown,
		"creator_address", dto.CreatorAddress,
	)

	// Persist the result regardless of decision.
	if err := w.adapter.InsertDataQuality(ctx, dqDTO); err != nil {
		w.logger.Warn("dq_worker_persist_failed", "event_id", dqDTO.EventID, "error", err)
	}

	// Determine the actual current lifecycle state.
	// StartLifecycle is idempotent (ON CONFLICT DO NOTHING): a rescan
	// re-evaluation returns the existing lifecycle ID whose state may
	// already be REJECTED. We must use the real from-state so that the
	// CAS guard in doMandatoryTransition receives the correct origin.
	lc, err := w.adapter.GetLifecycle(ctx, lifecycleID)
	if err != nil {
		return nil, fmt.Errorf("dq_worker: get_lifecycle: %w", err)
	}
	fromState := lc.CurrentState

	// Guard: only DETECTED, REJECTED, and DQ_SKIPPED are DQ-eligible entry states.
	// Tokens that already advanced past DQ (FEATURE_READY and beyond)
	// are drained idempotently — no re-scoring of already-accepted tokens.
	// DQ_SKIPPED is included so rescan workers can re-evaluate a previously
	// skipped token (e.g., after holder count or social links are populated).
	if fromState != "DETECTED" && fromState != "REJECTED" && fromState != "DQ_SKIPPED" {
		return nil, fmt.Errorf("dq_worker: lifecycle %s already past DQ (state=%s): %w",
			lifecycleID, fromState, database.ErrLifecycleAlreadyAdvanced)
	}

	// SKIP: silent drop — do NOT emit data_quality_event; do NOT contribute to
	// reject-rate statistics. Transition token_lifecycle to DQ_SKIPPED so the
	// audit trail is preserved for replay and debugging.
	if dqDTO.Decision == contracts.DecisionSkip {
		w.logger.Info("dq_skip",
			"token", dqDTO.TokenAddress,
			"chain", dqDTO.Chain,
			"flags", dqDTO.Flags,
			"profile", dqDTO.Profile,
			"trace_id", dqDTO.TraceID,
		)
		// Idempotent: token already DQ_SKIPPED — no transition needed.
		// This occurs when a rescan re-emits an event for a token that was
		// already silently dropped in a prior cycle (same skip criteria still
		// apply). Drain cleanly without retrying a forbidden self-transition.
		if fromState == "DQ_SKIPPED" {
			return nil, nil
		}
		if err := doMandatoryTransition(ctx, w.adapter, lifecycleID, fromState, "DQ_SKIPPED", "serial_launcher_skip", "dq_worker"); err != nil {
			return nil, fmt.Errorf("dq_worker: skip_transition: %w", err)
		}
		recordDQSkipShadow(ctx, w.adapter, dqDTO, w.logger)
		return nil, nil // No downstream event — SKIP is silent.
	}

	// Lifecycle transition: fromState → DQ_PASSED or REJECTED.
	nextState := "DQ_PASSED"
	if dqDTO.Decision == "REJECT" {
		nextState = "REJECTED"
	}

	// Rescan re-rejection: token still fails DQ — no state change needed,
	// no downstream event. Drain idempotently without an error.
	if fromState == "REJECTED" && nextState == "REJECTED" {
		orchestrator.RecordDecision(ctx, orchestrator.StageStatusRejected, dqRejectReason(dqDTO))
		return nil, nil
	}

	if err := doMandatoryTransition(ctx, w.adapter, lifecycleID, fromState, nextState, dqDTO.Decision, "dq_worker"); err != nil {
		return nil, fmt.Errorf("dq_worker: transition: %w", err)
	}

	// Do not emit downstream event for first-time rejections.
	if dqDTO.Decision == "REJECT" {
		orchestrator.RecordDecision(ctx, orchestrator.StageStatusRejected, dqRejectReason(dqDTO))
		return nil, nil
	}

	return makeOutputEvent(
		dqDTO.EventID, dqDTO, "data_quality_event",
		evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
	)
}

func recordDQSkipShadow(
	ctx context.Context,
	adapter database.Adapter,
	dqDTO contracts.DataQualityDTO,
	logger *slog.Logger,
) {
	shadowID := contracts.ContentIDFromString(dqDTO.TokenLifecycleID + "dq_skip" + dqDTO.EventID)
	st := database.ShadowTrade{
		ShadowID:            shadowID,
		TokenAddress:        dqDTO.TokenAddress,
		Stage:               "dq_skip",
		RejectedAt:          time.Now().UTC().Format(time.RFC3339Nano),
		ObservationComplete: false,
		Classification:      "pending_skip_fn",
		VersionID:           dqDTO.VersionID,
	}
	if err := adapter.InsertShadowTrade(ctx, st); err != nil {
		if logger != nil {
			logger.Warn("dq_skip_shadow_record_failed",
				"token", dqDTO.TokenAddress,
				"shadow_id", shadowID,
				"error", err,
			)
		}
	}
}
