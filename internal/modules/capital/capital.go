// Package capital implements Layer 7: Capital Engine.
// Consumes SelectionOutputDTO and emits AllocationDTO.
// Pure function: no DB, no side effects.
package capital

import (
"context"
"fmt"
"time"

"crypto-sniping-bot/contracts"
"crypto-sniping-bot/internal/app/config"
)

// Module is the capital allocation engine.
type Module struct {
cfg *config.CapitalConfig
}

// New returns a new capital Module.
func New(cfg *config.CapitalConfig) *Module {
if cfg == nil {
cfg = &config.CapitalConfig{
FixedEntrySizeUsd:   10.0,
MaxSizeUsd:          100.0,
TTLSeconds:          3,
}
}
return &Module{cfg: cfg}
}

// Process computes the capital allocation for a selected trade.
// Phase 2: fixed base allocation; Phase 7 adds Kelly-adjacent sizing.
// ExecutionID is content-addressable: SHA256(trace_id || version_id || token_address || chain) per architecture § 4.10.D.2.
func (m *Module) Process(_ context.Context, in contracts.SelectionOutputDTO, chain string) (contracts.AllocationDTO, error) {
nowTime := time.Now().UTC()
now := nowTime.Format(time.RFC3339Nano)

if !in.Selected {
// Emit rejected allocation to propagate downstream.
eventID := contracts.ContentIDFromString(fmt.Sprintf("alloc-skip:%s", in.EventID))
return contracts.AllocationDTO{
EventID:       eventID,
TraceID:       in.TraceID,
CorrelationID: in.CorrelationID,
CausationID:   in.EventID,
VersionID:     in.VersionID,

TokenLifecycleID: in.TokenLifecycleID,
TokenAddress:     in.TokenAddress,
Chain:            chain,

ExecutionID:  "",
SizeUsd:      0,
SizeBaseRaw:  "0",
Rejected:     true,
RejectReason: in.RejectReason,
AllocatedAt:  now,
}, nil
}

sizeUsd := m.cfg.FixedEntrySizeUsd
if sizeUsd > m.cfg.MaxSizeUsd {
sizeUsd = m.cfg.MaxSizeUsd
}

expiresAt := nowTime.Add(
time.Duration(m.cfg.TTLSeconds) * time.Second,
).Format(time.RFC3339Nano)

// ExecutionID: content-addressable per architecture § 4.10.D.2.
executionID := contracts.ContentIDFromString(in.TraceID + in.VersionID + in.TokenAddress + chain)

eventID := contracts.ContentIDFromString(fmt.Sprintf("alloc:%s", in.EventID))

return contracts.AllocationDTO{
EventID:       eventID,
TraceID:       in.TraceID,
CorrelationID: in.CorrelationID,
CausationID:   in.EventID,
VersionID:     in.VersionID,

TokenLifecycleID: in.TokenLifecycleID,
TokenAddress:     in.TokenAddress,
Chain:            chain,

ExecutionID:    executionID,
SizeUsd:        sizeUsd,
SizeBaseRaw:    "0", // set by worker after price lookup
MaxSlippageBps: 200,
WalletAddress:  m.cfg.WalletAddress,
WalletShard:    0,

Rejected:    false,
RejectReason: "",
CohortID:    "default",

ExpiresAt:   expiresAt,
AllocatedAt: now,
}, nil
}
