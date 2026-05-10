package contracts

// EdgeDTO carries the raw trading edge, pre-validation.
// Emitted by Layer 3 signal & edge discovery.
//
// Source file: contracts/edge.go
// Producer:    internal/modules/edge
// Consumer:    internal/modules/validation
type EdgeDTO struct {
	EventID       string `json:"event_id"`
	TraceID       string `json:"trace_id"`
	CorrelationID string `json:"correlation_id"`
	CausationID   string `json:"causation_id"`
	VersionID     string `json:"version_id"`

	TokenLifecycleID string `json:"token_lifecycle_id"`
	TokenAddress     string `json:"token_address"`

	EdgeType         string  `json:"edge_type"`       // NEW_LAUNCH | MOMENTUM | WALLET_SURGE
	EdgeStrength     float64 `json:"edge_strength"`   // [0.0, 1.0]
	EdgeConfidence   float64 `json:"edge_confidence"` // [0.0, 1.0]
	MomentumScore    float64 `json:"momentum_score"`  // [0.0, 1.0]
	ThresholdApplied float64 `json:"threshold_applied"`

	// §8.4 additive: time window within which entry must complete.
	// Computed at emission: base_ms * (1 + momentum_factor) from config.
	OpportunityWindowMs int32 `json:"opportunity_window_ms"`

	ExpiresAt  string `json:"expires_at"`  // ISO 8601 UTC; "" = no expiry
	Priority   int32  `json:"priority"`    // higher = processed first; default 0
	DetectedAt string `json:"detected_at"` // ISO 8601

	// Phase 11 (Reference-Repo Improvements R2 — DETECT/EDGE) — creator &
	// liquidity-add metadata for early-launch filtering. All optional;
	// zero values mean "unknown / disabled".
	//
	// CreatorAddress: dev wallet that deployed the token (mint authority on
	// Solana, deployer on EVM). Used for per-creator dedup and blacklist.
	// DevBuyPctBps: percentage (in bps, 0..10000) of initial supply bought
	// by the creator wallet inside the launch window — a heuristic for
	// rug-likelihood. m8s-lab uses ~5000 bps as the cap.
	// CreatorRugCount: number of confirmed rugs previously attributable to
	// CreatorAddress. Populated from the creator_blacklist table.
	// DevWalletAgeSeconds: wall-clock age of the creator wallet in
	// seconds. 0 means "unknown / not populated" — the
	// MinDevWalletAgeSeconds gate explicitly skips rejection on 0 so
	// that missing creator metadata never blocks a candidate.
	CreatorAddress      string `json:"creator_address,omitempty"`
	DevBuyPctBps        int32  `json:"dev_buy_pct_bps,omitempty"`
	CreatorRugCount     int32  `json:"creator_rug_count,omitempty"`
	DevWalletAgeSeconds int64  `json:"dev_wallet_age_seconds,omitempty"`

	// EdgeModelVersionID identifies the edge-discovery taxonomy / weight
	// configuration that produced this DTO (additive, transport-only —
	// not yet persisted to the `edges` table; downstream modules MAY use
	// it for attribution and replay differencing). Populated from
	// EdgeConfig.ModelVersion. Zero value means "unversioned legacy".
	EdgeModelVersionID string `json:"edge_model_version_id,omitempty"`

	// RejectReason carries a short token explaining why no edge fired.
	// Empty for accepted edges; populated for EdgeType="NONE".
	// Examples: "no_qualifying_edge", "creator_dev_buy_too_high".
	RejectReason string `json:"reject_reason,omitempty"`

	// P7 — 20-slot time-series bottom detection.
	// BottomDetectionScore is the composite V-shape score in [0, 1].
	// 0 = no bottom pattern / insufficient data.
	// 1 = strong V-shape recovery detected.
	// Populated only when the bottom-detection subsystem is enabled.
	BottomDetectionScore float64 `json:"bottom_detection_score,omitempty"`

	// SlotWindowSize records how many price observations were analysed.
	// 0 means the bottom-detection subsystem was not active.
	SlotWindowSize int32 `json:"slot_window_size,omitempty"`
}

// Edge type taxonomy (per docs/architecture.md § 3.3 and the
// edge-detection skill). Modules MUST NOT invent edges outside this set.
const (
	EdgeTypeNewLaunch = "NEW_LAUNCH_EDGE"
	EdgeTypeMomentum  = "MOMENTUM_EDGE"
	EdgeTypeNone      = "NONE"
)

// IsEdgeDetected reports whether the EdgeDTO represents an actionable
// edge. It is the single canonical predicate used by downstream code
// (workers, validation) — never compare EdgeType to "" directly.
func (e EdgeDTO) IsEdgeDetected() bool {
	return e.EdgeType != "" && e.EdgeType != EdgeTypeNone
}
