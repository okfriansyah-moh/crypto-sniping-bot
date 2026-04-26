package learning_test

import (
	"context"
	"testing"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/learning"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func defaultLearningCfg() *config.LearningConfig {
	return &config.LearningConfig{
		EvalWindowMinutes:        60,
		ObservationWindowSeconds: 3600,
		MinSampleSize:            3, // low for tests
		MaxDeltaPct:              0.10,
		Families:                 []string{"thresholds", "weights"},
		FnGainThresholdPct:       0.10,
		RollbackThresholdPct:     0.10,
	}
}

func positionDTO(status, tokenAddr, lifecycleID string, pnlPct float64, outcome string) contracts.PositionStateDTO {
	return contracts.PositionStateDTO{
		TokenAddress:     tokenAddr,
		TokenLifecycleID: lifecycleID,
		Status:           status,
		PnlPct:           pnlPct,
		ExitReason:       outcome,
	}
}

// ─── CohortLabel ─────────────────────────────────────────────────────────────

func TestCohortLabel_Valid(t *testing.T) {
	liq := 50_000.0
	age := int64(30)
	source := "dexscreener"
	label := learning.CohortLabel(liq, age, source)
	if label == "" {
		t.Fatal("expected non-empty cohort label")
	}
	// Must contain colon separators (3 parts).
	count := 0
	for _, c := range label {
		if c == ':' {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 colons in cohort label, got %d: %q", count, label)
	}
}

func TestCohortLabel_ZeroValues(t *testing.T) {
	label := learning.CohortLabel(0, 0, "")
	if label == "" {
		t.Fatal("expected non-empty cohort label for zero values")
	}
}

// ─── FP/FN Classifier ────────────────────────────────────────────────────────

func TestClassifier_TruePositive(t *testing.T) {
	var c learning.Classifier
	class := c.Classify("TP1", 0.20)
	if class != "TP" {
		t.Errorf("expected TP, got %q", class)
	}
}

func TestClassifier_FalsePositive_SL(t *testing.T) {
	var c learning.Classifier
	class := c.Classify("SL", -0.08)
	if class != "FP" {
		t.Errorf("expected FP, got %q", class)
	}
}

func TestClassifier_FalsePositive_Rug(t *testing.T) {
	var c learning.Classifier
	class := c.Classify("RUG", -0.50)
	if class != "FP" {
		t.Errorf("expected FP for rug, got %q", class)
	}
}

func TestClassifyShadow_FalseNegative(t *testing.T) {
	// 15% gain exceeds 10% threshold → FN (we should have traded)
	class := learning.ClassifyShadow(0.15, 0.10)
	if class != "FN" {
		t.Errorf("expected FN, got %q", class)
	}
}

func TestClassifyShadow_TrueNegative(t *testing.T) {
	// 5% gain < 10% threshold → TN (correct to skip)
	class := learning.ClassifyShadow(0.05, 0.10)
	if class != "TN" {
		t.Errorf("expected TN, got %q", class)
	}
}

func TestClassifyShadow_NegativeReturn(t *testing.T) {
	// Would have lost money → TN
	class := learning.ClassifyShadow(-0.10, 0.10)
	if class != "TN" {
		t.Errorf("expected TN for negative return, got %q", class)
	}
}

// ─── Recorder ────────────────────────────────────────────────────────────────

func TestRecorder_RecordExecuted(t *testing.T) {
	recorder := learning.NewRecorder()
	pos := positionDTO("exited", "0xDEF", "lc-exec-1", 0.15, "TP1")
	lr, err := recorder.RecordExecuted(context.Background(), pos, "evt-1", "v-1", "active")
	if err != nil {
		t.Fatalf("RecordExecuted failed: %v", err)
	}
	if lr.RecordID == "" {
		t.Error("expected non-empty RecordID")
	}
	if lr.Shadow {
		t.Error("executed trade should not be shadow")
	}
	if lr.Classification != "TP" {
		t.Errorf("expected TP, got %q", lr.Classification)
	}
}

func TestRecorder_RecordExecuted_Loss(t *testing.T) {
	recorder := learning.NewRecorder()
	pos := positionDTO("exited", "0xGHI", "lc-exec-2", -0.10, "SL")
	lr, err := recorder.RecordExecuted(context.Background(), pos, "evt-2", "v-1", "active")
	if err != nil {
		t.Fatalf("RecordExecuted failed: %v", err)
	}
	if lr.Classification != "FP" {
		t.Errorf("expected FP for SL trade, got %q", lr.Classification)
	}
}

// ─── ShadowRecorder ──────────────────────────────────────────────────────────

func TestShadowRecorder_RecordRejection(t *testing.T) {
	recorder := learning.NewShadowRecorder()
	lr, sp, err := recorder.RecordRejection(context.Background(),
		"data_quality", "0xJKL", "lc-shadow-1", "evt-3", "v-1", "active")
	if err != nil {
		t.Fatalf("RecordRejection failed: %v", err)
	}
	if !lr.Shadow {
		t.Error("rejected trade should be shadow")
	}
	if sp.ShadowID == "" {
		t.Error("expected non-empty ShadowID")
	}
	if sp.ObservationComplete {
		t.Error("observation should not be complete at rejection time")
	}
}

// ─── Evaluator ───────────────────────────────────────────────────────────────

func TestEvaluator_EvaluateWindow_Empty(t *testing.T) {
	evaluator := learning.NewEvaluator()
	now := time.Now().UTC()
	start := now.Add(-time.Hour)

	eval, err := evaluator.EvaluateWindow(context.Background(), "v-1", start, now, nil)
	if err != nil {
		t.Fatalf("EvaluateWindow failed: %v", err)
	}
	if eval.SampleSize != 0 {
		t.Errorf("expected 0 samples, got %d", eval.SampleSize)
	}
}

func TestEvaluator_EvaluateWindow_Mixed(t *testing.T) {
	evaluator := learning.NewEvaluator()
	now := time.Now().UTC()
	start := now.Add(-time.Hour)

	records := []contracts.LearningRecordDTO{
		{Classification: "TP", PnlPct: 0.20},
		{Classification: "TP", PnlPct: 0.15},
		{Classification: "FP", PnlPct: -0.08},
		{Classification: "TN"},
		{Classification: "FN"},
	}

	eval, err := evaluator.EvaluateWindow(context.Background(), "v-1", start, now, records)
	if err != nil {
		t.Fatalf("EvaluateWindow failed: %v", err)
	}
	if eval.SampleSize != 5 {
		t.Errorf("expected 5 samples, got %d", eval.SampleSize)
	}
	if eval.TruePositiveCount != 2 {
		t.Errorf("expected 2 TPs, got %d", eval.TruePositiveCount)
	}
	if eval.FalsePositiveCount != 1 {
		t.Errorf("expected 1 FP, got %d", eval.FalsePositiveCount)
	}
	if eval.Expectancy <= 0 {
		t.Errorf("expected positive expectancy, got %.4f", eval.Expectancy)
	}
}

// ─── Updater / ProposeVersion ────────────────────────────────────────────────

func TestUpdater_ProposeVersion_InsufficientSamples(t *testing.T) {
	cfg := defaultLearningCfg()
	cfg.MinSampleSize = 30 // require 30 samples
	updater := learning.NewUpdater(cfg)

	eval := contracts.EvaluationDTO{SampleSize: 5, Expectancy: 0.10, VersionID: "v-1"}
	_, err := updater.ProposeVersion(context.Background(),
		[]byte(`{"threshold":0.3}`), "v-1", eval, "trace-1")
	if err == nil {
		t.Fatal("expected error for insufficient samples")
	}
}

func TestUpdater_ProposeVersion_Success(t *testing.T) {
	cfg := defaultLearningCfg()
	updater := learning.NewUpdater(cfg)

	eval := contracts.EvaluationDTO{SampleSize: 10, Expectancy: 0.12, VersionID: "v-1"}
	sv, err := updater.ProposeVersion(context.Background(),
		[]byte(`{"threshold":0.3,"weight":0.5}`), "v-1", eval, "trace-1")
	if err != nil {
		t.Fatalf("ProposeVersion failed: %v", err)
	}
	if sv.StrategyVersionID == "" {
		t.Error("expected non-empty StrategyVersionID")
	}
	if sv.ParentVersionID != "v-1" {
		t.Errorf("expected ParentVersionID=v-1, got %q", sv.ParentVersionID)
	}
	if sv.Status != "draft" {
		t.Errorf("expected status=draft, got %q", sv.Status)
	}
}

// ─── ABPromoter ──────────────────────────────────────────────────────────────

func TestABPromoter_ShouldPromote_Success(t *testing.T) {
	promoter := learning.NewABPromoter(defaultLearningCfg())

	candidate := contracts.EvaluationDTO{
		SampleSize:     10,
		Expectancy:     0.130,
		MaxDrawdownPct: 0.05,
	}
	baseline := contracts.EvaluationDTO{
		SampleSize:     10,
		Expectancy:     0.100,
		MaxDrawdownPct: 0.08,
	}
	ok, reason, err := promoter.ShouldPromote(context.Background(), candidate, baseline)
	if err != nil {
		t.Fatalf("ShouldPromote error: %v", err)
	}
	if !ok {
		t.Errorf("expected promotion, got reason: %q", reason)
	}
}

func TestABPromoter_ShouldPromote_InsufficientSamples(t *testing.T) {
	cfg := defaultLearningCfg()
	cfg.MinSampleSize = 30
	promoter := learning.NewABPromoter(cfg)

	candidate := contracts.EvaluationDTO{SampleSize: 5, Expectancy: 0.20}
	baseline := contracts.EvaluationDTO{SampleSize: 30, Expectancy: 0.10}
	ok, _, err := promoter.ShouldPromote(context.Background(), candidate, baseline)
	if err != nil {
		t.Fatalf("ShouldPromote error: %v", err)
	}
	if ok {
		t.Error("expected promotion blocked due to insufficient samples")
	}
}

func TestABPromoter_ShouldPromote_DrawdownWorse(t *testing.T) {
	promoter := learning.NewABPromoter(defaultLearningCfg())
	candidate := contracts.EvaluationDTO{SampleSize: 10, Expectancy: 0.13, MaxDrawdownPct: 0.20}
	baseline := contracts.EvaluationDTO{SampleSize: 10, Expectancy: 0.10, MaxDrawdownPct: 0.08}
	ok, _, err := promoter.ShouldPromote(context.Background(), candidate, baseline)
	if err != nil {
		t.Fatalf("ShouldPromote error: %v", err)
	}
	if ok {
		t.Error("expected promotion blocked due to worse drawdown")
	}
}

// ─── ShouldRollback ──────────────────────────────────────────────────────────

func TestShouldRollback_Triggers(t *testing.T) {
	promoted := contracts.EvaluationDTO{Expectancy: 0.05}
	baseline := contracts.EvaluationDTO{Expectancy: 0.10}
	// degradation = (0.10-0.05)/0.10 = 0.50 > 0.10 threshold
	if !learning.ShouldRollback(promoted, baseline, 0.10) {
		t.Error("expected rollback triggered")
	}
}

func TestShouldRollback_NoTrigger(t *testing.T) {
	promoted := contracts.EvaluationDTO{Expectancy: 0.095}
	baseline := contracts.EvaluationDTO{Expectancy: 0.10}
	// degradation = (0.10-0.095)/0.10 = 0.05 < 0.10 threshold
	if learning.ShouldRollback(promoted, baseline, 0.10) {
		t.Error("expected no rollback for minor degradation")
	}
}

// ─── OpportunityMonitor ──────────────────────────────────────────────────────

func TestOpportunityMonitor_Starvation(t *testing.T) {
	monitor := learning.NewOpportunityMonitor(defaultLearningCfg())
	mode, reason, err := monitor.Check(context.Background(), nil, time.Hour)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if mode != "EXPLORATION" {
		t.Errorf("expected EXPLORATION for starvation, got %q: %q", mode, reason)
	}
}

func TestOpportunityMonitor_HighFPRate(t *testing.T) {
	monitor := learning.NewOpportunityMonitor(defaultLearningCfg())
	records := []contracts.LearningRecordDTO{
		{Classification: "FP"},
		{Classification: "FP"},
		{Classification: "FP"},
		{Classification: "FP"},
		{Classification: "TP"},
	}
	mode, _, err := monitor.Check(context.Background(), records, time.Hour)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if mode != "STRICT" {
		t.Errorf("expected STRICT for high FP rate, got %q", mode)
	}
}

func TestOpportunityMonitor_Healthy(t *testing.T) {
	monitor := learning.NewOpportunityMonitor(defaultLearningCfg())
	records := []contracts.LearningRecordDTO{
		{Classification: "TP"},
		{Classification: "TP"},
		{Classification: "TP"},
		{Classification: "FP"},
	}
	mode, _, err := monitor.Check(context.Background(), records, time.Hour)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if mode != "BALANCED" {
		t.Errorf("expected BALANCED for healthy mix, got %q", mode)
	}
}

// ─── test helpers ────────────────────────────────────────────────────────────
