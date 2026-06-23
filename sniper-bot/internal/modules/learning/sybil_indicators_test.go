package learning_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/sniper-bot/internal/modules/learning"
)

const (
	testSybilMinWallets   = 50
	testSybilMaxWashScore = 0.30
)

func sybilMarketWith(unique int32, entropy float64, known bool) contracts.MarketDataDTO {
	return contracts.MarketDataDTO{
		WashStatsKnown:  known,
		UniqueWallets1m: unique,
		WalletEntropy:   entropy,
	}
}

func sybilDQWithWash(washScore float64) contracts.DataQualityDTO {
	return contracts.DataQualityDTO{WashScore: washScore}
}

func sybilLossRecord() *contracts.LearningRecordDTO {
	return &contracts.LearningRecordDTO{
		RecordID:         "rec-123",
		TokenLifecycleID: "tok-abc",
		TraceID:          "trace-xyz",
		Shadow:           false,
		PnlUsd:           -42.0,
		PnlPct:           -0.18,
		Outcome:          "SL",
		Classification:   "FP",
	}
}

func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestLearningClassifier_LossWithLowWash_PopulatesSybilIndicators(t *testing.T) {
	rec := sybilLossRecord()
	market := sybilMarketWith(60, 3.2, true)
	dq := sybilDQWithWash(0.10)
	var buf bytes.Buffer

	learning.ApplySybilIndicators(rec, market, dq, testSybilMinWallets, testSybilMaxWashScore, captureLogger(&buf))

	if rec.SybilClusterIndicators == nil {
		t.Fatal("expected SybilClusterIndicators to be populated, got nil")
	}
	if rec.SybilClusterIndicators.UniqueWallets1m != 60 {
		t.Errorf("UniqueWallets1m: expected 60, got %d", rec.SybilClusterIndicators.UniqueWallets1m)
	}
	if rec.SybilClusterIndicators.WalletEntropyNats != 3.2 {
		t.Errorf("WalletEntropyNats: expected 3.2, got %v", rec.SybilClusterIndicators.WalletEntropyNats)
	}
	if rec.SybilClusterIndicators.SuspectClusterSize != 0 {
		t.Errorf("SuspectClusterSize must be 0 (reserved), got %d", rec.SybilClusterIndicators.SuspectClusterSize)
	}
	if rec.SybilClusterIndicators.FundingSourceShared {
		t.Error("FundingSourceShared must be false (reserved)")
	}
	if !strings.Contains(buf.String(), "loss_bucket_sybil_suspect") {
		t.Errorf("expected loss_bucket_sybil_suspect log line, got: %s", buf.String())
	}
}

func TestLearningClassifier_Win_DoesNotPopulate(t *testing.T) {
	rec := sybilLossRecord()
	rec.PnlUsd = 17.0
	rec.PnlPct = 0.25
	rec.Outcome = "TP1"
	rec.Classification = "TP"

	learning.ApplySybilIndicators(rec, sybilMarketWith(60, 3.2, true), sybilDQWithWash(0.10),
		testSybilMinWallets, testSybilMaxWashScore, slog.Default())

	if rec.SybilClusterIndicators != nil {
		t.Errorf("wins must not populate SybilClusterIndicators, got %+v", rec.SybilClusterIndicators)
	}
}

func TestLearningClassifier_LossButHighWash_DoesNotPopulate(t *testing.T) {
	rec := sybilLossRecord()
	// wash already caught it — no need to flag a Sybil bypass.
	dq := sybilDQWithWash(0.55)

	learning.ApplySybilIndicators(rec, sybilMarketWith(60, 3.2, true), dq,
		testSybilMinWallets, testSybilMaxWashScore, slog.Default())

	if rec.SybilClusterIndicators != nil {
		t.Errorf("high wash score must not populate, got %+v", rec.SybilClusterIndicators)
	}
}

func TestLearningClassifier_LossLowWashFewWallets_DoesNotPopulate(t *testing.T) {
	rec := sybilLossRecord()
	market := sybilMarketWith(20, 1.5, true) // below threshold of 50

	learning.ApplySybilIndicators(rec, market, sybilDQWithWash(0.10),
		testSybilMinWallets, testSybilMaxWashScore, slog.Default())

	if rec.SybilClusterIndicators != nil {
		t.Errorf("few wallets must not populate, got %+v", rec.SybilClusterIndicators)
	}
}

func TestLearningClassifier_WashStatsUnknown_DoesNotPopulate(t *testing.T) {
	rec := sybilLossRecord()
	// Stats not measured upstream — cannot prove dispersion.
	market := sybilMarketWith(60, 3.2, false)

	learning.ApplySybilIndicators(rec, market, sybilDQWithWash(0.10),
		testSybilMinWallets, testSybilMaxWashScore, slog.Default())

	if rec.SybilClusterIndicators != nil {
		t.Errorf("unknown wash stats must not populate, got %+v", rec.SybilClusterIndicators)
	}
}

func TestLearningClassifier_Shadow_DoesNotPopulate(t *testing.T) {
	rec := sybilLossRecord()
	rec.Shadow = true
	rec.PnlUsd = 0 // shadow trades have no realized PnL anyway

	learning.ApplySybilIndicators(rec, sybilMarketWith(60, 3.2, true), sybilDQWithWash(0.10),
		testSybilMinWallets, testSybilMaxWashScore, slog.Default())

	if rec.SybilClusterIndicators != nil {
		t.Errorf("shadow trades must not populate, got %+v", rec.SybilClusterIndicators)
	}
}

func TestLearningClassifier_NilRecord_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil record must not panic, got %v", r)
		}
	}()
	learning.ApplySybilIndicators(nil, contracts.MarketDataDTO{}, contracts.DataQualityDTO{},
		testSybilMinWallets, testSybilMaxWashScore, nil)
}

// TestSybilIndicators_JSONRoundTrip locks the JSON wire-format the
// adapter relies on.
func TestSybilIndicators_JSONRoundTrip(t *testing.T) {
	rec := sybilLossRecord()
	rec.SybilClusterIndicators = &contracts.SybilIndicators{
		UniqueWallets1m:     77,
		WalletEntropyNats:   4.21,
		SuspectClusterSize:  0,
		FundingSourceShared: false,
	}

	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"sybil_cluster_indicators"`) {
		t.Errorf("expected sybil_cluster_indicators key in JSON, got: %s", raw)
	}

	var out contracts.LearningRecordDTO
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.SybilClusterIndicators == nil {
		t.Fatal("round-trip lost SybilClusterIndicators")
	}
	if out.SybilClusterIndicators.UniqueWallets1m != 77 {
		t.Errorf("UniqueWallets1m round-trip mismatch: got %d", out.SybilClusterIndicators.UniqueWallets1m)
	}
	if out.SybilClusterIndicators.WalletEntropyNats != 4.21 {
		t.Errorf("WalletEntropyNats round-trip mismatch: got %v", out.SybilClusterIndicators.WalletEntropyNats)
	}

	// nil indicators must omit the field entirely (omitempty).
	rec.SybilClusterIndicators = nil
	raw, _ = json.Marshal(rec)
	if strings.Contains(string(raw), `"sybil_cluster_indicators"`) {
		t.Errorf("nil indicators must omit field, got: %s", raw)
	}
}
