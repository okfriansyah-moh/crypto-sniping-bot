package contracts

// Decision values for DataQualityDTO.Decision.
//
// PASS          — token meets all quality gates; emit data_quality_event with full DTO.
// RISKY_PASS    — token passes but with elevated risk; capital engine applies smaller allocation.
// REJECT        — token fails one or more gates; emit data_quality_event; contributes to reject-rate.
// SKIP (NEW)    — token silently dropped (mode-aware serial launcher); do NOT emit data_quality_event;
//
//	do NOT contribute to reject-rate; token_lifecycle transitions to `skipped`.
//	Used exclusively by EXPLORATION / VERY_EXPLORATION modes when a serial-launcher
//	token fails the quality gate (see PRODUCTION_GATE_ANALYSIS §9).
//	SKIP must NOT contribute to reject-rate statistics in Layer 10.
const (
	DecisionPass      = "PASS"
	DecisionRiskyPass = "RISKY_PASS"
	DecisionReject    = "REJECT"
	// DecisionSkip is the silent-drop decision for EXPLORATION/VERY_EXPLORATION mode.
	// Tokens assigned SKIP are dropped without emitting a data_quality_event and
	// without counting toward reject-rate metrics. The token_lifecycle is updated to
	// the terminal `skipped` state so traceability is preserved.
	DecisionSkip = "SKIP"
)

// Canonical flag values emitted in DataQualityDTO.Flags.
//
// serial_launcher_monitored — emitted alongside a RISKY_PASS decision when the
//
//	creator has launched previous tokens but remains below the hard-reject threshold
//	in EXPLORATION/VERY_EXPLORATION mode. Layer 7 applies a smaller allocation;
//	Layer 9 applies tighter TP1, tighter trailing stop, and kill-switch priority.
//
// serial_launcher_skipped — emitted alongside a SKIP decision when the creator
//
//	exceeds the serial-launcher threshold in EXPLORATION/VERY_EXPLORATION mode.
//	Informational only; not used by downstream layers (token is dropped silently).
const (
	FlagSerialLauncherMonitored = "serial_launcher_monitored"
	FlagSerialLauncherSkipped   = "serial_launcher_skipped" // legacy; prefer granular flags below

	FlagSerialLauncherSkippedNoSocial      = "serial_launcher_skipped:no_social"
	FlagSerialLauncherSkippedLowHolders    = "serial_launcher_skipped:low_holders"
	FlagSerialLauncherSkippedHolderUnknown = "serial_launcher_skipped:holder_unknown"
	FlagSerialLauncherSkippedRisk          = "serial_launcher_skipped:risk"
)

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

	Decision  string  `json:"decision"`   // PASS | REJECT | RISKY_PASS | SKIP
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
	// (one of: STRICT | BALANCED | EXPLORATION | VERY_EXPLORATION). Required for replay and
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

	// ── External provider fields (P1 — additive) ──────────────────────────
	// ExternalProviderScore is the weighted-average risk score from external
	// validation providers (e.g. rugcheck.xyz). Range [0.0, 1.0].
	// Zero when providers are disabled or when no supported-chain provider ran.
	ExternalProviderScore float64 `json:"external_provider_score,omitempty"`

	// ProviderFlags carries provider-specific diagnostic codes collected from
	// external validation providers (e.g. "rugcheck:FREEZE_AUTHORITY_ENABLED").
	// Always non-nil when providers ran; may be empty. Format: "provider:FLAG".
	ProviderFlags []string `json:"provider_flags,omitempty"`

	// ProvidersDegraded is true when at least one configured provider returned
	// a partial or no response (timeout, HTTP error, parse failure).
	// The pipeline continues regardless; this field is for observability only.
	ProvidersDegraded bool `json:"providers_degraded,omitempty"`

	// ── BirdEye enrichment fields (P3 — additive) ─────────────────────────
	// CreatorRiskScore is the creator wallet's holding percentage expressed as
	// a risk signal in [0, 1].  Set by BirdEye provider; zero when not available.
	CreatorRiskScore float64 `json:"creator_risk_score,omitempty"`

	// LpLockPct is the percentage [0, 100] of LP tokens locked per BirdEye.
	// Zero when unavailable (not the same as "unlocked").
	LpLockPct float64 `json:"lp_lock_pct,omitempty"`
}
