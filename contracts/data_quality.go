package contracts

// DataQualityDTO carries the pass/reject decision with risk attribution.
// Emitted after static and heuristic checks in Layer 1.
//
// Source file: contracts/data_quality.go
// Producer:    internal/modules/data_quality
// Consumer:    internal/modules/features (PASS / RISKY_PASS only)
type DataQualityDTO struct {
EventID       string `json:"event_id"`
TraceID       string `json:"trace_id"`
CorrelationID string `json:"correlation_id"`
CausationID   string `json:"causation_id"`
VersionID     string `json:"version_id"`

TokenLifecycleID string `json:"token_lifecycle_id"`
TokenAddress     string `json:"token_address"`
Chain            string `json:"chain"`

Decision  string  `json:"decision"`   // PASS | REJECT | RISKY_PASS
RiskScore float64 `json:"risk_score"` // [0.0, 1.0] — higher = riskier

IsHoneypot      bool `json:"is_honeypot"`
IsFakeLiquidity bool `json:"is_fake_liquidity"`
IsWashTrading   bool `json:"is_wash_trading"`
IsRugRisk       bool `json:"is_rug_risk"`
IsTaxAnomaly    bool `json:"is_tax_anomaly"`

BuyTaxBps        int32 `json:"buy_tax_bps"`  // 0–10000
SellTaxBps       int32 `json:"sell_tax_bps"`
LpLocked         bool  `json:"lp_locked"`
LpHolderCount    int32 `json:"lp_holder_count"`
ContractVerified bool  `json:"contract_verified"`

RejectReasons []string `json:"reject_reasons"` // enum codes; empty when PASS

ExpiresAt   string `json:"expires_at"`   // ISO 8601 UTC; "" = no expiry
Priority    int32  `json:"priority"`     // higher = processed first; default 0
EvaluatedAt string `json:"evaluated_at"` // ISO 8601
}
