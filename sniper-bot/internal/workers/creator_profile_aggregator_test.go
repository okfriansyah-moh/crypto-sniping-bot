package workers

import (
	"context"
	"encoding/json"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
)

// TestRunCreatorProfileAggregator_NoEvent: nil event (idle queue) returns nil error.
func TestRunCreatorProfileAggregator_NoEvent(t *testing.T) {
	adapter := &stubAdapter{}
	err := RunCreatorProfileAggregator(context.Background(), adapter, nil, nil)
	if err != nil {
		t.Fatalf("expected nil error on idle queue, got: %v", err)
	}
}

// TestRunCreatorProfileAggregator_MarketDataEvent_SkipsFactoryProgram: pump.fun
// bonding-curve creator address is silently skipped without calling upsert.
func TestRunCreatorProfileAggregator_MarketDataEvent_SkipsFactoryProgram(t *testing.T) {
	md := contracts.MarketDataDTO{
		EventID:        "evt-001",
		Chain:          "solana-raydium",
		TokenAddress:   "TKN1",
		CreatorAddress: factoryPumpFunBondingCurve,
	}
	payload, _ := json.Marshal(md)
	adapter := &trackingAdapter{
		stubAdapter: stubAdapter{},
		nextEvent: &database.Event{
			EventID:   "evt-001",
			EventType: "market_data_event",
			Payload:   payload,
		},
	}

	err := RunCreatorProfileAggregator(context.Background(), adapter, testCfg(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adapter.upsertLaunchCalled {
		t.Error("UpsertCreatorProfileOnLaunch must NOT be called for factory programs")
	}
	if !adapter.markProcessedCalled {
		t.Error("MarkEventProcessed must be called even when skipping")
	}
}

// TestRunCreatorProfileAggregator_MarketDataEvent_SkipsEmptyCreator: empty
// CreatorAddress is skipped without calling upsert.
func TestRunCreatorProfileAggregator_MarketDataEvent_SkipsEmptyCreator(t *testing.T) {
	md := contracts.MarketDataDTO{
		EventID:        "evt-002",
		Chain:          "eth-uniswap-v2",
		TokenAddress:   "TKN2",
		CreatorAddress: "",
	}
	payload, _ := json.Marshal(md)
	adapter := &trackingAdapter{
		stubAdapter: stubAdapter{},
		nextEvent: &database.Event{
			EventID:   "evt-002",
			EventType: "market_data_event",
			Payload:   payload,
		},
	}

	err := RunCreatorProfileAggregator(context.Background(), adapter, testCfg(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adapter.upsertLaunchCalled {
		t.Error("UpsertCreatorProfileOnLaunch must NOT be called for empty creator")
	}
}

// TestRunCreatorProfileAggregator_MarketDataEvent_SkipsSystemMintToken: WSOL as
// TokenAddress must not increment creator_profiles (Task 14).
func TestRunCreatorProfileAggregator_MarketDataEvent_SkipsSystemMintToken(t *testing.T) {
	const wsol = "So11111111111111111111111111111111111111112"
	md := contracts.MarketDataDTO{
		EventID:        "evt-wsol",
		Chain:          "solana",
		TokenAddress:   wsol,
		CreatorAddress: "CreatorWallet1111111111111111111111111111111",
	}
	payload, _ := json.Marshal(md)
	adapter := &trackingAdapter{
		stubAdapter: stubAdapter{},
		nextEvent: &database.Event{
			EventID:   "evt-wsol",
			EventType: "market_data_event",
			Payload:   payload,
		},
	}
	cfg := testCfg()
	cfg.Solana.SystemMintReject = config.SystemMintRejectConfig{
		Enabled: true,
		Mints:   []string{wsol},
	}

	err := RunCreatorProfileAggregator(context.Background(), adapter, cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adapter.upsertLaunchCalled {
		t.Error("UpsertCreatorProfileOnLaunch must NOT be called for system mint TokenAddress")
	}
	if !adapter.markProcessedCalled {
		t.Error("MarkEventProcessed must be called when skipping system mint")
	}
}

// TestRunCreatorProfileAggregator_MarketDataEvent_IncrementsLaunch: valid creator
// triggers UpsertCreatorProfileOnLaunch and event is marked processed.
func TestRunCreatorProfileAggregator_MarketDataEvent_IncrementsLaunch(t *testing.T) {
	md := contracts.MarketDataDTO{
		EventID:        "evt-003",
		Chain:          "eth-uniswap-v2",
		TokenAddress:   "TKN3",
		CreatorAddress: "0xDevWallet",
	}
	payload, _ := json.Marshal(md)
	adapter := &trackingAdapter{
		stubAdapter: stubAdapter{},
		nextEvent: &database.Event{
			EventID:   "evt-003",
			EventType: "market_data_event",
			Payload:   payload,
		},
	}

	err := RunCreatorProfileAggregator(context.Background(), adapter, testCfg(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !adapter.upsertLaunchCalled {
		t.Error("UpsertCreatorProfileOnLaunch must be called for valid creator")
	}
	if adapter.upsertChain != "eth-uniswap-v2" || adapter.upsertCreator != "0xDevWallet" {
		t.Errorf("wrong args: chain=%q creator=%q", adapter.upsertChain, adapter.upsertCreator)
	}
	if !adapter.markProcessedCalled {
		t.Error("MarkEventProcessed must be called after successful upsert")
	}
}

// TestRunCreatorProfileAggregator_LearningRecordEvent_RUG: "RUG" outcome maps
// to the "rug" bucket and IncrementCreatorOutcome is called.
func TestRunCreatorProfileAggregator_LearningRecordEvent_RUG(t *testing.T) {
	lr := buildLearningRecord("RUG", 0.0, "SolChain", "0xCreator1")
	adapter := &trackingAdapter{
		stubAdapter: stubAdapter{},
		nextEvent:   learningEvent("evt-010", lr),
	}

	err := RunCreatorProfileAggregator(context.Background(), adapter, testCfg(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adapter.incrementOutcome != "rug" {
		t.Errorf("expected bucket 'rug', got %q", adapter.incrementOutcome)
	}
}

// TestRunCreatorProfileAggregator_LearningRecordEvent_GoldenGem: "TP" with
// PnlPct ≥ 200 maps to the "golden" bucket.
func TestRunCreatorProfileAggregator_LearningRecordEvent_GoldenGem(t *testing.T) {
	lr := buildLearningRecord("TP", 250.0, "SolChain", "0xCreator2")
	adapter := &trackingAdapter{
		stubAdapter: stubAdapter{},
		nextEvent:   learningEvent("evt-011", lr),
	}

	err := RunCreatorProfileAggregator(context.Background(), adapter, testCfg(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adapter.incrementOutcome != "golden" {
		t.Errorf("expected bucket 'golden', got %q", adapter.incrementOutcome)
	}
}

// TestRunCreatorProfileAggregator_LearningRecordEvent_Win: "TP" with PnlPct < 200
// maps to the "win" bucket.
func TestRunCreatorProfileAggregator_LearningRecordEvent_Win(t *testing.T) {
	lr := buildLearningRecord("TP", 150.0, "SolChain", "0xCreator3")
	adapter := &trackingAdapter{
		stubAdapter: stubAdapter{},
		nextEvent:   learningEvent("evt-012", lr),
	}

	err := RunCreatorProfileAggregator(context.Background(), adapter, testCfg(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adapter.incrementOutcome != "win" {
		t.Errorf("expected bucket 'win', got %q", adapter.incrementOutcome)
	}
}

// TestRunCreatorProfileAggregator_LearningRecordEvent_Loss: "SL" outcome maps
// to the "loss" bucket.
func TestRunCreatorProfileAggregator_LearningRecordEvent_Loss(t *testing.T) {
	lr := buildLearningRecord("SL", -40.0, "SolChain", "0xCreator4")
	adapter := &trackingAdapter{
		stubAdapter: stubAdapter{},
		nextEvent:   learningEvent("evt-013", lr),
	}

	err := RunCreatorProfileAggregator(context.Background(), adapter, testCfg(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adapter.incrementOutcome != "loss" {
		t.Errorf("expected bucket 'loss', got %q", adapter.incrementOutcome)
	}
}

// TestRunCreatorProfileAggregator_LearningRecordEvent_SkipsEmptyCreator: learning
// record with no EdgeSnapshot.CreatorAddress is silently skipped.
func TestRunCreatorProfileAggregator_LearningRecordEvent_SkipsEmptyCreator(t *testing.T) {
	lr := buildLearningRecord("TP", 100.0, "SolChain", "" /* empty creator */)
	adapter := &trackingAdapter{
		stubAdapter: stubAdapter{},
		nextEvent:   learningEvent("evt-014", lr),
	}

	err := RunCreatorProfileAggregator(context.Background(), adapter, testCfg(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adapter.incrementOutcomeCalled {
		t.Error("IncrementCreatorOutcome must NOT be called when creator is empty")
	}
}

// TestResolveOutcome_MissedPump_ReturnsEmpty: "MISSED_PUMP" and "CORRECT_REJECT"
// outcomes have no profile bucket — resolveOutcome must return "".
func TestResolveOutcome_MissedPump_ReturnsEmpty(t *testing.T) {
	for _, outcome := range []string{"MISSED_PUMP", "CORRECT_REJECT", "UNKNOWN"} {
		got := resolveOutcome(outcome, 0, 200)
		if got != "" {
			t.Errorf("resolveOutcome(%q) = %q, want empty", outcome, got)
		}
	}
}

// TestResolveOutcome_TimeAndTimeout_ReturnLoss: TIME, TIMEOUT, FORCED_CLOSE
// all map to "loss".
func TestResolveOutcome_TimeAndTimeout_ReturnLoss(t *testing.T) {
	for _, outcome := range []string{"TIME", "TIMEOUT", "FORCED_CLOSE"} {
		got := resolveOutcome(outcome, 0, 200)
		if got != "loss" {
			t.Errorf("resolveOutcome(%q) = %q, want 'loss'", outcome, got)
		}
	}
}

// TestIsFactoryProgram: verifies pump.fun factory addresses are detected.
func TestIsFactoryProgram(t *testing.T) {
	if !isFactoryProgram(factoryPumpFunBondingCurve) {
		t.Error("bonding-curve must be detected as factory program")
	}
	if !isFactoryProgram(factoryPumpFunAMM) {
		t.Error("AMM must be detected as factory program")
	}
	if isFactoryProgram("0xRegularCreator") {
		t.Error("regular address must NOT be detected as factory program")
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

// testCfg builds a minimal Config with a 200% golden-gem threshold.
func testCfg() *config.Config {
	return &config.Config{
		CreatorProfile: config.CreatorProfileConfig{
			GoldenGemPnlThresholdPct: 200.0,
		},
	}
}

// buildLearningRecord creates a LearningRecordDTO with the given parameters.
func buildLearningRecord(outcome string, pnlPct float64, chain, creatorAddress string) contracts.LearningRecordDTO {
	return contracts.LearningRecordDTO{
		EventID:       "lr-evt-001",
		TraceID:       "trace-001",
		CorrelationID: "corr-001",
		VersionID:     "v1",
		Outcome:       outcome,
		PnlPct:        pnlPct,
		FeaturesSnapshot: contracts.FeatureDTO{
			EventID: "feat-001",
			Chain:   chain,
		},
		EdgeSnapshot: contracts.EdgeDTO{
			EventID:        "edge-001",
			CreatorAddress: creatorAddress,
		},
	}
}

// learningEvent wraps a LearningRecordDTO into a database.Event.
func learningEvent(eventID string, lr contracts.LearningRecordDTO) *database.Event {
	payload, _ := json.Marshal(lr)
	return &database.Event{
		EventID:   eventID,
		EventType: "learning_record_event",
		Payload:   payload,
		TraceID:   "trace-001",
	}
}

// ── trackingAdapter ──────────────────────────────────────────────────────────

// trackingAdapter wraps stubAdapter to record which creator profile methods
// were called and with what arguments.
type trackingAdapter struct {
	stubAdapter

	nextEvent *database.Event

	upsertLaunchCalled     bool
	upsertChain            string
	upsertCreator          string
	incrementOutcomeCalled bool
	incrementOutcome       string
	markProcessedCalled    bool
}

func (a *trackingAdapter) ClaimNextEvent(_ context.Context, _ string, _ []string) (*database.Event, error) {
	evt := a.nextEvent
	a.nextEvent = nil
	return evt, nil
}

func (a *trackingAdapter) MarkEventProcessed(_ context.Context, _ string) error {
	a.markProcessedCalled = true
	return nil
}

func (a *trackingAdapter) ReleaseEventClaim(_ context.Context, _ string) error {
	return nil
}

func (a *trackingAdapter) UpsertCreatorProfileOnLaunch(_ context.Context, chain, creator string) error {
	a.upsertLaunchCalled = true
	a.upsertChain = chain
	a.upsertCreator = creator
	return nil
}

func (a *trackingAdapter) IncrementCreatorOutcome(_ context.Context, _, _, outcome string) error {
	a.incrementOutcomeCalled = true
	a.incrementOutcome = outcome
	return nil
}

func (a *trackingAdapter) GetCreatorProfile(_ context.Context, _, _ string) (contracts.CreatorProfile, bool, error) {
	return contracts.CreatorProfile{}, false, nil
}

func (a *trackingAdapter) InsertEvent(_ context.Context, _ database.Event) error {
	return nil
}
