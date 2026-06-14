package contracts

// LearningRecordDTO carries the per-trade and per-shadow-trade record
// for the Layer 10 learning engine.
// RecordID = SHA256(token_lifecycle_id||shadow_flag)[:16].
//
// Source file: contracts/learning_record.go
// Producer:    internal/modules/learning
// Consumer:    internal/modules/learning (self-consuming for parameter updates)
type LearningRecordDTO struct {
	EventID       string `json:"event_id"`
	TraceID       string `json:"trace_id"`
	CorrelationID string `json:"correlation_id"`
	CausationID   string `json:"causation_id"`
	VersionID     string `json:"version_id"`

	RecordID         string `json:"record_id"` // SHA256(token_lifecycle_id||shadow_flag)[:16]
	TokenLifecycleID string `json:"token_lifecycle_id"`

	Shadow          bool    `json:"shadow"`         // TRUE if this is a rejected-opportunity observation
	Outcome         string  `json:"outcome"`        // TP | SL | TIME | RUG | MISSED_PUMP | CORRECT_REJECT
	Classification  string  `json:"classification"` // TP | FP | TN | FN
	PnlUsd          float64 `json:"pnl_usd"`        // 0 for shadow; realized PnL for executed
	PnlPct          float64 `json:"pnl_pct"`
	PredictionError float64 `json:"prediction_error"`
	Cohort          string  `json:"cohort"` // "liquidity_bucket:age_bucket:source"

	FeaturesSnapshot  FeatureDTO       `json:"features_snapshot"`
	EdgeSnapshot      EdgeDTO          `json:"edge_snapshot"`
	ValidatedSnapshot ValidatedEdgeDTO `json:"validated_snapshot"`

	// §8.8 additive: shadow execution and strategy tracking fields.
	Simulated      bool   `json:"simulated"`       // true if from shadow execution mode
	ExpiredSource  bool   `json:"expired_source"`  // true if record derived from expired_event
	StrategyStatus string `json:"strategy_status"` // "active" | "shadow" at record time

	ExpiresAt  string `json:"expires_at"`  // ISO 8601 UTC; "" = no expiry
	Priority   int32  `json:"priority"`    // higher = processed first; default 0
	RecordedAt string `json:"recorded_at"` // ISO 8601

	// SybilClusterIndicators carries proxy signals that the wash-detector
	// missed because volume was spread across many distinct wallets
	// (residual risk #5 — F-SEC-08). Currently populated from coarse
	// heuristics on the suspicious "wash detector said OK but trade
	// lost" case; full funding-graph clustering is a follow-up.
	// nil on every other record (wins, true-rugs caught by wash, etc.).
	SybilClusterIndicators *SybilIndicators `json:"sybil_cluster_indicators,omitempty"`

	// ─────────────────────────────────────────────────────────────────────
	// AI Loss Explanation — populated by the loss_explainer (Layer 10).
	//
	// AIExplanationKnown=false means the loss_explainer has not run or
	// encountered an error (fail-open). Operators inspect AIExplanation
	// to understand systemic loss patterns without reading raw DTO chains.
	//
	// AILossCategory: canonical bucket for the root cause
	//   (timing|scam|momentum_fade|execution|data_quality|narrative|unknown).
	// AIExplanation: human-readable explanation, max 200 chars.
	// ─────────────────────────────────────────────────────────────────────
	AIExplanationKnown bool   `json:"ai_explanation_known,omitempty"`
	AILossCategory     string `json:"ai_loss_category,omitempty"`
	AIExplanation      string `json:"ai_explanation,omitempty"`
}

// SybilIndicators bundles wallet-distribution proxy signals used to
// flag a losing trade as a probable wash-trading bypass via Sybil
// wallets. SuspectClusterSize and FundingSourceShared are reserved for
// the follow-up funding-graph analyzer; they are 0 / false today.
type SybilIndicators struct {
	UniqueWallets1m     int     `json:"unique_wallets_1m"`
	WalletEntropyNats   float64 `json:"wallet_entropy_nats"`
	SuspectClusterSize  int     `json:"suspect_cluster_size"`  // 0 = not yet computed
	FundingSourceShared bool    `json:"funding_source_shared"` // false today; reserved for follow-up
}
