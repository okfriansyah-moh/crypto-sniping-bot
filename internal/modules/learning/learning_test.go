package learning_test

import (
	"context"
	"errors"
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

// ─── Classifier – additional branches ───────────────────────────────────────

func TestClassifier_TruePositive_TP2(t *testing.T) {
	var c learning.Classifier
	if got := c.Classify("TP2", 0.10); got != "TP" {
		t.Errorf("expected TP, got %q", got)
	}
}

func TestClassifier_TP1_ZeroPnl_IsFP(t *testing.T) {
	var c learning.Classifier
	// pnlPct == 0 is not > 0, so TP1 with zero return is FP.
	if got := c.Classify("TP1", 0.0); got != "FP" {
		t.Errorf("expected FP for TP1 with zero pnl, got %q", got)
	}
}

func TestClassifier_TIME_IsFP(t *testing.T) {
	var c learning.Classifier
	if got := c.Classify("TIME", -0.01); got != "FP" {
		t.Errorf("expected FP for TIME exit, got %q", got)
	}
}

func TestClassifier_MISSED_PUMP_IsFN(t *testing.T) {
	var c learning.Classifier
	if got := c.Classify("MISSED_PUMP", 0); got != "FN" {
		t.Errorf("expected FN, got %q", got)
	}
}

func TestClassifier_CORRECT_REJECT_IsTN(t *testing.T) {
	var c learning.Classifier
	if got := c.Classify("CORRECT_REJECT", 0); got != "TN" {
		t.Errorf("expected TN, got %q", got)
	}
}

func TestClassifier_Default_PositivePnl_IsTP(t *testing.T) {
	var c learning.Classifier
	if got := c.Classify("UNKNOWN_REASON", 0.05); got != "TP" {
		t.Errorf("expected TP for unknown reason with positive pnl, got %q", got)
	}
}

func TestClassifier_Default_NegativePnl_IsFP(t *testing.T) {
	var c learning.Classifier
	if got := c.Classify("UNKNOWN_REASON", -0.05); got != "FP" {
		t.Errorf("expected FP for unknown reason with negative pnl, got %q", got)
	}
}

// ─── OutcomeFromPosition ──────────────────────────────────────────────────────

func TestOutcomeFromPosition_TP1(t *testing.T) {
	if got := learning.OutcomeFromPosition("TP1", 0.20); got != "TP1" {
		t.Errorf("expected TP1, got %q", got)
	}
}

func TestOutcomeFromPosition_TP2(t *testing.T) {
	if got := learning.OutcomeFromPosition("TP2", 0.35); got != "TP2" {
		t.Errorf("expected TP2, got %q", got)
	}
}

func TestOutcomeFromPosition_SL(t *testing.T) {
	if got := learning.OutcomeFromPosition("SL", -0.05); got != "SL" {
		t.Errorf("expected SL, got %q", got)
	}
}

func TestOutcomeFromPosition_TIME(t *testing.T) {
	if got := learning.OutcomeFromPosition("TIME", -0.01); got != "TIME" {
		t.Errorf("expected TIME, got %q", got)
	}
}

func TestOutcomeFromPosition_RUG(t *testing.T) {
	if got := learning.OutcomeFromPosition("RUG", -0.90); got != "RUG" {
		t.Errorf("expected RUG, got %q", got)
	}
}

func TestOutcomeFromPosition_Default_PositivePnl(t *testing.T) {
	if got := learning.OutcomeFromPosition("", 0.10); got != "TP1" {
		t.Errorf("expected TP1 for default with positive pnl, got %q", got)
	}
}

func TestOutcomeFromPosition_Default_NegativePnl(t *testing.T) {
	if got := learning.OutcomeFromPosition("", -0.05); got != "SL" {
		t.Errorf("expected SL for default with negative pnl, got %q", got)
	}
}

// ─── Recorder – error path ───────────────────────────────────────────────────

func TestRecorder_RecordExecuted_NotExited_ReturnsError(t *testing.T) {
	recorder := learning.NewRecorder()
	pos := positionDTO("open", "0xABC", "lc-1", 0.10, "")
	_, err := recorder.RecordExecuted(context.Background(), pos, "evt-1", "v-1", "active")
	if err == nil {
		t.Fatal("expected error for non-exited position")
	}
}

// ─── ShadowRecorder – error path ─────────────────────────────────────────────

func TestShadowRecorder_RecordRejection_EmptyStage_ReturnsError(t *testing.T) {
	recorder := learning.NewShadowRecorder()
	_, _, err := recorder.RecordRejection(context.Background(), "", "0xABC", "lc-1", "evt-1", "v-1", "active")
	if err == nil {
		t.Fatal("expected error for empty stage")
	}
}

// ─── Evaluator – additional paths ────────────────────────────────────────────

func TestEvaluator_EvaluateWindow_EmptyVersionID_ReturnsError(t *testing.T) {
	evaluator := learning.NewEvaluator()
	now := time.Now().UTC()
	_, err := evaluator.EvaluateWindow(context.Background(), "", now.Add(-time.Hour), now, nil)
	if err == nil {
		t.Fatal("expected error for empty versionID")
	}
}

func TestEvaluator_EvaluateWindow_WithPredictionErrors(t *testing.T) {
	evaluator := learning.NewEvaluator()
	now := time.Now().UTC()
	records := []contracts.LearningRecordDTO{
		{Classification: "TP", PnlPct: 0.20, PredictionError: 0.05},
		{Classification: "FP", PnlPct: -0.10, PredictionError: -0.03},
	}
	eval, err := evaluator.EvaluateWindow(context.Background(), "v-1", now.Add(-time.Hour), now, records)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eval.BrierScore <= 0 {
		t.Errorf("expected positive BrierScore when prediction errors present, got %.4f", eval.BrierScore)
	}
}

func TestEvaluator_EvaluateWindow_MaxDrawdown(t *testing.T) {
	evaluator := learning.NewEvaluator()
	now := time.Now().UTC()
	records := []contracts.LearningRecordDTO{
		{Classification: "FP", PnlPct: -0.15},
		{Classification: "FP", PnlPct: -0.05},
	}
	eval, err := evaluator.EvaluateWindow(context.Background(), "v-1", now.Add(-time.Hour), now, records)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Max drawdown should be the worst single trade loss.
	if eval.MaxDrawdownPct != 0.15 {
		t.Errorf("expected MaxDrawdownPct=0.15, got %.4f", eval.MaxDrawdownPct)
	}
}

// ─── Updater – additional paths ──────────────────────────────────────────────

func TestUpdater_Propose_NoFamilies_ReturnsError(t *testing.T) {
	cfg := &config.LearningConfig{
		MinSampleSize: 3,
		MaxDeltaPct:   0.10,
		Families:      []string{},
	}
	updater := learning.NewUpdater(cfg)
	eval := contracts.EvaluationDTO{SampleSize: 10, Expectancy: 0.10}
	_, _, err := updater.Propose(context.Background(), []byte(`{}`), eval)
	if err == nil {
		t.Fatal("expected error when no families configured")
	}
}

func TestUpdater_Propose_RoundRobin_AdvancesFamily(t *testing.T) {
	cfg := &config.LearningConfig{
		MinSampleSize: 3,
		MaxDeltaPct:   0.10,
		Families:      []string{"thresholds", "weights"},
	}
	updater := learning.NewUpdater(cfg)
	eval := contracts.EvaluationDTO{SampleSize: 10, Expectancy: 0.10}
	snapshot := []byte(`{}`)

	_, family1, err := updater.Propose(context.Background(), snapshot, eval)
	if err != nil {
		t.Fatalf("first Propose failed: %v", err)
	}
	_, family2, err := updater.Propose(context.Background(), snapshot, eval)
	if err != nil {
		t.Fatalf("second Propose failed: %v", err)
	}
	if family1 == family2 {
		t.Errorf("expected different families on consecutive Propose calls; both=%q", family1)
	}
}

func TestUpdater_Propose_NegativeExpectancy_TightensParams(t *testing.T) {
	cfg := &config.LearningConfig{
		MinSampleSize: 3,
		MaxDeltaPct:   0.10,
		Families:      []string{"thresholds"},
	}
	updater := learning.NewUpdater(cfg)
	// Snapshot with actual threshold keys so applyBoundedDelta modifies them.
	snapshot := []byte(`{"edge.min_velocity_score":0.5,"edge.min_liquidity_score":0.4,"validation.ev_threshold_bps":100}`)
	eval := contracts.EvaluationDTO{SampleSize: 10, Expectancy: -0.05}
	params, family, err := updater.Propose(context.Background(), snapshot, eval)
	if err != nil {
		t.Fatalf("Propose failed: %v", err)
	}
	if family != "thresholds" {
		t.Errorf("expected family=thresholds, got %q", family)
	}
	// Direction is negative (expectancy < 0), so thresholds should decrease.
	if params["edge.min_velocity_score"] >= 0.5 {
		t.Errorf("expected threshold to tighten (decrease), got %.4f", params["edge.min_velocity_score"])
	}
}

func TestUpdater_Propose_WeightsFamily(t *testing.T) {
	cfg := &config.LearningConfig{
		MinSampleSize: 3,
		MaxDeltaPct:   0.10,
		Families:      []string{"weights"},
	}
	updater := learning.NewUpdater(cfg)
	snapshot := []byte(`{"features.momentum_weight":0.4,"features.liquidity_weight":0.3,"features.velocity_weight":0.3}`)
	eval := contracts.EvaluationDTO{SampleSize: 10, Expectancy: 0.10}
	params, family, err := updater.Propose(context.Background(), snapshot, eval)
	if err != nil {
		t.Fatalf("Propose failed: %v", err)
	}
	if family != "weights" {
		t.Errorf("expected family=weights, got %q", family)
	}
	// Positive expectancy → weights should relax upward.
	if params["features.momentum_weight"] <= 0.4 {
		t.Errorf("expected weight to increase, got %.4f", params["features.momentum_weight"])
	}
}

func TestUpdater_Propose_CohortMulstFamily(t *testing.T) {
	cfg := &config.LearningConfig{
		MinSampleSize: 3,
		MaxDeltaPct:   0.10,
		Families:      []string{"cohort_mults"},
	}
	updater := learning.NewUpdater(cfg)
	snapshot := []byte(`{"cohort.high.multiplier":1.5,"cohort.mid.multiplier":1.0,"cohort.low.multiplier":0.5}`)
	eval := contracts.EvaluationDTO{SampleSize: 10, Expectancy: 0.10}
	params, family, err := updater.Propose(context.Background(), snapshot, eval)
	if err != nil {
		t.Fatalf("Propose failed: %v", err)
	}
	if family != "cohort_mults" {
		t.Errorf("expected family=cohort_mults, got %q", family)
	}
	// Should have cohort keys updated.
	if params["cohort.high.multiplier"] <= 1.5 {
		t.Errorf("expected cohort.high.multiplier to increase, got %.4f", params["cohort.high.multiplier"])
	}
}

func TestUpdater_Propose_UnknownFamily_NoKeyModification(t *testing.T) {
	cfg := &config.LearningConfig{
		MinSampleSize: 3,
		MaxDeltaPct:   0.10,
		Families:      []string{"nonexistent_family"},
	}
	updater := learning.NewUpdater(cfg)
	snapshot := []byte(`{"some.key":0.5}`)
	eval := contracts.EvaluationDTO{SampleSize: 10, Expectancy: 0.10}
	params, family, err := updater.Propose(context.Background(), snapshot, eval)
	if err != nil {
		t.Fatalf("Propose failed: %v", err)
	}
	if family != "nonexistent_family" {
		t.Errorf("expected family=nonexistent_family, got %q", family)
	}
	// Unknown family → no keys modified.
	if params["some.key"] != 0.5 {
		t.Errorf("expected key unchanged for unknown family, got %.4f", params["some.key"])
	}
}

func TestNewVersionID_Deterministic(t *testing.T) {
	params := map[string]float64{"threshold": 0.3, "weight": 0.5}
	id1, err1 := learning.NewVersionID(params)
	id2, err2 := learning.NewVersionID(params)
	if err1 != nil || err2 != nil {
		t.Fatalf("NewVersionID error: %v / %v", err1, err2)
	}
	if id1 != id2 {
		t.Errorf("expected deterministic ID, got %q and %q", id1, id2)
	}
}

func TestNewVersionID_DistinctForDifferentParams(t *testing.T) {
	p1 := map[string]float64{"a": 1.0}
	p2 := map[string]float64{"a": 2.0}
	id1, _ := learning.NewVersionID(p1)
	id2, _ := learning.NewVersionID(p2)
	if id1 == id2 {
		t.Error("expected different IDs for different params")
	}
}

func TestBuildStrategyVersion_ValidParams(t *testing.T) {
	params := map[string]float64{"threshold": 0.3}
	vp, err := learning.BuildStrategyVersion(params, "parent-v1", "thresholds", 10, "trace-1")
	if err != nil {
		t.Fatalf("BuildStrategyVersion failed: %v", err)
	}
	if vp.Status != "draft" {
		t.Errorf("expected status=draft, got %q", vp.Status)
	}
	if vp.ParentVersionID != "parent-v1" {
		t.Errorf("expected ParentVersionID=parent-v1, got %q", vp.ParentVersionID)
	}
	if len(vp.ConfigSnapshot) == 0 {
		t.Error("expected non-empty ConfigSnapshot")
	}
}

// ─── ABPromoter – expectancy not improved ────────────────────────────────────

func TestABPromoter_ShouldPromote_ExpectancyNotImproved(t *testing.T) {
	promoter := learning.NewABPromoter(defaultLearningCfg())
	// Candidate expectancy must be > baseline × 1.05.
	// 0.105 is exactly baseline×1.05 — should NOT promote (not strictly greater).
	candidate := contracts.EvaluationDTO{SampleSize: 10, Expectancy: 0.105, MaxDrawdownPct: 0.05}
	baseline := contracts.EvaluationDTO{SampleSize: 10, Expectancy: 0.100, MaxDrawdownPct: 0.08}
	ok, _, err := promoter.ShouldPromote(context.Background(), candidate, baseline)
	if err != nil {
		t.Fatalf("ShouldPromote error: %v", err)
	}
	if ok {
		t.Error("expected no promotion when expectancy barely meets (not exceeds) 1.05× threshold")
	}
}

// ─── ShouldRollback – baseline zero ──────────────────────────────────────────

func TestShouldRollback_BaselineZero_NoRollback(t *testing.T) {
	promoted := contracts.EvaluationDTO{Expectancy: 0.05}
	baseline := contracts.EvaluationDTO{Expectancy: 0.0}
	if learning.ShouldRollback(promoted, baseline, 0.10) {
		t.Error("expected no rollback when baseline expectancy is zero")
	}
}

// ─── CohortLabel – bucket coverage ──────────────────────────────────────────

func TestCohortLabel_MidLiquidity(t *testing.T) {
	// liquidityScore=0.5 is in the mid bucket (0.35–0.7).
	label := learning.CohortLabel(0.5, 0, "src")
	if label == "" {
		t.Fatal("expected non-empty label")
	}
	// First segment should be "mid".
	parts := splitLabel(label)
	if parts[0] != "mid" {
		t.Errorf("expected mid liquidity bucket, got %q (full label: %q)", parts[0], label)
	}
}

func TestCohortLabel_YoungAge(t *testing.T) {
	// tokenAgeSeconds=600 is in the young bucket (300–3600).
	label := learning.CohortLabel(0.0, 600, "src")
	parts := splitLabel(label)
	if parts[1] != "young" {
		t.Errorf("expected young age bucket, got %q (full label: %q)", parts[1], label)
	}
}

func TestCohortLabel_HighLiquidity(t *testing.T) {
	label := learning.CohortLabel(0.8, 400, "src")
	parts := splitLabel(label)
	if parts[0] != "high" {
		t.Errorf("expected high liquidity bucket, got %q", parts[0])
	}
}

func TestCohortLabel_MatureAge(t *testing.T) {
	label := learning.CohortLabel(0.0, 7200, "src")
	parts := splitLabel(label)
	if parts[1] != "mature" {
		t.Errorf("expected mature age bucket, got %q", parts[1])
	}
}

// splitLabel splits a colon-delimited cohort label into parts.
func splitLabel(label string) []string {
	var parts []string
	start := 0
	for i, c := range label {
		if c == ':' {
			parts = append(parts, label[start:i])
			start = i + 1
		}
	}
	parts = append(parts, label[start:])
	return parts
}

// ─── OpportunityMonitor – additional paths ───────────────────────────────────

func TestOpportunityMonitor_NegativeWindow_ReturnsError(t *testing.T) {
	monitor := learning.NewOpportunityMonitor(defaultLearningCfg())
	_, _, err := monitor.Check(context.Background(), nil, -time.Hour)
	if err == nil {
		t.Fatal("expected error for non-positive window")
	}
}

func TestOpportunityMonitor_HighFNRate_LowWins(t *testing.T) {
	monitor := learning.NewOpportunityMonitor(defaultLearningCfg())
	// FN rate > 0.5 and tp < 3 → EXPLORATION
	records := []contracts.LearningRecordDTO{
		{Classification: "FN"},
		{Classification: "FN"},
		{Classification: "FN"},
		{Classification: "TP"},
		{Classification: "TP"},
	}
	mode, _, err := monitor.Check(context.Background(), records, time.Hour)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if mode != "EXPLORATION" {
		t.Errorf("expected EXPLORATION for high FN rate with few wins, got %q", mode)
	}
}

func TestOpportunityMonitor_StarvationFewExecuted(t *testing.T) {
	monitor := learning.NewOpportunityMonitor(defaultLearningCfg())
	// Only 1 TP and 1 FP (=2 executed) → starvation
	records := []contracts.LearningRecordDTO{
		{Classification: "TP"},
		{Classification: "FP"},
		{Classification: "TN"},
	}
	mode, _, err := monitor.Check(context.Background(), records, time.Hour)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if mode != "EXPLORATION" {
		t.Errorf("expected EXPLORATION for starvation (few executed), got %q", mode)
	}
}

// ─── ShadowObserver ──────────────────────────────────────────────────────────

type stubPriceClient struct {
	price string
	err   error
}

func (s *stubPriceClient) GetTokenPrice(_ context.Context, _, _ string) (string, error) {
	return s.price, s.err
}

func TestShadowObserver_NilPriceClient_ReturnsIncomplete(t *testing.T) {
	observer := learning.NewShadowObserver(nil, "eth")
	ret, complete, err := observer.Observe(context.Background(), "0xABC", "1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if complete {
		t.Error("expected incomplete when priceClient is nil")
	}
	if ret != 0 {
		t.Errorf("expected 0 return, got %.4f", ret)
	}
}

func TestShadowObserver_PriceClientError_ReturnsError(t *testing.T) {
	client := &stubPriceClient{err: errors.New("rpc timeout")}
	observer := learning.NewShadowObserver(client, "eth")
	_, _, err := observer.Observe(context.Background(), "0xABC", "1.0")
	if err == nil {
		t.Fatal("expected error when price client returns error")
	}
}

func TestShadowObserver_EmptyCurrentPrice_ReturnsIncomplete(t *testing.T) {
	client := &stubPriceClient{price: ""}
	observer := learning.NewShadowObserver(client, "eth")
	_, complete, err := observer.Observe(context.Background(), "0xABC", "1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if complete {
		t.Error("expected incomplete when current price is empty")
	}
}

func TestShadowObserver_InvalidCurrentPrice_ReturnsIncomplete(t *testing.T) {
	client := &stubPriceClient{price: "not-a-number"}
	observer := learning.NewShadowObserver(client, "eth")
	_, complete, err := observer.Observe(context.Background(), "0xABC", "1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if complete {
		t.Error("expected incomplete when current price is invalid")
	}
}

func TestShadowObserver_ZeroRejectionPrice_CompletesWithZeroReturn(t *testing.T) {
	client := &stubPriceClient{price: "2.0"}
	observer := learning.NewShadowObserver(client, "eth")
	// rejection price "0" → safe fallback → complete with 0 return
	ret, complete, err := observer.Observe(context.Background(), "0xABC", "0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !complete {
		t.Error("expected complete when rejection price is zero (safe fallback)")
	}
	if ret != 0 {
		t.Errorf("expected 0 return for zero rejection price, got %.4f", ret)
	}
}

func TestShadowObserver_InvalidRejectionPrice_CompletesWithZeroReturn(t *testing.T) {
	client := &stubPriceClient{price: "2.0"}
	observer := learning.NewShadowObserver(client, "eth")
	ret, complete, err := observer.Observe(context.Background(), "0xABC", "not-a-number")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !complete {
		t.Error("expected complete when rejection price is invalid (safe fallback)")
	}
	if ret != 0 {
		t.Errorf("expected 0 return for invalid rejection price, got %.4f", ret)
	}
}

func TestShadowObserver_ValidPrices_ComputesReturn(t *testing.T) {
	client := &stubPriceClient{price: "1.5"}
	observer := learning.NewShadowObserver(client, "eth")
	// return = (1.5 - 1.0) / 1.0 = 0.5
	ret, complete, err := observer.Observe(context.Background(), "0xABC", "1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !complete {
		t.Error("expected complete for valid prices")
	}
	if ret != 0.5 {
		t.Errorf("expected return=0.5, got %.4f", ret)
	}
}

func TestShadowObserver_NegativeReturn(t *testing.T) {
	client := &stubPriceClient{price: "0.8"}
	observer := learning.NewShadowObserver(client, "eth")
	// return = (0.8 - 1.0) / 1.0 ≈ -0.2
	ret, complete, err := observer.Observe(context.Background(), "0xABC", "1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !complete {
		t.Error("expected complete")
	}
	const want = -0.2
	const epsilon = 1e-9
	if ret < want-epsilon || ret > want+epsilon {
		t.Errorf("expected return≈-0.2, got %.10f", ret)
	}
}

// ─── ProposeVersion error path from Propose ───────────────────────────────────

func TestUpdater_ProposeVersion_ProposeError_ReturnsError(t *testing.T) {
	// Families is empty → Propose returns error → ProposeVersion propagates it.
	cfg := &config.LearningConfig{
		MinSampleSize: 3,
		MaxDeltaPct:   0.10,
		Families:      []string{},
	}
	updater := learning.NewUpdater(cfg)
	eval := contracts.EvaluationDTO{SampleSize: 10, Expectancy: 0.10}
	_, err := updater.ProposeVersion(context.Background(), []byte(`{}`), "v-1", eval, "trace-1")
	if err == nil {
		t.Fatal("expected error propagated from Propose when no families configured")
	}
}

func TestUpdater_ProposeVersion_ZeroMinSample_DefaultsTo30(t *testing.T) {
	// MinSampleSize=0 should default to 30 inside ProposeVersion.
	cfg := &config.LearningConfig{
		MinSampleSize: 0, // triggers default path (→ 30)
		MaxDeltaPct:   0.10,
		Families:      []string{"thresholds"},
	}
	updater := learning.NewUpdater(cfg)
	eval := contracts.EvaluationDTO{SampleSize: 5, Expectancy: 0.10}
	_, err := updater.ProposeVersion(context.Background(), []byte(`{}`), "v-1", eval, "trace-1")
	// 5 < 30 (default min sample) → error expected
	if err == nil {
		t.Fatal("expected error: 5 samples < default minimum of 30")
	}
}

func TestUpdater_Propose_InvalidJSON_FallsBackToEmpty(t *testing.T) {
	cfg := &config.LearningConfig{
		MinSampleSize: 3,
		MaxDeltaPct:   0.10,
		Families:      []string{"thresholds"},
	}
	updater := learning.NewUpdater(cfg)
	// Invalid JSON triggers the fallback to empty paramMap.
	snapshot := []byte(`not valid json`)
	eval := contracts.EvaluationDTO{SampleSize: 10, Expectancy: 0.10}
	params, family, err := updater.Propose(context.Background(), snapshot, eval)
	if err != nil {
		t.Fatalf("Propose with invalid JSON snapshot failed: %v", err)
	}
	if family != "thresholds" {
		t.Errorf("expected family=thresholds, got %q", family)
	}
	// With empty fallback map, no threshold keys are present → no modifications.
	if len(params) != 0 {
		t.Errorf("expected empty params map for invalid JSON fallback, got %v", params)
	}
}

func TestABPromoter_ShouldPromote_ZeroMinSample_DefaultsTo30(t *testing.T) {
	cfg := &config.LearningConfig{
		MinSampleSize: 0, // triggers default path → 30
	}
	promoter := learning.NewABPromoter(cfg)
	candidate := contracts.EvaluationDTO{SampleSize: 5, Expectancy: 0.20, MaxDrawdownPct: 0.05}
	baseline := contracts.EvaluationDTO{SampleSize: 30, Expectancy: 0.10, MaxDrawdownPct: 0.08}
	ok, reason, err := promoter.ShouldPromote(context.Background(), candidate, baseline)
	if err != nil {
		t.Fatalf("ShouldPromote error: %v", err)
	}
	if ok {
		t.Errorf("expected promotion blocked (5 < 30 default), got reason: %q", reason)
	}
}

// ─── sanitizeSource with special characters ───────────────────────────────────

func TestCohortLabel_SpecialCharSource(t *testing.T) {
	// Source with spaces and dots should have non-alphanumeric chars replaced by '_'.
	label := learning.CohortLabel(0.0, 0, "my source.v1")
	parts := splitLabel(label)
	// Source segment should not contain spaces or dots.
	for _, ch := range parts[2] {
		if ch != '_' && !(ch >= 'a' && ch <= 'z') && !(ch >= 'A' && ch <= 'Z') && !(ch >= '0' && ch <= '9') && ch != '-' {
			t.Errorf("sanitizeSource left invalid char %q in label segment %q", ch, parts[2])
		}
	}
}

// ─── test helpers ────────────────────────────────────────────────────────────
