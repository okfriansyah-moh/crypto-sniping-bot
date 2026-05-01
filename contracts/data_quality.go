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

	BuyTaxBps        int32 `json:"buy_tax_bps"` // 0–10000
	SellTaxBps       int32 `json:"sell_tax_bps"`
	LpLocked         bool  `json:"lp_locked"`
	LpHolderCount    int32 `json:"lp_holder_count"`
	ContractVerified bool  `json:"contract_verified"`

	RejectReasons []string `json:"reject_reasons"` // enum codes; empty when PASS

	// Per-detector sub-scores (Layer 1 fix). Each is in [0,1] and
	// contributes to RiskScore via the configured weight. They expose the
	// inner attribution so the learning engine and Telegram dispatcher can
	// explain a decision without re-running detectors.
	HoneypotScore float64 `json:"honeypot_score"`
	RugScore      float64 `json:"rug_score"`
	WashScore     float64 `json:"wash_score"`
	FakeLiqScore  float64 `json:"fake_liq_score"`
	TaxScore      float64 `json:"tax_score"`

	// Profile is the operational-mode profile that produced the decision
	// (one of: STRICT | BALANCED | EXPLORATION). Required for replay and
	// for the learning engine to attribute false positives/negatives to the
	// active threshold profile.
	Profile string `json:"profile"`

	// Flags carries non-reject diagnostic codes from the detectors —
	// notably `dq_unknown_*` markers that fire when a detector input was
	// not populated upstream. Always non-nil; may be empty. Distinct from
	// RejectReasons (which is empty unless Decision == "REJECT").
	Flags []string `json:"flags"`

	ExpiresAt   string `json:"expires_at"`   // ISO 8601 UTC; "" = no expiry
	Priority    int32  `json:"priority"`     // higher = processed first; default 0
	EvaluatedAt string `json:"evaluated_at"` // ISO 8601
}
