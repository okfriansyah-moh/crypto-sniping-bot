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
	// DevWalletAgeSeconds: 0 = brand-new wallet (highest risk).
	CreatorAddress      string `json:"creator_address,omitempty"`
	DevBuyPctBps        int32  `json:"dev_buy_pct_bps,omitempty"`
	CreatorRugCount     int32  `json:"creator_rug_count,omitempty"`
	DevWalletAgeSeconds int64  `json:"dev_wallet_age_seconds,omitempty"`
}
