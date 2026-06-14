package contracts

// SelectionOutputDTO carries the top-K selection result from Layer 6.
//
// Source file: contracts/selection.go
// Producer:    internal/modules/selection
// Consumer:    internal/modules/capital
type SelectionOutputDTO struct {
	EventID       string `json:"event_id"`
	TraceID       string `json:"trace_id"`
	CorrelationID string `json:"correlation_id"`
	CausationID   string `json:"causation_id"`
	VersionID     string `json:"version_id"`

	TokenLifecycleID string `json:"token_lifecycle_id"`
	TokenAddress     string `json:"token_address"`

	Selected        bool    `json:"selected"`
	Rank            int32   `json:"rank"`           // 1-based
	CombinedScore   float64 `json:"combined_score"` // edge × prob × confidence
	DiversityBucket string  `json:"diversity_bucket"`
	IsExploration   bool    `json:"is_exploration"` // explore-band pick
	RejectReason    string  `json:"reject_reason"`  // empty if Selected

	ExpiresAt  string `json:"expires_at"`  // ISO 8601 UTC; "" = no expiry
	Priority   int32  `json:"priority"`    // higher = processed first; default 0
	SelectedAt string `json:"selected_at"` // ISO 8601

	// Phase 11 (Reference-Repo Improvements R2 — SELECT) — creator address
	// propagated from EdgeDTO so the per-creator dedup gate can run
	// without a DB lookup. Empty = unknown.
	CreatorAddress string `json:"creator_address,omitempty"`
}
