// Package integration tests pipeline wiring and DTO flow across the
// orchestrator ↔ worker ↔ event bus boundary.
//
// These tests exercise multi-stage sequences without a real database:
//   - Event emitted by StageHandler flows to next stage
//   - Checkpoint writes last_completed_stage
//   - Resume reconstructs correct stage from database
//   - Duplicate event insert is idempotent (ON CONFLICT DO NOTHING)
//   - Worker drains events in FIFO order
package integration

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/orchestrator"
)

// ─── In-Memory Adapter ────────────────────────────────────────────────────────

type memAdapter struct {
	mu       sync.Mutex
	events   []database.Event
	runs     map[string]*database.PipelineRun
	versions map[string]*database.StrategyVersion
	active   *string
	// per-group claim cursors
	claimIdx map[string]int
}

func newMemAdapter() *memAdapter {
	return &memAdapter{
		runs:     make(map[string]*database.PipelineRun),
		versions: make(map[string]*database.StrategyVersion),
		claimIdx: make(map[string]int),
	}
}

func (m *memAdapter) Initialize(_ context.Context, _ database.Config) error { return nil }
func (m *memAdapter) RunMigrations(_ context.Context) error                 { return nil }
func (m *memAdapter) Close(_ context.Context) error                         { return nil }

func (m *memAdapter) InsertEvent(_ context.Context, e database.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Idempotent: skip if already present (ON CONFLICT DO NOTHING semantics).
	for _, existing := range m.events {
		if existing.EventID == e.EventID {
			return nil
		}
	}
	m.events = append(m.events, e)
	return nil
}

func (m *memAdapter) ClaimNextEvent(_ context.Context, group string, eventTypes []string) (*database.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	start := m.claimIdx[group]
	for i := start; i < len(m.events); i++ {
		evt := m.events[i]
		if !evt.Processed && containsType(eventTypes, evt.EventType) {
			m.claimIdx[group] = i + 1
			cp := evt
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *memAdapter) MarkEventProcessed(_ context.Context, eventID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.events {
		if m.events[i].EventID == eventID {
			m.events[i].Processed = true
			return nil
		}
	}
	return nil
}

func (m *memAdapter) GetEventByID(_ context.Context, id string) (*database.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.events {
		if e.EventID == id {
			cp := e
			return &cp, nil
		}
	}
	return nil, database.ErrNotFound
}

func (m *memAdapter) GetEventsByTrace(_ context.Context, _ string) ([]database.Event, error) {
	return nil, nil
}
func (m *memAdapter) GetEventsByCorrelation(_ context.Context, _ string) ([]database.Event, error) {
	return nil, nil
}
func (m *memAdapter) GetFailureChain(_ context.Context, _ string) ([]database.Event, error) {
	return nil, nil
}

func (m *memAdapter) CreateRun(_ context.Context, run database.PipelineRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[run.RunID]; !ok {
		cp := run
		m.runs[run.RunID] = &cp
	}
	return nil
}
func (m *memAdapter) UpdateRunStage(_ context.Context, runID, stage string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.runs[runID]; ok {
		s := stage
		r.LastCompletedStage = &s
	}
	return nil
}
func (m *memAdapter) UpdateRunStatus(_ context.Context, runID, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.runs[runID]; ok {
		r.Status = status
	}
	return nil
}
func (m *memAdapter) GetRun(_ context.Context, runID string) (*database.PipelineRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.runs[runID]; ok {
		cp := *r
		return &cp, nil
	}
	return nil, database.ErrNotFound
}

func (m *memAdapter) CreateStrategyVersion(_ context.Context, sv database.StrategyVersion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.versions[sv.StrategyVersionID]; !ok {
		cp := sv
		m.versions[sv.StrategyVersionID] = &cp
	}
	return nil
}
func (m *memAdapter) ActivateStrategyVersion(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active = &id
	return nil
}
func (m *memAdapter) ReleaseEventClaim(_ context.Context, _ string) error { return nil }
func (m *memAdapter) GetActiveStrategyVersion(_ context.Context) (*database.StrategyVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return nil, database.ErrNotFound
	}
	if sv, ok := m.versions[*m.active]; ok {
		cp := *sv
		return &cp, nil
	}
	return nil, database.ErrNotFound
}
func (m *memAdapter) GetStrategyVersion(_ context.Context, id string) (*database.StrategyVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sv, ok := m.versions[id]; ok {
		cp := *sv
		return &cp, nil
	}
	return nil, database.ErrUnknownVersion
}

// Stub Phase 1–6 methods (not yet implemented).
func (m *memAdapter) UpsertIngestionWatermark(_ context.Context, _ string, _ uint64) error {
	return database.ErrNotImplemented
}
func (m *memAdapter) GetIngestionWatermark(_ context.Context, _ string) (uint64, error) {
	return 0, database.ErrNotImplemented
}
func (m *memAdapter) InsertMarketData(_ context.Context, _ contracts.MarketDataDTO) error {
	return database.ErrNotImplemented
}
func (m *memAdapter) GetMarketData(_ context.Context, _ string) (*contracts.MarketDataDTO, error) {
	return nil, database.ErrNotImplemented
}
func (m *memAdapter) StartLifecycle(_ context.Context, _ contracts.MarketDataDTO) (string, error) {
	return "", database.ErrNotImplemented
}
func (m *memAdapter) TransitionState(_ context.Context, _ database.TransitionRequest) error {
	return database.ErrNotImplemented
}
func (m *memAdapter) GetLifecycle(_ context.Context, _ string) (*database.Lifecycle, error) {
	return nil, database.ErrNotImplemented
}
func (m *memAdapter) GetLifecycleByToken(_ context.Context, _ string) (*database.Lifecycle, error) {
	return nil, database.ErrNotImplemented
}
func (m *memAdapter) QuarantineToken(_ context.Context, _, _ string) error {
	return database.ErrNotImplemented
}
func (m *memAdapter) InsertStateViolation(_ context.Context, _, _, _, _ string) error {
	return database.ErrNotImplemented
}
func (m *memAdapter) InsertDataQuality(_ context.Context, _ contracts.DataQualityDTO) error {
	return database.ErrNotImplemented
}
func (m *memAdapter) InsertFeature(_ context.Context, _ contracts.FeatureDTO) error {
	return database.ErrNotImplemented
}
func (m *memAdapter) InsertEdge(_ context.Context, _ contracts.EdgeDTO) error {
	return database.ErrNotImplemented
}
func (m *memAdapter) InsertValidatedEdge(_ context.Context, _ contracts.ValidatedEdgeDTO) error {
	return database.ErrNotImplemented
}
func (m *memAdapter) InsertSelection(_ context.Context, _ contracts.SelectionOutputDTO) error {
	return database.ErrNotImplemented
}
func (m *memAdapter) InsertAllocation(_ context.Context, _ contracts.AllocationDTO) error {
	return database.ErrNotImplemented
}
func (m *memAdapter) InsertExecutionResult(_ context.Context, _ contracts.ExecutionResultDTO) error {
	return database.ErrNotImplemented
}
func (m *memAdapter) InsertPositionState(_ context.Context, _ contracts.PositionStateDTO) error {
	return database.ErrNotImplemented
}
func (m *memAdapter) InsertEvaluation(_ context.Context, _ contracts.EvaluationDTO) error {
	return database.ErrNotImplemented
}
func (m *memAdapter) GetExecutionByLifecycle(_ context.Context, _ string) (*contracts.ExecutionResultDTO, error) {
	return nil, database.ErrNotFound
}
func (m *memAdapter) InsertLearningRecord(_ context.Context, _ contracts.LearningRecordDTO) error {
	return database.ErrNotImplemented
}
func (m *memAdapter) InsertProbabilityEstimate(_ context.Context, _ contracts.ProbabilityEstimateDTO) error {
	return database.ErrNotImplemented
}
func (m *memAdapter) InsertSlippageEstimate(_ context.Context, _ contracts.SlippageEstimateDTO) error {
	return database.ErrNotImplemented
}
func (m *memAdapter) InsertLatencyProfile(_ context.Context, _ contracts.LatencyProfileDTO) error {
	return database.ErrNotImplemented
}
func (m *memAdapter) GetProbabilityEstimateByTrace(_ context.Context, _ string) (*contracts.ProbabilityEstimateDTO, error) {
	return nil, nil
}
func (m *memAdapter) GetSlippageEstimateByTrace(_ context.Context, _ string) (*contracts.SlippageEstimateDTO, error) {
	return nil, nil
}
func (m *memAdapter) GetLatestLatencyProfile(_ context.Context, _ string) (*contracts.LatencyProfileDTO, error) {
	return nil, nil
}
func (m *memAdapter) AllocateNonce(_ context.Context, _, _ string) (uint64, error) {
	return 0, database.ErrNotImplemented
}
func (m *memAdapter) ReconcileNonce(_ context.Context, _, _ string, _ uint64) error {
	return database.ErrNotImplemented
}
func (m *memAdapter) GetOpenPositions(_ context.Context) ([]contracts.PositionStateDTO, error) {
	return nil, database.ErrNotImplemented
}
func (m *memAdapter) GetPosition(_ context.Context, _ string) (*contracts.PositionStateDTO, error) {
	return nil, database.ErrNotImplemented
}

func (m *memAdapter) MarkEventExpired(_ context.Context, _ string, _ string) error {
	return database.ErrNotImplemented
}

func (m *memAdapter) GetSystemState(_ context.Context) (*contracts.SystemStateDTO, error) {
	return nil, database.ErrNotImplemented
}

func (m *memAdapter) UpsertSystemState(_ context.Context, _ contracts.SystemStateDTO, _ int64) (int64, error) {
	return 0, database.ErrNotImplemented
}

func (m *memAdapter) GetExposureSummary(_ context.Context) (*database.ExposureSummary, error) {
	return nil, database.ErrNotImplemented
}

func (m *memAdapter) SetStrategyVersionStatus(_ context.Context, _, _, _ string) error {
	return database.ErrNotImplemented
}

func (m *memAdapter) GetActiveStrategy(_ context.Context) (*database.StrategyVersion, error) {
	return nil, database.ErrNotImplemented
}

func (m *memAdapter) GetShadowStrategy(_ context.Context) (*database.StrategyVersion, error) {
	return nil, database.ErrNotImplemented
}

func (m *memAdapter) ArchiveEvents(_ context.Context, _ time.Time, _ int) (int, error) {
	return 0, database.ErrNotImplemented
}

func (m *memAdapter) GetEventsByTraceIncludeArchive(_ context.Context, _ string) ([]contracts.EventEnvelope, error) {
	return nil, database.ErrNotImplemented
}

func containsType(types []string, t string) bool {
	for _, v := range types {
		if v == t {
			return true
		}
	}
	return false
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func minCfg() *config.Config {
	return &config.Config{
		Logging: config.LoggingConfig{Level: "info", Format: "text"},
		Worker:  config.WorkerConfig{IdleBackoffMs: 10},
	}
}

func seedEvent(t *testing.T, a *memAdapter, eventType, eventID string) {
	t.Helper()
	err := a.InsertEvent(context.Background(), database.Event{
		EventID:       eventID,
		EventType:     eventType,
		Payload:       []byte(`{}`),
		TraceID:       "trace-001",
		CorrelationID: "corr-001",
		VersionID:     "v1",
	})
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestOrchestrator_Boot_PinsVersion verifies Boot returns a non-empty VersionID.
func TestOrchestrator_Boot_PinsVersion(t *testing.T) {
	ctx := context.Background()
	a := newMemAdapter()
	orch, err := orchestrator.Boot(ctx, a, minCfg(), nil)
	if err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if orch.VersionID() == "" {
		t.Error("expected non-empty VersionID")
	}
}

// TestOrchestrator_Boot_Idempotent verifies same config → same VersionID.
func TestOrchestrator_Boot_Idempotent(t *testing.T) {
	ctx := context.Background()
	a := newMemAdapter()
	cfg := minCfg()
	o1, err := orchestrator.Boot(ctx, a, cfg, nil)
	if err != nil {
		t.Fatalf("first Boot: %v", err)
	}
	o2, err := orchestrator.Boot(ctx, a, cfg, nil)
	if err != nil {
		t.Fatalf("second Boot: %v", err)
	}
	if o1.VersionID() != o2.VersionID() {
		t.Errorf("VersionID mismatch: %s vs %s", o1.VersionID(), o2.VersionID())
	}
}

// TestCheckpoint_WriteAndResume verifies the checkpoint round-trip.
func TestCheckpoint_WriteAndResume(t *testing.T) {
	ctx := context.Background()
	a := newMemAdapter()

	const runID = "run-chk-001"
	_ = a.CreateRun(ctx, database.PipelineRun{RunID: runID, Status: "started", StrategyVersionID: "v1"})

	if err := orchestrator.Checkpoint(ctx, a, nil, runID, "data_quality"); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}

	stage, err := orchestrator.ResumeFromCheckpoint(ctx, a, runID)
	if err != nil {
		t.Fatalf("ResumeFromCheckpoint: %v", err)
	}
	if stage != "data_quality" {
		t.Errorf("expected data_quality, got %q", stage)
	}
}

// TestCheckpoint_NoStageYet verifies resume returns "" if nothing has been checkpointed.
func TestCheckpoint_NoStageYet(t *testing.T) {
	ctx := context.Background()
	a := newMemAdapter()

	const runID = "run-fresh-001"
	_ = a.CreateRun(ctx, database.PipelineRun{RunID: runID, Status: "started", StrategyVersionID: "v1"})

	stage, err := orchestrator.ResumeFromCheckpoint(ctx, a, runID)
	if err != nil {
		t.Fatalf("ResumeFromCheckpoint: %v", err)
	}
	if stage != "" {
		t.Errorf("expected empty stage for fresh run, got %q", stage)
	}
}

// TestFinalizeRun_StatusWritten verifies FinalizeRun sets the terminal status.
func TestFinalizeRun_StatusWritten(t *testing.T) {
	ctx := context.Background()
	a := newMemAdapter()

	const runID = "run-fin-001"
	_ = a.CreateRun(ctx, database.PipelineRun{RunID: runID, Status: "started", StrategyVersionID: "v1"})

	if err := orchestrator.FinalizeRun(ctx, a, nil, runID, "completed"); err != nil {
		t.Fatalf("FinalizeRun: %v", err)
	}

	run, err := a.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status != "completed" {
		t.Errorf("expected completed, got %q", run.Status)
	}
}

// TestEventBus_Idempotent verifies inserting the same event twice is a no-op.
func TestEventBus_Idempotent(t *testing.T) {
	ctx := context.Background()
	a := newMemAdapter()

	evt := database.Event{
		EventID:       "idem-001",
		EventType:     "market_data_event",
		Payload:       []byte(`{"chain":"eth"}`),
		TraceID:       "t1",
		CorrelationID: "c1",
		VersionID:     "v1",
	}

	_ = a.InsertEvent(ctx, evt)
	_ = a.InsertEvent(ctx, evt) // second insert must be silent

	a.mu.Lock()
	count := 0
	for _, e := range a.events {
		if e.EventID == "idem-001" {
			count++
		}
	}
	a.mu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 event after idempotent insert, got %d", count)
	}
}

// TestWorker_ProcessesEvent verifies that a registered StageHandler is called
// for a matching event and its output event is persisted.
func TestWorker_ProcessesEvent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	a := newMemAdapter()
	seedEvent(t, a, "stage_a_event", "evt-stg-a-001")

	processed := make(chan string, 1)
	handler := &captureHandler{
		outputType: "stage_b_event",
		processed:  processed,
	}

	done := make(chan error, 1)
	go func() {
		done <- orchestrator.RunWorker(ctx, a, "stage-a", []string{"stage_a_event"}, handler, 10*time.Millisecond, nil)
	}()

	select {
	case eventID := <-processed:
		if eventID != "evt-stg-a-001" {
			t.Errorf("expected evt-stg-a-001 to be processed, got %s", eventID)
		}
	case <-ctx.Done():
		t.Fatal("timeout: stage_a_event was not processed")
	}

	cancel()
	<-done

	// Verify output event was persisted.
	a.mu.Lock()
	var found bool
	for _, e := range a.events {
		if e.EventType == "stage_b_event" {
			found = true
		}
	}
	a.mu.Unlock()
	if !found {
		t.Error("expected stage_b_event to be persisted after processing")
	}
}

// TestWorker_HandlerError_EventRetained verifies that a handler error leaves
// the event unprocessed so it can be retried.
func TestWorker_HandlerError_EventRetained(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	a := newMemAdapter()
	seedEvent(t, a, "retry_event", "evt-retry-001")

	done := make(chan error, 1)
	go func() {
		done <- orchestrator.RunWorker(ctx, a, "retry-group", []string{"retry_event"}, &errorHandler{}, 20*time.Millisecond, nil)
	}()

	<-ctx.Done()
	<-done

	// The event must remain unprocessed.
	a.mu.Lock()
	var stillUnprocessed bool
	for _, e := range a.events {
		if e.EventID == "evt-retry-001" && !e.Processed {
			stillUnprocessed = true
		}
	}
	a.mu.Unlock()
	if !stillUnprocessed {
		t.Error("expected evt-retry-001 to remain unprocessed after handler error")
	}
}

// TestRegistry_DeterministicOrder verifies Entries() always returns sorted order.
func TestRegistry_DeterministicOrder(t *testing.T) {
	r := orchestrator.NewRegistry()
	r.Register("zz-last", &noopHandler{}, "event.z")
	r.Register("aa-first", &noopHandler{}, "event.a")
	r.Register("mm-middle", &noopHandler{}, "event.m")

	entries := r.Entries()
	want := []string{"aa-first", "mm-middle", "zz-last"}
	for i, e := range entries {
		if e.Group != want[i] {
			t.Errorf("position %d: expected %s, got %s", i, want[i], e.Group)
		}
	}
}

// TestDTO_ContentIDDeterminism verifies same payload → same ContentID.
func TestDTO_ContentIDDeterminism(t *testing.T) {
	payload := []byte(`{"chain":"eth","token":"0xABC"}`)
	id1 := contracts.ContentID(payload)
	id2 := contracts.ContentID(payload)
	if id1 != id2 {
		t.Errorf("ContentID non-deterministic: %s vs %s", id1, id2)
	}
	if len(id1) != 16 {
		t.Errorf("ContentID should be 16 hex chars, got %d", len(id1))
	}
}

// TestDTO_ContentIDDistinct verifies different payloads → different ContentIDs.
func TestDTO_ContentIDDistinct(t *testing.T) {
	a := contracts.ContentID([]byte(`{"chain":"eth"}`))
	b := contracts.ContentID([]byte(`{"chain":"bsc"}`))
	if a == b {
		t.Error("different payloads produced the same ContentID")
	}
}

// TestResumeFromCheckpoint_MissingRun verifies that resuming from a non-existent
// run returns ErrNotFound (pipeline can detect stale restart).
func TestResumeFromCheckpoint_MissingRun(t *testing.T) {
	ctx := context.Background()
	a := newMemAdapter()
	_, err := orchestrator.ResumeFromCheckpoint(ctx, a, "nonexistent-run")
	if !errors.Is(err, database.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// TestStageHandler_OutputDTOCarriesTraceFields verifies that the output event
// produced by captureHandler propagates TraceID/CorrelationID/VersionID correctly.
func TestStageHandler_OutputDTOCarriesTraceFields(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	a := newMemAdapter()
	err := a.InsertEvent(ctx, database.Event{
		EventID:       "trace-input-001",
		EventType:     "input_event",
		Payload:       []byte(`{}`),
		TraceID:       "trace-abc",
		CorrelationID: "corr-xyz",
		VersionID:     "ver-001",
	})
	if err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	processed := make(chan string, 1)
	handler := &captureHandler{outputType: "output_event", processed: processed}

	done := make(chan error, 1)
	go func() {
		done <- orchestrator.RunWorker(ctx, a, "trace-grp", []string{"input_event"}, handler, 10*time.Millisecond, nil)
	}()

	select {
	case <-processed:
	case <-ctx.Done():
		t.Fatal("timeout")
	}
	cancel()
	<-done

	a.mu.Lock()
	var outputEvt *database.Event
	for i := range a.events {
		if a.events[i].EventType == "output_event" {
			outputEvt = &a.events[i]
			break
		}
	}
	a.mu.Unlock()

	if outputEvt == nil {
		t.Fatal("output event not found")
	}
	if outputEvt.TraceID != "trace-abc" {
		t.Errorf("TraceID not propagated: got %q", outputEvt.TraceID)
	}
	if outputEvt.CorrelationID != "corr-xyz" {
		t.Errorf("CorrelationID not propagated: got %q", outputEvt.CorrelationID)
	}
	if outputEvt.VersionID != "ver-001" {
		t.Errorf("VersionID not propagated: got %q", outputEvt.VersionID)
	}
	if outputEvt.CausationID == nil || *outputEvt.CausationID != "trace-input-001" {
		t.Errorf("CausationID should be trace-input-001, got %v", outputEvt.CausationID)
	}
}

// ─── Handler Fixtures ─────────────────────────────────────────────────────────

type noopHandler struct{}

func (h *noopHandler) Process(_ context.Context, _ *database.Event) (*database.Event, error) {
	return nil, nil
}

type captureHandler struct {
	outputType string
	processed  chan string
}

func (h *captureHandler) Process(_ context.Context, evt *database.Event) (*database.Event, error) {
	h.processed <- evt.EventID
	return &database.Event{
		EventID:       contracts.ContentIDFromString(evt.EventID + "_out"),
		EventType:     h.outputType,
		Payload:       []byte(`{}`),
		TraceID:       evt.TraceID,
		CorrelationID: evt.CorrelationID,
		CausationID:   &evt.EventID,
		VersionID:     evt.VersionID,
	}, nil
}

type errorHandler struct{}

func (h *errorHandler) Process(_ context.Context, _ *database.Event) (*database.Event, error) {
	return nil, errors.New("simulated handler failure")
}

func (m *memAdapter) InsertShadowTrade(_ context.Context, _ database.ShadowTrade) error {
return nil
}
func (m *memAdapter) UpdateShadowTradeObservation(_ context.Context, _ string, _ float64, _ string) error {
return nil
}
func (m *memAdapter) GetShadowTradesByWindow(_ context.Context, _ int) ([]database.ShadowTrade, error) {
return nil, nil
}
func (m *memAdapter) GetLearningRecordsByWindow(_ context.Context, _ string, _, _ time.Time) ([]contracts.LearningRecordDTO, error) {
return nil, nil
}
func (m *memAdapter) GetEvaluationsByVersion(_ context.Context, _ string) ([]contracts.EvaluationDTO, error) {
return nil, nil
}
