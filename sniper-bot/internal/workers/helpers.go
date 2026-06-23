// Package workers contains stage handler implementations for the pipeline event bus.
// Workers are the ONLY components that call adapter methods.
// Modules are pure functions; workers pass them data, persist results, and route events.
package workers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
)

// Decision-reason fallback codes for stage_completed log records when the
// upstream DTO did not enumerate a specific reason. These are stable
// machine-parseable strings — never free-form prose — so downstream
// log parsers and dashboards can group on them.
const (
	// reasonDataQualityRejectFallback is used when DataQualityDTO.Decision
	// is REJECT but RejectReasons is empty (defensive — modules SHOULD
	// always populate the slice).
	reasonDataQualityRejectFallback = "data_quality_reject"

	// reasonValidationRejectFallback is used when ValidatedEdgeDTO.Decision
	// is not ACCEPT but RejectReason is empty.
	reasonValidationRejectFallback = "validation_reject"
)

// dqRejectReason returns a stable, machine-parseable decision_reason for a
// DataQualityDTO REJECT. Reject reason codes are joined with "," so a single
// log field carries the full attribution without nesting.
func dqRejectReason(dq contracts.DataQualityDTO) string {
	if len(dq.RejectReasons) == 0 {
		return reasonDataQualityRejectFallback
	}
	return strings.Join(dq.RejectReasons, ",")
}

// validationRejectReason returns a stable decision_reason for a non-ACCEPT
// ValidatedEdgeDTO. Falls back to a constant when the module did not record
// a structured reason.
func validationRejectReason(reason string) string {
	if reason == "" {
		return reasonValidationRejectFallback
	}
	return reason
}

// urlKeyPathRe matches API keys embedded as a path segment after /v<N>/.
// Covers Infura  wss://mainnet.infura.io/ws/v3/<KEY>
//
//	Alchemy  https://eth-mainnet.g.alchemy.com/v2/<KEY>
var urlKeyPathRe = regexp.MustCompile(`(?i)(/v\d+/)([a-zA-Z0-9_\-]{20,})`)

// urlKeyQueryRe matches API keys embedded as query parameters.
// Covers ?token=KEY &key=KEY &apikey=KEY &api_key=KEY &api-key=KEY (Helius)
var urlKeyQueryRe = regexp.MustCompile(`(?i)([?&](?:token|key|apikey|api[_\-]key)=)([a-zA-Z0-9_\-]+)`)

// urlKeyTrailingRe matches QuickNode-style trailing-segment keys.
var urlKeyTrailingRe = regexp.MustCompile(`(?i)(://[^/]+/)([a-zA-Z0-9]{32,})(/|$)`)

// sanitizeURL masks API keys embedded in RPC endpoint URLs so they are safe
// to include in log output and error messages without leaking credentials.
func sanitizeURL(rawURL string) string {
	s := urlKeyPathRe.ReplaceAllString(rawURL, "${1}[REDACTED]")
	s = urlKeyQueryRe.ReplaceAllString(s, "${1}[REDACTED]")
	s = urlKeyTrailingRe.ReplaceAllString(s, "${1}[REDACTED]${3}")
	return s
}

// deriveEventID computes a deterministic, content-addressable event ID for fan-out
// routing events that share the same FeatureDTO payload but need unique event_ids
// so the event bus ON CONFLICT DO NOTHING semantics remain correct.
// Format: SHA256(eventType + ":" + baseEventID)[:8] → 16 lowercase hex chars.
func deriveEventID(baseEventID, eventType string) string {
	h := sha256.Sum256([]byte(eventType + ":" + baseEventID))
	return hex.EncodeToString(h[:8])
}

// makeOutputEvent serialises dto into a downstream database.Event.
// dtoEventID must already be computed by the module (content-addressable).
// causationID is set to the inbound event's EventID — never empty for pipeline workers.
func makeOutputEvent(
	dtoEventID string,
	dto interface{},
	eventType string,
	traceID, correlationID, causationID, versionID string,
) (*database.Event, error) {
	payload, err := json.Marshal(dto)
	if err != nil {
		return nil, fmt.Errorf("makeOutputEvent: marshal %s: %w", eventType, err)
	}
	var cid *string
	if causationID != "" {
		cid = &causationID
	}
	return &database.Event{
		EventID:       dtoEventID,
		EventType:     eventType,
		Payload:       payload,
		TraceID:       traceID,
		CorrelationID: correlationID,
		CausationID:   cid,
		VersionID:     versionID,
	}, nil
}

// transitionBestEffort applies a lifecycle CAS transition.
// Errors are logged but never propagated — Phase 2 best-effort semantics.
func transitionBestEffort(
	ctx context.Context,
	adapter database.Adapter,
	req database.TransitionRequest,
	logger *slog.Logger,
) {
	if err := adapter.TransitionState(ctx, req); err != nil {
		logger.Warn("lifecycle_transition_failed",
			"lifecycle_id", req.LifecycleID,
			"from", req.ExpectedFromState,
			"to", req.NewState,
			"error", err,
		)
	}
}

// transitionMandatory applies a lifecycle CAS transition.
// Returns an error on failure — Phase 3 mandatory semantics.
// The event stays unprocessed so the worker can retry on next poll.
func transitionMandatory(
	ctx context.Context,
	adapter database.Adapter,
	req database.TransitionRequest,
) error {
	if err := adapter.TransitionState(ctx, req); err != nil {
		return fmt.Errorf("lifecycle_transition %s→%s: %w", req.ExpectedFromState, req.NewState, err)
	}
	return nil
}

// doMandatoryTransition fetches the current lifecycle and performs a mandatory CAS transition.
// Returns an error on any failure — Phase 3 mandatory semantics.
func doMandatoryTransition(
	ctx context.Context,
	adapter database.Adapter,
	lifecycleID, from, to, reason, actor string,
) error {
	lc, err := adapter.GetLifecycle(ctx, lifecycleID)
	if err != nil {
		return fmt.Errorf("fetch_lifecycle %s: %w", lifecycleID, err)
	}
	if lc.CurrentState != from {
		// Idempotent skip: lifecycle already advanced past the expected from-state.
		// This occurs when a stale unprocessed event from a prior session is
		// re-consumed. Return the sentinel so the worker can drain it cleanly.
		return fmt.Errorf("lifecycle_already_advanced %s: state=%q, expected=%q: %w",
			lifecycleID, lc.CurrentState, from, database.ErrLifecycleAlreadyAdvanced)
	}
	return transitionMandatory(ctx, adapter, database.TransitionRequest{
		LifecycleID:       lifecycleID,
		ExpectedFromState: from,
		ExpectedVersion:   lc.StateVersion,
		NewState:          to,
		Reason:            reason,
		ActorWorker:       actor,
	})
}

// Returns (nil, false) on error and logs a warning.
func fetchLifecycle(
	ctx context.Context,
	adapter database.Adapter,
	lifecycleID string,
	logger *slog.Logger,
) (*database.Lifecycle, bool) {
	lc, err := adapter.GetLifecycle(ctx, lifecycleID)
	if err != nil {
		logger.Warn("fetch_lifecycle_failed",
			"lifecycle_id", lifecycleID,
			"error", err,
		)
		return nil, false
	}
	return lc, true
}

// firstChain returns the configured chain key when exactly one chain exists.
// Returns "" when the config is nil, empty, or contains multiple chains.
// Callers MUST handle the empty-string case; "" is never a valid chain key.
func firstChain(cfg *config.Config) string {
	if cfg == nil || len(cfg.Chains) != 1 {
		return ""
	}
	for k := range cfg.Chains {
		return k
	}
	return ""
}

// chainBaseToken returns the first base token address for the given chain from
// config/chains.yaml.  Returns "" when the chain is not configured.
// Used by the execution worker to source the swap path base token deterministically.
func chainBaseToken(cfg *config.Config, chain string) string {
	if cfg == nil {
		return ""
	}
	chainCfg, ok := cfg.Chains[chain]
	if !ok || len(chainCfg.BaseTokens) == 0 {
		return ""
	}
	return chainCfg.BaseTokens[0].Address
}

// allocationSizeFromCorrelation looks up the AllocationDTO for the given
// correlation ID in the event log and returns its SizeUsd.
// Returns 0 when the allocation event cannot be found or decoded.
func allocationSizeFromCorrelation(
	ctx context.Context,
	adapter database.Adapter,
	correlationID string,
	logger *slog.Logger,
) float64 {
	evts, err := adapter.GetEventsByCorrelation(ctx, correlationID)
	if err != nil {
		logger.Warn("allocation_size_from_correlation_failed",
			"correlation_id", correlationID,
			"error", err,
		)
		return 0
	}
	for _, evt := range evts {
		if evt.EventType != "allocation_event" {
			continue
		}
		var dto contracts.AllocationDTO
		if jsonErr := json.Unmarshal(evt.Payload, &dto); jsonErr == nil {
			return dto.SizeUsd
		}
	}
	return 0
}

// creatorFromCorrelation walks the event log for edge_event and returns CreatorAddress.
func creatorFromCorrelation(
	ctx context.Context,
	adapter database.Adapter,
	correlationID string,
	logger *slog.Logger,
) string {
	evts, err := adapter.GetEventsByCorrelation(ctx, correlationID)
	if err != nil {
		logger.Warn("creator_from_correlation_failed",
			"correlation_id", correlationID,
			"error", err,
		)
		return ""
	}
	for _, evt := range evts {
		if evt.EventType != "edge_event" {
			continue
		}
		var dto contracts.EdgeDTO
		if jsonErr := json.Unmarshal(evt.Payload, &dto); jsonErr == nil {
			return dto.CreatorAddress
		}
	}
	return ""
}

// chainFromCorrelation walks the event log to find the chain for a correlation.
// Returns "" if the market_data_event cannot be found or decoded.
func chainFromCorrelation(
	ctx context.Context,
	adapter database.Adapter,
	correlationID string,
	logger *slog.Logger,
) string {
	evts, err := adapter.GetEventsByCorrelation(ctx, correlationID)
	if err != nil {
		logger.Warn("chain_from_correlation_failed",
			"correlation_id", correlationID,
			"error", err,
		)
		return ""
	}
	for _, evt := range evts {
		if evt.EventType != "market_data_event" {
			continue
		}
		var dto contracts.MarketDataDTO
		if jsonErr := json.Unmarshal(evt.Payload, &dto); jsonErr == nil {
			return dto.Chain
		}
	}
	return ""
}

// openPositionBusEventID returns the event_id of the canonical open
// position_state_event row in the events table. Poll snapshots use
// pos-snap:* ids persisted only to the positions table; those must not
// be used as causation_id for bus events (FK → events.event_id).
func openPositionBusEventID(
	ctx context.Context,
	adapter database.Adapter,
	pos contracts.PositionStateDTO,
) string {
	// Position rows keep causation_id = execution_result_event id across snapshots.
	if pos.CausationID != "" && !strings.HasPrefix(pos.CausationID, "pos-snap:") {
		return contracts.ContentIDFromString(fmt.Sprintf("pos-open:%s", pos.CausationID))
	}
	if strings.HasPrefix(pos.EventID, "pos-open:") {
		return pos.EventID
	}
	if exec, err := adapter.GetExecutionByLifecycle(ctx, pos.TokenLifecycleID); err == nil && exec != nil && exec.CausationID != "" {
		// execution_results.event_id may be the allocation_event; causation_id chains to execution_result_event.
		return contracts.ContentIDFromString(fmt.Sprintf("pos-open:%s", exec.CausationID))
	}
	return pos.EventID
}

// mergeIdentityFieldsFromLatest copies creator/symbol/name from the richest
// persisted row when the source DTO is missing them (common on rescan re-emits).
func mergeIdentityFieldsFromLatest(out *contracts.MarketDataDTO, latest *contracts.MarketDataDTO) {
	if out == nil || latest == nil {
		return
	}
	if out.CreatorAddress == "" && latest.CreatorAddress != "" {
		out.CreatorAddress = latest.CreatorAddress
	}
	if out.Symbol == "" && latest.Symbol != "" {
		out.Symbol = latest.Symbol
	}
	if out.Name == "" && latest.Name != "" {
		out.Name = latest.Name
	}
}

// mergeLiquidityFieldsFromLatest copies LP reserve/USD fields from the richest
// persisted row when the rescan re-emit lacks them.
func mergeLiquidityFieldsFromLatest(out *contracts.MarketDataDTO, latest *contracts.MarketDataDTO) {
	if out == nil || latest == nil {
		return
	}
	if out.LiquidityUsd <= 0 && latest.LiquidityUsd > 0 {
		out.LiquidityUsd = latest.LiquidityUsd
	}
	if !out.LpStatsKnown && latest.LpStatsKnown {
		out.LpStatsKnown = true
	}
	if (out.ReserveBaseRaw == "" || out.ReserveBaseRaw == "0") &&
		latest.ReserveBaseRaw != "" && latest.ReserveBaseRaw != "0" {
		out.ReserveBaseRaw = latest.ReserveBaseRaw
	}
	if (out.ReserveTokenRaw == "" || out.ReserveTokenRaw == "0") &&
		latest.ReserveTokenRaw != "" && latest.ReserveTokenRaw != "0" {
		out.ReserveTokenRaw = latest.ReserveTokenRaw
	}
}
