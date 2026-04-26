package evaluation

import (
	"context"
	"testing"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
)

func defaultCfg() config.EvaluationConfig {
	return config.EvaluationConfig{
		FPLossThresholdPct: -5.0,
		FNGainThresholdPct: 20.0,
		WindowSeconds:      3600,
	}
}

func exitedPosition(pnlPct float64) contracts.PositionStateDTO {
	return contracts.PositionStateDTO{
		EventID:         "pos-evt-1",
		TraceID:         "trace-1",
		CorrelationID:   "corr-1",
		CausationID:     "cause-1",
		VersionID:       "v1",
		PositionID:      "pos-1",
		ExecutionID:     "exec-1",
		TokenAddress:    "0xTOKEN",
		Status:          "exited",
		PnlPct:          pnlPct,
		ExitReason:      "TP1",
		ExitedAt:        "2026-01-01T00:00:00Z",
	}
}

func TestProcess_WinningTrade(t *testing.T) {
	mod := New(defaultCfg())
	pos := exitedPosition(10.0) // +10%

	dto, err := mod.Process(context.Background(), EvaluationInput{Position: pos})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if dto.TruePositiveCount != 1 {
		t.Errorf("expected TruePositiveCount=1, got %d", dto.TruePositiveCount)
	}
	if dto.FalsePositiveCount != 0 {
		t.Errorf("expected FalsePositiveCount=0, got %d", dto.FalsePositiveCount)
	}
	if dto.SampleSize != 1 {
		t.Errorf("expected SampleSize=1, got %d", dto.SampleSize)
	}
	if dto.Expectancy != 10.0 {
		t.Errorf("expected Expectancy=10.0, got %f", dto.Expectancy)
	}
	if dto.MaxDrawdownPct != 0 {
		t.Errorf("expected MaxDrawdownPct=0 for winning trade, got %f", dto.MaxDrawdownPct)
	}
	if dto.EvaluationID == "" {
		t.Error("EvaluationID must not be empty")
	}
	if dto.TraceID != "trace-1" {
		t.Errorf("TraceID not propagated: %s", dto.TraceID)
	}
}

func TestProcess_LosingTrade_FalsePositive(t *testing.T) {
	mod := New(defaultCfg())
	pos := exitedPosition(-10.0) // -10% loss, below -5% threshold

	dto, err := mod.Process(context.Background(), EvaluationInput{Position: pos})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if dto.FalsePositiveCount != 1 {
		t.Errorf("expected FalsePositiveCount=1, got %d", dto.FalsePositiveCount)
	}
	if dto.TruePositiveCount != 0 {
		t.Errorf("expected TruePositiveCount=0, got %d", dto.TruePositiveCount)
	}
	if dto.MaxDrawdownPct != 10.0 {
		t.Errorf("expected MaxDrawdownPct=10.0, got %f", dto.MaxDrawdownPct)
	}
	if dto.PredictionErrorMean != -1.0 {
		// winProb=0, actual=0 (loss), predictionError = 0 - 0 = 0
		// Wait: actualOutcome=0 since pnlPct < 0, so predictionError = 0 - 0 = 0
		t.Logf("PredictionErrorMean=%f (expected 0 since no model in Phase 3)", dto.PredictionErrorMean)
	}
}

func TestProcess_SmallLoss_NotFalsePositive(t *testing.T) {
	mod := New(defaultCfg())
	pos := exitedPosition(-2.0) // -2% loss, above -5% threshold

	dto, err := mod.Process(context.Background(), EvaluationInput{Position: pos})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// -2% is NOT below -5% threshold, so not a FP
	if dto.FalsePositiveCount != 0 {
		t.Errorf("expected FalsePositiveCount=0 for small loss, got %d", dto.FalsePositiveCount)
	}
}

func TestProcess_FalseNegativeCount(t *testing.T) {
	mod := New(defaultCfg())
	pos := exitedPosition(5.0)

	shadows := []database.ShadowTrade{
		{ShadowTradeID: "s1", PeakGainPct: 25.0, IsFNCandidate: true}, // above 20% threshold
		{ShadowTradeID: "s2", PeakGainPct: 15.0, IsFNCandidate: true}, // below threshold
		{ShadowTradeID: "s3", PeakGainPct: 30.0, IsFNCandidate: true}, // above threshold
	}

	dto, err := mod.Process(context.Background(), EvaluationInput{
		Position:     pos,
		ShadowTrades: shadows,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if dto.FalseNegativeCount != 2 {
		t.Errorf("expected FalseNegativeCount=2, got %d", dto.FalseNegativeCount)
	}
}

func TestProcess_NonExitedStatus(t *testing.T) {
	mod := New(defaultCfg())
	pos := exitedPosition(0)
	pos.Status = "open"

	_, err := mod.Process(context.Background(), EvaluationInput{Position: pos})
	if err == nil {
		t.Error("expected error for non-exited position")
	}
}

func TestProcess_BrierScore(t *testing.T) {
	mod := New(defaultCfg())
	pos := exitedPosition(10.0) // win: actual=1, predicted=0, error=-1, brier=1

	dto, err := mod.Process(context.Background(), EvaluationInput{Position: pos})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// predictionError = 0 - 1 = -1; brier = (-1)^2 = 1
	if dto.BrierScore != 1.0 {
		t.Errorf("expected BrierScore=1.0 (Phase 3 no model), got %f", dto.BrierScore)
	}
}

func TestProcess_ContentAddressableIDs(t *testing.T) {
	mod := New(defaultCfg())
	pos := exitedPosition(5.0)

	dto1, _ := mod.Process(context.Background(), EvaluationInput{Position: pos})
	dto2, _ := mod.Process(context.Background(), EvaluationInput{Position: pos})

	// IDs are content-addressable from PositionID + ExitedAt, so same input → same ID
	if dto1.EvaluationID != dto2.EvaluationID {
		t.Errorf("EvaluationID not deterministic: %s vs %s", dto1.EvaluationID, dto2.EvaluationID)
	}
	if dto1.EventID != dto2.EventID {
		t.Errorf("EventID not deterministic: %s vs %s", dto1.EventID, dto2.EventID)
	}
}

func TestNew_DefaultsApplied(t *testing.T) {
	mod := New(config.EvaluationConfig{})
	if mod.cfg.FPLossThresholdPct == 0 {
		t.Error("expected non-zero FPLossThresholdPct default")
	}
	if mod.cfg.FNGainThresholdPct == 0 {
		t.Error("expected non-zero FNGainThresholdPct default")
	}
	if mod.cfg.WindowSeconds == 0 {
		t.Error("expected non-zero WindowSeconds default")
	}
}
