// Package evaluation implements the Evaluation Engine (Layer 10 pre-learning gate).
// Process joins a PositionStateDTO (exited) with stored ExecutionResultDTO and
// shadow_trades to compute PredictionError, FalsePositive, FalseNegative, and
// ExecutionError, then emits EvaluationDTO.
//
// The evaluation engine MUST run before the Learning Engine — Phase 5 MUST NOT
// run without evaluation_event as input.
//
// Pure function: no DB, no side effects. Adapter calls happen in the worker.
package evaluation

import (
	"context"
	"fmt"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
)

// Module is the evaluation engine.
type Module struct {
	cfg config.EvaluationConfig
}

// New returns a new evaluation Module.
func New(cfg config.EvaluationConfig) *Module {
	if cfg.FPLossThresholdPct == 0 {
		cfg.FPLossThresholdPct = -5.0
	}
	if cfg.FNGainThresholdPct == 0 {
		cfg.FNGainThresholdPct = 20.0
	}
	if cfg.WindowSeconds == 0 {
		cfg.WindowSeconds = 3600
	}
	return &Module{cfg: cfg}
}

// EvaluationInput bundles all data needed to evaluate a single exited position.
type EvaluationInput struct {
	Position     contracts.PositionStateDTO
	Execution    *contracts.ExecutionResultDTO // nil if not found
	ShadowTrades []database.ShadowTrade        // FN candidates in the evaluation window
}

// Process computes the EvaluationDTO from an exited PositionStateDTO.
// It uses the ExecutionResultDTO for ExecutionError and ShadowTrades for FalseNegative.
//
// The caller (worker) fetches execution and shadow_trades from the adapter and
// passes them via EvaluationInput so this function remains a pure computation.
func (m *Module) Process(_ context.Context, in EvaluationInput) (contracts.EvaluationDTO, error) {
	pos := in.Position
	if pos.Status != "exited" {
		return contracts.EvaluationDTO{}, fmt.Errorf("evaluation: position %s status=%q; expected 'exited'",
			pos.PositionID, pos.Status)
	}

	now := time.Now().UTC()
	windowEnd := now.Format(time.RFC3339Nano)
	windowStart := now.Add(-time.Duration(m.cfg.WindowSeconds) * time.Second).Format(time.RFC3339Nano)

	// ── Per-position error signals ─────────────────────────────────────────
	// PredictionError: WinProbability − actual_outcome
	// Phase 3 uses 0 as WinProbability since probability models come in Phase 4.
	winProbability := 0.0
	actualOutcome := 0.0
	if pos.PnlPct > 0 {
		actualOutcome = 1.0
	}
	predictionError := winProbability - actualOutcome

	// FalsePositive: accepted by pipeline AND PnlPct < threshold
	falsePositive := pos.PnlPct < m.cfg.FPLossThresholdPct

	// ExecutionError: AllocationDTO.MaxSlippageBps − realizedSlippageBps
	executionError := int32(0)
	if in.Execution != nil {
		executionError = int32(0) - in.Execution.SlippageRealizedBps // signed difference from 0-guard
	}

	// FalseNegative: count shadow trades that pumped above FN threshold
	fnCount := int32(0)
	for _, st := range in.ShadowTrades {
		if st.PeakGainPct > m.cfg.FNGainThresholdPct {
			fnCount++
		}
	}

	// Aggregate counts for window (single sample)
	tpCount := int32(0)
	fpCount := int32(0)
	if falsePositive {
		fpCount = 1
	} else if pos.PnlPct > 0 {
		tpCount = 1
	}

	// BrierScore = (predicted − actual)^2
	brierScore := predictionError * predictionError

	// Expectancy: P × avgWin − (1−P) × avgLoss (single-sample estimate)
	expectancy := 0.0
	if pos.PnlPct > 0 {
		expectancy = pos.PnlPct
	} else {
		expectancy = pos.PnlPct // negative loss
	}

	// ExecutionError as float for DTO (bps as float)
	_ = executionError // stored in DB via adapter; included in evaluation context

	evaluationID := contracts.ContentIDFromString(fmt.Sprintf("eval:%s:%s", pos.PositionID, pos.ExitedAt))

	return contracts.EvaluationDTO{
		EventID:       contracts.ContentIDFromString(fmt.Sprintf("eval-evt:%s:%s", pos.PositionID, pos.ExitedAt)),
		TraceID:       pos.TraceID,
		CorrelationID: pos.CorrelationID,
		CausationID:   pos.EventID,
		VersionID:     pos.VersionID,

		EvaluationID: evaluationID,
		WindowStart:  windowStart,
		WindowEnd:    windowEnd,
		SampleSize:   1,

		TruePositiveCount:  tpCount,
		FalsePositiveCount: fpCount,
		TrueNegativeCount:  0,
		FalseNegativeCount: fnCount,

		Expectancy:          expectancy,
		MaxDrawdownPct:      minPnl(pos.PnlPct),
		BrierScore:          brierScore,
		PredictionErrorMean: predictionError,

		EvaluatedAt: now.Format(time.RFC3339Nano),
	}, nil
}

func minPnl(pnl float64) float64 {
	if pnl < 0 {
		return -pnl // drawdown is positive magnitude
	}
	return 0
}
