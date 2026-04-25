package contracts

// PositionStateDTO is the position state snapshot emitted by Layer 9.
// The same DTO is emitted multiple times across a position's lifetime
// (each emission is a state snapshot — open, price update, exit).
// PositionID = SHA256(execution_id)[:16].
//
// Source file: contracts/position.go
// Producer:    internal/modules/position
// Consumer:    internal/modules/learning (on exit only)
type PositionStateDTO struct {
EventID       string `json:"event_id"`
TraceID       string `json:"trace_id"`
CorrelationID string `json:"correlation_id"`
CausationID   string `json:"causation_id"`
VersionID     string `json:"version_id"`

TokenLifecycleID string `json:"token_lifecycle_id"`
PositionID       string `json:"position_id"`  // SHA256(execution_id)[:16]
ExecutionID      string `json:"execution_id"`
TokenAddress     string `json:"token_address"`
Chain            string `json:"chain"`

Status       string  `json:"status"`        // open | exited | failed
EntryPrice   string  `json:"entry_price"`   // decimal string
EntrySizeUsd float64 `json:"entry_size_usd"`
CurrentPrice string  `json:"current_price"` // decimal string; "" if not polled yet

ExitPrice  string  `json:"exit_price"`  // empty until exited
ExitReason string  `json:"exit_reason"` // TP1 | TP2 | SL | TIME | MANUAL; empty until exited
PnlUsd     float64 `json:"pnl_usd"`     // 0 until exited
PnlPct     float64 `json:"pnl_pct"`     // 0 until exited

Tp1Bps         int32 `json:"tp1_bps"`
Tp2Bps         int32 `json:"tp2_bps"`
SlBps          int32 `json:"sl_bps"`
MaxHoldSeconds int32 `json:"max_hold_seconds"`

ExpiresAt  string `json:"expires_at"`  // ISO 8601 UTC; "" = no expiry
Priority   int32  `json:"priority"`    // higher = processed first; default 0
OpenedAt   string `json:"opened_at"`   // ISO 8601
ExitedAt   string `json:"exited_at"`   // empty until exited
SnapshotAt string `json:"snapshot_at"` // ISO 8601
}
