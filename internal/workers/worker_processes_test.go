package workers

import (
	"context"
	"encoding/json"
	"testing"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// defaultLC returns a non-nil lifecycle for transition mocks.
func defaultLC(id string) *database.Lifecycle {
	return &database.Lifecycle{
		TokenLifecycleID: id,
		CurrentState:     "DETECTED",
		StateVersion:     1,
	}
}

func makeDQEvent(dto contracts.MarketDataDTO) *database.Event {
	payload, _ := json.Marshal(dto)
	return &database.Event{
		EventID:       "evt-1",
		EventType:     "market_data_event",
		Payload:       payload,
		TraceID:       "trace-1",
		CorrelationID: "corr-1",
		VersionID:     "v1",
	}
}

func minConfig() *config.Config {
	return &config.Config{
		Edge: config.EdgeConfig{
			MinVelocityScore:  0.0,
			MinLiquidityScore: 0.0,
			TTLSeconds:        5,
		},
		Validation: config.ValidationConfig{
			PriorProbability: 0.7,
			PriorGainBps:     1000,
			PriorLossBps:     200,
			PriorSlippageBps: 50,
			EvThresholdBps:   10,
			FixedCostsBps:    20,
			BuildSubmitP95Ms: 500,
			TTLSeconds:       5,
		},
		Selection: config.SelectionConfig{MaxOpenPositions: 5},
		Capital: config.CapitalConfig{
			FixedEntrySizeUsd:      100,
			MaxTotalExposureUsd:    10000,
			MaxConcurrentPositions: 5,
			MaxSizeUsd:             500,
			TTLSeconds:             5,
		},
	}
}

// ── DataQualityWorker ─────────────────────────────────────────────────────────

func TestNewDataQualityWorker_NilLogger_DoesNotPanic(t *testing.T) {
	adapter := &stubAdapter{lifecycleResult: defaultLC("lc-1")}
	w := NewDataQualityWorker(adapter, minConfig(), nil)
	if w == nil {
		t.Fatal("expected non-nil worker")
	}
}

func TestDataQualityWorker_Process_HappyPath(t *testing.T) {
	// Arrange
	adapter := &stubAdapter{lifecycleResult: defaultLC("lc-1")}
	w := NewDataQualityWorker(adapter, minConfig(), nil)

	dto := contracts.MarketDataDTO{
		TokenAddress:  "0xTEST1",
		Chain:         "eth",
		TraceID:       "trace-1",
		CorrelationID: "corr-1",
		VersionID:     "v1",
		EventTopic:    "PairCreated",
		PoolAddress:   "0xPOOL",
	}
	evt := makeDQEvent(dto)

	// Act
	out, err := w.Process(context.Background(), evt)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Output may be nil if the DQ module rejects (allowed); if non-nil it's a dq event.
	if out != nil && out.EventType != "data_quality_event" {
		t.Errorf("expected data_quality_event, got %q", out.EventType)
	}
}

func TestDataQualityWorker_Process_InvalidPayload_ReturnsError(t *testing.T) {
	adapter := &stubAdapter{}
	w := NewDataQualityWorker(adapter, minConfig(), nil)

	evt := &database.Event{
		EventID:   "bad-evt",
		EventType: "market_data_event",
		Payload:   []byte("not json"),
	}

	_, err := w.Process(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for invalid JSON payload")
	}
}

func TestDataQualityWorker_Process_EmptyTokenAddress_ReturnsError(t *testing.T) {
	adapter := &stubAdapter{lifecycleResult: defaultLC("lc-1")}
	w := NewDataQualityWorker(adapter, minConfig(), nil)

	dto := contracts.MarketDataDTO{TokenAddress: ""} // empty
	evt := makeDQEvent(dto)

	_, err := w.Process(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for empty token address")
	}
}

// ── FeaturesWorker ────────────────────────────────────────────────────────────

func TestNewFeaturesWorker_NilLogger_DoesNotPanic(t *testing.T) {
	adapter := &stubAdapter{lifecycleResult: defaultLC("lc-1")}
	w := NewFeaturesWorker(adapter, minConfig(), nil)
	if w == nil {
		t.Fatal("expected non-nil worker")
	}
}

func TestFeaturesWorker_Process_HappyPath(t *testing.T) {
	// Arrange
	lc := &database.Lifecycle{
		TokenLifecycleID: "lc-feat",
		CurrentState:     "DQ_PASSED",
		StateVersion:     1,
	}
	adapter := &stubAdapter{lifecycleResult: lc}
	w := NewFeaturesWorker(adapter, minConfig(), nil)

	dto := contracts.DataQualityDTO{
		EventID:          "dq-evt-1",
		TokenAddress:     "0xFEATTOKEN",
		TokenLifecycleID: "lc-feat",
		TraceID:          "trace-1",
		CorrelationID:    "corr-1",
		VersionID:        "v1",
		Decision:         "PASS",
	}
	payload, _ := json.Marshal(dto)
	evt := &database.Event{
		EventID:       "feat-evt-1",
		EventType:     "data_quality_event",
		Payload:       payload,
		TraceID:       "trace-1",
		CorrelationID: "corr-1",
		VersionID:     "v1",
	}

	// Act
	out, err := w.Process(context.Background(), evt)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == nil {
		t.Fatal("expected output event")
	}
	if out.EventType != "feature_event" {
		t.Errorf("expected feature_event, got %q", out.EventType)
	}
}

func TestFeaturesWorker_Process_InvalidPayload_ReturnsError(t *testing.T) {
	adapter := &stubAdapter{}
	w := NewFeaturesWorker(adapter, minConfig(), nil)

	evt := &database.Event{
		Payload: []byte("invalid json"),
	}
	_, err := w.Process(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for invalid payload")
	}
}

// ── EdgeWorker ────────────────────────────────────────────────────────────────

func TestNewEdgeWorker_NilLogger_DoesNotPanic(t *testing.T) {
	adapter := &stubAdapter{lifecycleResult: defaultLC("lc-1")}
	w := NewEdgeWorker(adapter, minConfig(), nil)
	if w == nil {
		t.Fatal("expected non-nil worker")
	}
}

func TestEdgeWorker_Process_InvalidPayload_ReturnsError(t *testing.T) {
	adapter := &stubAdapter{}
	w := NewEdgeWorker(adapter, minConfig(), nil)

	evt := &database.Event{Payload: []byte("invalid json")}
	_, err := w.Process(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for invalid payload")
	}
}

func TestEdgeWorker_Process_HappyPath(t *testing.T) {
	// Arrange
	lc := &database.Lifecycle{
		TokenLifecycleID: "lc-edge",
		CurrentState:     "FEATURE_READY",
		StateVersion:     1,
	}
	adapter := &stubAdapter{lifecycleResult: lc}
	w := NewEdgeWorker(adapter, minConfig(), nil)

	dto := contracts.FeatureDTO{
		EventID:          "feat-evt-1",
		TokenAddress:     "0xEDGETOKEN",
		TokenLifecycleID: "lc-edge",
		TraceID:          "trace-1",
		CorrelationID:    "corr-1",
		VersionID:        "v1",
		TxVelocityScore:  0.9,
		LiquidityScore:   0.8,
	}
	payload, _ := json.Marshal(dto)
	evt := &database.Event{
		EventID:       "edge-evt-1",
		EventType:     "feature_event",
		Payload:       payload,
		TraceID:       "trace-1",
		CorrelationID: "corr-1",
		VersionID:     "v1",
	}

	// Act
	out, err := w.Process(context.Background(), evt)

	// Assert — no error (edge may be nil if no edge detected, that's OK)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// If an edge event is emitted, it must be the right type.
	if out != nil && out.EventType != "edge_event" {
		t.Errorf("expected edge_event, got %q", out.EventType)
	}
}

// ── SelectionWorker ───────────────────────────────────────────────────────────

func TestNewSelectionWorker_NilLogger_DoesNotPanic(t *testing.T) {
	adapter := &stubAdapter{lifecycleResult: defaultLC("lc-1")}
	w := NewSelectionWorker(adapter, minConfig(), nil)
	if w == nil {
		t.Fatal("expected non-nil worker")
	}
}

func TestSelectionWorker_Process_InvalidPayload_ReturnsError(t *testing.T) {
	adapter := &stubAdapter{}
	w := NewSelectionWorker(adapter, minConfig(), nil)

	evt := &database.Event{Payload: []byte("invalid json")}
	_, err := w.Process(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for invalid payload")
	}
}

// ── CapitalWorker ─────────────────────────────────────────────────────────────

func TestNewCapitalWorker_NilLogger_DoesNotPanic(t *testing.T) {
	adapter := &stubAdapter{lifecycleResult: defaultLC("lc-1")}
	w := NewCapitalWorker(adapter, minConfig(), nil)
	if w == nil {
		t.Fatal("expected non-nil worker")
	}
}

func TestCapitalWorker_Process_InvalidPayload_ReturnsError(t *testing.T) {
	adapter := &stubAdapter{}
	w := NewCapitalWorker(adapter, minConfig(), nil)

	evt := &database.Event{Payload: []byte("invalid json")}
	_, err := w.Process(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for invalid payload")
	}
}

// ── ValidationWorker ─────────────────────────────────────────────────────────

func TestNewValidationWorker_NilLogger_DoesNotPanic(t *testing.T) {
	adapter := &stubAdapter{lifecycleResult: defaultLC("lc-1")}
	w := NewValidationWorker(adapter, minConfig(), nil)
	if w == nil {
		t.Fatal("expected non-nil worker")
	}
}

func TestValidationWorker_Process_InvalidPayload_ReturnsError(t *testing.T) {
	adapter := &stubAdapter{}
	w := NewValidationWorker(adapter, minConfig(), nil)

	evt := &database.Event{Payload: []byte("invalid json")}
	_, err := w.Process(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for invalid payload")
	}
}

// ── ExecutionWorker ───────────────────────────────────────────────────────────

func TestNewExecutionWorker_NilLogger_DoesNotPanic(t *testing.T) {
	adapter := &stubAdapter{}
	w := NewExecutionWorker(adapter, minConfig(), nil, "", 1, "0xROUTER", nil, nil)
	if w == nil {
		t.Fatal("expected non-nil worker")
	}
}

func TestExecutionWorker_Process_InvalidPayload_ReturnsError(t *testing.T) {
	adapter := &stubAdapter{}
	w := NewExecutionWorker(adapter, minConfig(), nil, "", 1, "0xROUTER", nil, nil)

	evt := &database.Event{Payload: []byte("invalid json")}
	_, err := w.Process(context.Background(), evt)
	if err == nil {
		t.Fatal("expected error for invalid payload")
	}
}
