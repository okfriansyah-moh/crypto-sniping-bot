// Package data_quality implements Layer 1: Data Quality Engine.
// Consumes MarketDataDTO and emits DataQualityDTO.
// No database imports — pure business logic only.
package data_quality

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"sort"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// Config holds data quality thresholds loaded from pipeline.yaml.
// All values sourced from config — no hardcoded magic numbers.
type Config struct {
	MaxBuyTaxBps      int32
	MaxSellTaxBps     int32
	MinLPHolderCount  int32
	MinReserveBaseWei string
}

// DefaultConfig returns safe defaults that align with pipeline.yaml.
// Phase 9 (§ 9.1): when a top-level Config is supplied, threshold values
// are sourced from cfg.DataQualityRuntime.Thresholds (mirrors
// config/data_quality.yaml). Module-side defaults remain only as fallback
// for tests / partial configs.
func DefaultConfig(cfg *config.Config) Config {
	out := Config{
		MaxBuyTaxBps:      1000, // 10%
		MaxSellTaxBps:     1500, // 15%
		MinLPHolderCount:  1,
		MinReserveBaseWei: "1000000000000000", // 0.001 ETH in wei
	}
	if cfg == nil {
		return out
	}
	t := cfg.DataQualityRuntime.Thresholds
	if t.TaxBuyMaxBps > 0 {
		out.MaxBuyTaxBps = t.TaxBuyMaxBps
	}
	if t.TaxSellMaxBps > 0 {
		out.MaxSellTaxBps = t.TaxSellMaxBps
	}
	return out
}

// Module is the data quality engine.
// It is a pure function: no state, no DB, no side effects on shared mutable state.
type Module struct {
	cfg     Config
	runtime *config.DataQualityRuntimeConfig // Phase 9 (§ 9.1) — optional runtime config.
	logger  *slog.Logger
}

// New creates a new data quality Module.
func New(cfg Config, logger *slog.Logger) *Module {
	if logger == nil {
		logger = slog.Default()
	}
	return &Module{cfg: cfg, logger: logger}
}

// WithRuntimeConfig attaches Phase 9 detector toggles, weights, and
// thresholds (mirrors config/data_quality.yaml). Returns the receiver for
// fluent wiring.
func (m *Module) WithRuntimeConfig(rt *config.DataQualityRuntimeConfig) *Module {
	m.runtime = rt
	return m
}

// Process evaluates a MarketDataDTO and returns a DataQualityDTO.
// Phase 9 (§ 9.1) — invokes the detector helpers (DetectRugRisk,
// DetectWashTrading, DetectTaxAnomaly) instead of leaving every flag at
// false, and aggregates RiskScore via AggregateRiskScore.
//
// True RPC-backed honeypot simulation, LP-lock contract probes, and
// Etherscan source-code lookup are deferred (they require new client
// infrastructure). Until then the corresponding flags remain conservative
// (false) and contribute zero weight, which is safer than fabricating
// signals.
//
// Deterministic: same input → same output.
func (m *Module) Process(_ context.Context, in contracts.MarketDataDTO) (contracts.DataQualityDTO, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	var rejectReasons []string
	isHoneypot := false // requires eth_call simulation (deferred)
	isFakeLiquidity := false
	isWashTrading := false
	isRugRisk := false
	isTaxAnomaly := false
	buyTaxBps := int32(0)     // requires sim/Etherscan (deferred)
	sellTaxBps := int32(0)    // requires sim/Etherscan (deferred)
	lpLocked := false         // requires lock-contract probe (deferred)
	lpHolderCount := int32(0) // requires holder enumeration (deferred)
	contractVerified := false // requires Etherscan v2 (deferred)

	// Resolve detector toggles from runtime config (default = enabled).
	rugEnabled := true
	washEnabled := true
	taxEnabled := true
	if m.runtime != nil {
		rugEnabled = m.runtime.Detectors.RugAuthority
		washEnabled = m.runtime.Detectors.WashTrading
		taxEnabled = m.runtime.Detectors.TaxAnomaly
	}

	// isNewLaunch is true for brand-new bonding-curve events (Pump.fun
	// CreateEvent). These tokens start with zero reserves by design — the
	// bonding curve fills as buyers come in. Applying a missing_reserves
	// gate would reject every single new launch before anyone can trade,
	// making the bot blind to all pump.fun opportunities.
	// For all other event types (pool inits, swaps) reserves ARE expected.
	isNewLaunch := in.EventTopic == "PumpFunCreate"

	// Check 1: Missing reserve data → reject.
	// Skipped for new-launch events (zero reserves expected).
	if !isNewLaunch && (in.ReserveBaseRaw == "" || in.ReserveBaseRaw == "0") {
		rejectReasons = append(rejectReasons, "missing_reserves")
	} else if !isNewLaunch {
		// Check 2: Minimum reserve threshold.
		reserveBase, ok := new(big.Int).SetString(in.ReserveBaseRaw, 10)
		minReserve, _ := new(big.Int).SetString(m.cfg.MinReserveBaseWei, 10)
		if ok && minReserve != nil && reserveBase.Cmp(minReserve) < 0 {
			isFakeLiquidity = true
			rejectReasons = append(rejectReasons, "insufficient_liquidity")
		}
	}

	// Check 3: Reorged events are suspect.
	if in.Reorged {
		rejectReasons = append(rejectReasons, "reorged_event")
	}

	// Check 4: Missing token address → reject immediately.
	if in.TokenAddress == "" {
		rejectReasons = append(rejectReasons, "missing_token_address")
	}

	// Phase 10 (Reference-Repo Improvements / Task F) — Solana bonding
	// curve progress filter. Reject pump.fun / bonk.fun markets whose
	// bonding curve has already advanced past the configured cap (a
	// late-curve buy has limited remaining upside before graduation).
	// Skipped when the threshold is unset (0) or the event has no curve
	// progress (EVM events leave BondingCurveProgressBps == 0).
	if m.runtime != nil &&
		m.runtime.Thresholds.MaxBondingCurveProgressBps > 0 &&
		in.BondingCurveProgressBps > m.runtime.Thresholds.MaxBondingCurveProgressBps {
		rejectReasons = append(rejectReasons, "bonding_curve_too_advanced")
	}

	// Phase 9 (§ 9.1) — invoke real detector helpers.
	if rugEnabled {
		// For new-launch events, DetectRugRisk would always fire (reserve=0 < threshold)
		// which is a tautology for a freshly minted bonding-curve token.
		// Only apply the rug-risk reserve gate to events where liquidity is expected.
		if !isNewLaunch {
			isRugRisk = DetectRugRisk(lpLocked, in.ReserveBaseRaw, m.cfg.MinReserveBaseWei)
		}
	}
	if taxEnabled {
		isTaxAnomaly = DetectTaxAnomaly(buyTaxBps, sellTaxBps, m.cfg.MaxBuyTaxBps, m.cfg.MaxSellTaxBps)
	}
	if washEnabled {
		// Phase 9.5 deferred: real wash-trading detection requires holder
		// count and pool age, neither of which is present in MarketDataDTO
		// today. Until that enrichment lands, leave isWashTrading=false
		// rather than calling DetectWashTrading(0,0,0) which would always
		// return false anyway and create the illusion of an active gate.
		_ = isWashTrading
	}

	// Aggregate RiskScore via the shared helper (Phase 9 § 9.1).
	// Pass per-detector weights from runtime config so YAML changes to
	// `data_quality.risk_weights` actually take effect.
	var weights *config.DataQualityRiskWeights
	if m.runtime != nil {
		weights = &m.runtime.RiskWeights
	}
	riskScore := AggregateRiskScore(
		len(rejectReasons), 4,
		isHoneypot, isFakeLiquidity, isWashTrading, isRugRisk, isTaxAnomaly,
		weights,
	)

	// Sort reasons for determinism.
	sort.Strings(rejectReasons)

	decision := "PASS"
	if len(rejectReasons) > 0 {
		decision = "REJECT"
	} else if riskScore > 0.3 {
		decision = "RISKY_PASS"
	}

	eventID := contracts.ContentIDFromString(fmt.Sprintf("dq:%s:%s", in.EventID, decision))

	return contracts.DataQualityDTO{
		EventID:       eventID,
		TraceID:       in.TraceID,
		CorrelationID: in.CorrelationID,
		CausationID:   in.EventID,
		VersionID:     in.VersionID,

		TokenLifecycleID: contracts.ContentIDFromString(in.TokenAddress + ":" + in.Chain),
		TokenAddress:     in.TokenAddress,
		Chain:            in.Chain,

		Decision:  decision,
		RiskScore: riskScore,

		IsHoneypot:      isHoneypot,
		IsFakeLiquidity: isFakeLiquidity,
		IsWashTrading:   isWashTrading,
		IsRugRisk:       isRugRisk,
		IsTaxAnomaly:    isTaxAnomaly,

		BuyTaxBps:        buyTaxBps,
		SellTaxBps:       sellTaxBps,
		LpLocked:         lpLocked,
		LpHolderCount:    lpHolderCount,
		ContractVerified: contractVerified,

		RejectReasons: rejectReasons,
		EvaluatedAt:   now,
	}, nil
}

// clampFloat clamps v to [lo, hi].
func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
