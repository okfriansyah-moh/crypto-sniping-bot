package contracts

// SystemStateDTO is the singleton DTO describing the current system-wide
// risk posture and operational mode. Not published to the event bus — read
// directly via adapter.GetSystemState. A mode change emits a
// mode_transition_event for audit.
//
// Source file: contracts/system_state.go
// Producer:    internal/modules/risk_controller (background worker)
// Consumer:    execution, selection, capital workers (pre-check gate)
type SystemStateDTO struct {
	// Mode is the current operational mode.
	// One of: BALANCED | STRICT | EXPLORATION | DEGRADED | HALTED
	Mode                 string  `json:"mode"`
	DrawdownPct          float64 `json:"drawdown_pct"` // [0.0, 1.0]
	DrawdownWindowHours  int32   `json:"drawdown_window_hours"`
	OpenPositions        int32   `json:"open_positions"`
	TotalExposureUsd     float64 `json:"total_exposure_usd"`
	ActiveStrategyID     string  `json:"active_strategy_id"` // 16 hex
	ShadowStrategyID     string  `json:"shadow_strategy_id"` // 16 hex or ""
	LastTransitionReason string  `json:"last_transition_reason"`
	UpdatedAt            string  `json:"updated_at"` // ISO 8601
	VersionID            string  `json:"version_id"` // active strategy at update time
	// StateVersion is the CAS counter read from the database.
	// Populated by adapter.GetSystemState; must be passed back to
	// adapter.UpsertSystemState as expectedVersion for optimistic concurrency.
	// Never set by callers directly — treat as read-only after GetSystemState.
	StateVersion int64 `json:"state_version,omitempty"`
}
