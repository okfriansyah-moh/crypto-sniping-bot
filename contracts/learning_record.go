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

Shadow          bool    `json:"shadow"`           // TRUE if this is a rejected-opportunity observation
Outcome         string  `json:"outcome"`          // TP | SL | TIME | RUG | MISSED_PUMP | CORRECT_REJECT
Classification  string  `json:"classification"`   // TP | FP | TN | FN
PnlUsd          float64 `json:"pnl_usd"`          // 0 for shadow; realized PnL for executed
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
}
