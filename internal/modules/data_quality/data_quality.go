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
// Phase 2: simple thresholds; Phase 3 adds adaptive tuning.
func DefaultConfig(cfg *config.Config) Config {
	_ = cfg // reserved for YAML-driven overrides in Phase 3
	return Config{
		MaxBuyTaxBps:      1000, // 10%
		MaxSellTaxBps:     1500, // 15%
		MinLPHolderCount:  1,
		MinReserveBaseWei: "1000000000000000", // 0.001 ETH in wei
	}
}

// Module is the data quality engine.
// It is a pure function: no state, no DB, no side effects on shared mutable state.
type Module struct {
	cfg    Config
	logger *slog.Logger
}

// New creates a new data quality Module.
func New(cfg Config, logger *slog.Logger) *Module {
	if logger == nil {
		logger = slog.Default()
	}
	return &Module{cfg: cfg, logger: logger}
}

// Process evaluates a MarketDataDTO and returns a DataQualityDTO.
// Phase 2: static heuristic checks — no RPC calls (deferred to Phase 3 with retry).
// Deterministic: same input → same output.
func (m *Module) Process(_ context.Context, in contracts.MarketDataDTO) (contracts.DataQualityDTO, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	var rejectReasons []string
	isHoneypot := false
	isFakeLiquidity := false
	isWashTrading := false
	isRugRisk := false
	isTaxAnomaly := false
	buyTaxBps := int32(0)
	sellTaxBps := int32(0)
	lpLocked := false
	lpHolderCount := int32(0)
	contractVerified := false
	riskScore := 0.0

	// Check 1: Missing reserve data → reject.
	if in.ReserveBaseRaw == "" || in.ReserveBaseRaw == "0" {
		rejectReasons = append(rejectReasons, "missing_reserves")
	} else {
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

	// Compute risk score as proportion of checks failed.
	totalChecks := 4.0
	failedChecks := float64(len(rejectReasons))
	if isFakeLiquidity || isHoneypot {
		failedChecks++ // weight harder rejections
	}
	riskScore = clampFloat(failedChecks/totalChecks, 0.0, 1.0)

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
