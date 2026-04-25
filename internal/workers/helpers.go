// Package workers contains stage handler implementations for the pipeline event bus.
// Workers are the ONLY components that call adapter methods.
// Modules are pure functions; workers pass them data, persist results, and route events.
package workers

import (
"context"
"encoding/json"
"fmt"
"log/slog"

"crypto-sniping-bot/contracts"
"crypto-sniping-bot/database"
"crypto-sniping-bot/internal/app/config"
)

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

// fetchLifecycle retrieves the current lifecycle for CAS guard values.
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

// firstChain returns the first chain key from the pipeline config.
// Used in Phase 2 where a single chain is assumed.
func firstChain(cfg *config.Config) string {
for k := range cfg.Chains {
return k
}
return "eth-uniswap-v2"
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
