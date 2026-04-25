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

	RecordID         string `json:"record_id"`          // SHA256(token_lifecycle_id||shadow_flag)[:16]
	TokenLifecycleID string `json:"token_lifecycle_id"`

	Shadow         bool    `json:"shadow"`          // TRUE if this is a rejected-opportunity observation
	Outcome        string  `json:"outcome"`         // TP | SL | TIME | RUG | MISSED_PUMP | CORRECT_REJECT
	Classification string  `json:"classification"`  // TP | FP | TN | FN
	PnlUsd         float64 `json:"pnl_usd"`         // 0 for shadow; realized PnL for executed
	PnlPct         float64 `json:"pnl_pct"`
	PredictionError float64 `json:"prediction_error"`
	Cohort         string  `json:"cohort"` // "liquidity_bucket:age_bucket:source"

	FeaturesSnapshot  FeatureDTO       `json:"features_snapshot"`
	EdgeSnapshot      EdgeDTO          `json:"edge_snapshot"`
	ValidatedSnapshot ValidatedEdgeDTO `json:"validated_snapshot"`

	RecordedAt string `json:"recorded_at"` // ISO 8601
}
