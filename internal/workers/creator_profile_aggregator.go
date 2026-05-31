package workers

// RunCreatorProfileAggregator consumes market_data_event and learning_record_event
// from the event bus and maintains the creator_profiles table for the
// serial-launcher check (Layer 1, Task 9).
//
// Design invariants:
//   - Pure event-bus consumer: no RPC calls, no module imports, no SQL.
//   - Skips events whose CreatorAddress is empty or matches a known factory
//     program (pump.fun bonding-curve or AMM) — these are protocol addresses,
//     not human creators.
//   - Emits a creator_profile_updated system event after each successful upsert
//     so operators can observe aggregation health without querying the DB.
//   - One return per call (matches Run* pattern used by all workers):
//     returns nil when no event was available (idle), returns the first
//     fatal error otherwise.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
)

// Factory program addresses for Solana pump.fun.
// These are protocol-owned wallets that deploy tokens on behalf of users;
// they must NOT be counted as a "creator" in the creator_profiles table.
//
// Addresses are imported from the plan §7 verbatim to avoid drift.
const (
	// factoryPumpFunBondingCurve is the pump.fun bonding-curve program.
	// Disabled by Task 3 for ingestion; still appears as CreatorAddress
	// on old events in the event bus that pre-date the Task 3 guard.
	factoryPumpFunBondingCurve = "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P"

	// factoryPumpFunAMM is the pump.fun AMM program (post-graduation pool).
	factoryPumpFunAMM = "pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA"
)

// consumerNameCreatorProfile is the stable consumer identity used for SKIP LOCKED.
const consumerNameCreatorProfile = "creator_profile_aggregator"

// RunCreatorProfileAggregator claims and processes one event per call.
// Call site (cmd/server.go) wraps it in a tight loop with a 100ms idle backoff.
// cfg may be nil — when nil, golden-gem classification is disabled (threshold=0).
func RunCreatorProfileAggregator(
	ctx context.Context,
	adapter database.Adapter,
	cfg *config.Config,
	logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}

	goldenGemThreshold := 0.0
	if cfg != nil {
		goldenGemThreshold = cfg.CreatorProfile.GoldenGemPnlThresholdPct
	}

	evt, err := adapter.ClaimNextEvent(ctx, consumerNameCreatorProfile,
		[]string{"market_data_event", "learning_record_event"})
	if err != nil {
		return err
	}
	if evt == nil {
		return nil
	}

	switch evt.EventType {
	case "market_data_event":
		return handleMarketDataEvent(ctx, adapter, evt, goldenGemThreshold, logger)
	case "learning_record_event":
		return handleLearningRecordEvent(ctx, adapter, evt, goldenGemThreshold, logger)
	default:
		// Unexpected event type — mark processed and continue.
		logger.Warn("creator_profile_aggregator_unexpected_type",
			"event_id", evt.EventID,
			"event_type", evt.EventType,
		)
		return adapter.MarkEventProcessed(ctx, evt.EventID)
	}
}

// handleMarketDataEvent increments total_tokens for the event's creator.
func handleMarketDataEvent(
	ctx context.Context,
	adapter database.Adapter,
	evt *database.Event,
	_ float64, // goldenGemThreshold unused for market_data_event
	logger *slog.Logger,
) error {
	var md contracts.MarketDataDTO
	if err := json.Unmarshal(evt.Payload, &md); err != nil {
		logger.Warn("creator_profile_aggregator_decode_market_data",
			"event_id", evt.EventID,
			"error", err,
		)
		// Malformed payload — mark processed so the partition advances.
		return adapter.MarkEventProcessed(ctx, evt.EventID)
	}

	if md.CreatorAddress == "" {
		return adapter.MarkEventProcessed(ctx, evt.EventID)
	}
	if isFactoryProgram(md.CreatorAddress) {
		return adapter.MarkEventProcessed(ctx, evt.EventID)
	}

	if err := adapter.UpsertCreatorProfileOnLaunch(ctx, md.Chain, md.CreatorAddress); err != nil {
		logger.Error("creator_profile_aggregator_upsert_launch",
			"event_id", evt.EventID,
			"chain", md.Chain,
			"creator", md.CreatorAddress,
			"error", err,
		)
		_ = adapter.ReleaseEventClaim(ctx, evt.EventID)
		return err
	}

	logger.Info("creator_profile_launch_recorded",
		"chain", md.Chain,
		"creator", md.CreatorAddress,
		"token", md.TokenAddress,
	)

	if emitErr := emitProfileUpdated(ctx, adapter, evt, md.Chain, md.CreatorAddress); emitErr != nil {
		logger.Warn("creator_profile_aggregator_emit_failed",
			"event_id", evt.EventID,
			"error", emitErr,
		)
		// Best-effort: emit failure does not block the main processing path.
	}

	return adapter.MarkEventProcessed(ctx, evt.EventID)
}

// handleLearningRecordEvent increments the matching outcome bucket for
// the token's creator based on the LearningRecordDTO's Outcome field.
func handleLearningRecordEvent(
	ctx context.Context,
	adapter database.Adapter,
	evt *database.Event,
	goldenGemThreshold float64,
	logger *slog.Logger,
) error {
	var lr contracts.LearningRecordDTO
	if err := json.Unmarshal(evt.Payload, &lr); err != nil {
		logger.Warn("creator_profile_aggregator_decode_learning_record",
			"event_id", evt.EventID,
			"error", err,
		)
		return adapter.MarkEventProcessed(ctx, evt.EventID)
	}

	// Resolve the creator address from the EdgeSnapshot embedded in the
	// LearningRecordDTO. EdgeDTO.CreatorAddress is populated by the edge module
	// from the upstream MarketDataDTO.CreatorAddress (Phase 11 additive field).
	// FeaturesSnapshot.Chain carries the chain key threaded from MarketDataDTO.
	// If either is empty the record pre-dates the propagation; skip silently.
	creator := lr.EdgeSnapshot.CreatorAddress
	chain := lr.FeaturesSnapshot.Chain
	if creator == "" || chain == "" {
		// Creator not propagated into the learning record — skip silently.
		return adapter.MarkEventProcessed(ctx, evt.EventID)
	}
	if isFactoryProgram(creator) {
		return adapter.MarkEventProcessed(ctx, evt.EventID)
	}

	outcome := resolveOutcome(lr.Outcome, lr.PnlPct, goldenGemThreshold)
	if outcome == "" {
		// Outcome not mappable to a bucket — mark processed without update.
		return adapter.MarkEventProcessed(ctx, evt.EventID)
	}

	if err := adapter.IncrementCreatorOutcome(ctx, chain, creator, outcome); err != nil {
		logger.Error("creator_profile_aggregator_increment_outcome",
			"event_id", evt.EventID,
			"chain", chain,
			"creator", creator,
			"outcome", outcome,
			"error", err,
		)
		_ = adapter.ReleaseEventClaim(ctx, evt.EventID)
		return err
	}

	logger.Info("creator_profile_outcome_recorded",
		"chain", chain,
		"creator", creator,
		"outcome", outcome,
		"pnl_pct", lr.PnlPct,
	)

	if emitErr := emitProfileUpdated(ctx, adapter, evt, chain, creator); emitErr != nil {
		logger.Warn("creator_profile_aggregator_emit_failed",
			"event_id", evt.EventID,
			"error", emitErr,
		)
	}

	return adapter.MarkEventProcessed(ctx, evt.EventID)
}

// resolveOutcome maps a LearningRecord Outcome string to the creator_profiles
// bucket name. Returns "" when the outcome has no corresponding bucket.
func resolveOutcome(outcome string, pnlPct, goldenGemThreshold float64) string {
	switch outcome {
	case "RUG":
		return "rug"
	case "MIGRATED":
		return "migrated"
	case "TP":
		if goldenGemThreshold > 0 && pnlPct >= goldenGemThreshold {
			return "golden"
		}
		return "win"
	case "SL", "TIME", "TIMEOUT", "FORCED_CLOSE":
		return "loss"
	case "MISSED_PUMP", "CORRECT_REJECT":
		// Shadow-trade outcomes: do not attribute to a creator bucket.
		return ""
	default:
		return ""
	}
}

// isFactoryProgram returns true when addr is one of the known Solana factory
// program addresses that create tokens on behalf of users.
func isFactoryProgram(addr string) bool {
	return addr == factoryPumpFunBondingCurve || addr == factoryPumpFunAMM
}

// emitProfileUpdated inserts a creator_profile_updated system_event into the
// event bus so operators can observe aggregation health. Best-effort: callers
// log the error but do not fail the main processing path.
func emitProfileUpdated(
	ctx context.Context,
	adapter database.Adapter,
	sourceEvt *database.Event,
	chain, creator string,
) error {
	eventID := deriveEventID(sourceEvt.EventID, "creator_profile_updated")

	type profileUpdatedPayload struct {
		Chain          string `json:"chain"`
		CreatorAddress string `json:"creator_address"`
		SourceEventID  string `json:"source_event_id"`
	}
	outEvt, err := makeOutputEvent(
		eventID,
		profileUpdatedPayload{
			Chain:          chain,
			CreatorAddress: creator,
			SourceEventID:  sourceEvt.EventID,
		},
		"creator_profile_updated",
		sourceEvt.TraceID,
		sourceEvt.CorrelationID,
		sourceEvt.EventID,
		sourceEvt.VersionID,
	)
	if err != nil {
		return fmt.Errorf("emitProfileUpdated: %w", err)
	}

	return adapter.InsertEvent(ctx, *outEvt)
}
