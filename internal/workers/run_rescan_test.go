package workers

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
)

// ── rescan test helpers ───────────────────────────────────────────────────────

// rescanAdapter embeds stubAdapter and overrides only the methods called by
// the rescan worker: GetActiveStrategyVersion, GetSystemState,
// GetTokensForRescan, InsertMarketData, InsertEvent.
type rescanAdapter struct {
	stubAdapter
	mu            sync.Mutex
	events        []database.Event
	marketData    []contracts.MarketDataDTO
	systemState   *contracts.SystemStateDTO
	queryResult   []contracts.MarketDataDTO
	queryErr      error
	lastQuery     database.RescanQuery // captured for IncludeSkippedForRetry assertions
	activeVersion *database.StrategyVersion
}

func newRescanAdapter(queryResult []contracts.MarketDataDTO) *rescanAdapter {
	return &rescanAdapter{
		queryResult: queryResult,
		activeVersion: &database.StrategyVersion{
			StrategyVersionID: "v-test-1",
			Status:            "active",
		},
	}
}

func (a *rescanAdapter) GetActiveStrategyVersion(_ context.Context) (*database.StrategyVersion, error) {
	return a.activeVersion, nil
}

func (a *rescanAdapter) GetSystemState(_ context.Context) (*contracts.SystemStateDTO, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.systemState, nil
}

func (a *rescanAdapter) GetTokensForRescan(_ context.Context, q database.RescanQuery) ([]contracts.MarketDataDTO, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastQuery = q // capture for IncludeSkippedForRetry pass-through assertion
	if a.queryErr != nil {
		return nil, a.queryErr
	}
	out := make([]contracts.MarketDataDTO, len(a.queryResult))
	copy(out, a.queryResult)
	return out, nil
}

func (a *rescanAdapter) InsertMarketData(_ context.Context, dto contracts.MarketDataDTO) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.marketData = append(a.marketData, dto)
	return nil
}

func (a *rescanAdapter) InsertEvent(_ context.Context, evt database.Event) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Idempotency: duplicate event_ids silently dropped (ON CONFLICT DO NOTHING).
	for _, e := range a.events {
		if e.EventID == evt.EventID {
			return nil
		}
	}
	a.events = append(a.events, evt)
	return nil
}

func (a *rescanAdapter) eventsCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.events)
}

func (a *rescanAdapter) eventsByType(t string) []database.Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []database.Event
	for _, e := range a.events {
		if e.EventType == t {
			out = append(out, e)
		}
	}
	return out
}

// ── minimal rescan config ─────────────────────────────────────────────────────

func minRescanConfig(enabled bool) *config.Config {
	return &config.Config{
		Rescan: config.RescanConfig{
			Enabled:           enabled,
			IntervalSeconds:   60,
			MaxPerBandPerTick: 10,
			SkipOpenPositions: true,
			Eligibility: config.RescanEligibility{
				MaxHoneypotScore: func() *float64 { v := 0.5; return &v }(),
				MaxRugScore:      func() *float64 { v := 0.65; return &v }(),
				MaxBuyTaxBps:     func() *int32 { v := int32(3000); return &v }(),
				IncludePassed:    true,
			},
			Bands: []config.RescanBand{
				{Name: "15m", MinAgeSeconds: 900, MaxAgeSeconds: 1800, Priority: 80},
				{Name: "30m", MinAgeSeconds: 1800, MaxAgeSeconds: 2700, Priority: 60},
			},
			ModeOverrides: map[string]config.RescanEligibility{
				"STRICT": {
					MaxHoneypotScore: func() *float64 { v := 0.30; return &v }(),
					MaxRugScore:      func() *float64 { v := 0.50; return &v }(),
					MaxBuyTaxBps:     func() *int32 { v := int32(1500); return &v }(),
					IncludePassed:    false,
				},
				"BALANCED": {
					MaxHoneypotScore: func() *float64 { v := 0.5; return &v }(),
					MaxRugScore:      func() *float64 { v := 0.65; return &v }(),
					MaxBuyTaxBps:     func() *int32 { v := int32(3000); return &v }(),
					IncludePassed:    true,
				},
			},
		},
	}
}

func sampleDTO(tokenAddr string) contracts.MarketDataDTO {
	return contracts.MarketDataDTO{
		EventID:       "orig-" + tokenAddr,
		TraceID:       "trace-orig",
		CorrelationID: "corr-orig",
		CausationID:   "",
		VersionID:     "v1",
		Chain:         "eth",
		Market:        "eth-uniswap-v2",
		TokenAddress:  tokenAddr,
		TxHash:        "0xabc",
		IngestedAt:    time.Now().Add(-20 * time.Minute).UTC().Format(time.RFC3339Nano),
	}
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestRescanWorker_DisabledExitsOnCancel verifies that when Enabled=false the
// worker returns cleanly when the context is cancelled.
func TestRescanWorker_DisabledExitsOnCancel(t *testing.T) {
	t.Parallel()
	adapter := newRescanAdapter(nil)
	cfg := minRescanConfig(false)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := RunRescan(ctx, adapter, cfg, nil, nil)
	if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
		t.Fatalf("expected nil or ctx error, got: %v", err)
	}
	if adapter.eventsCount() != 0 {
		t.Errorf("disabled worker must emit zero events, got %d", adapter.eventsCount())
	}
}

// TestRescanWorker_SingleTickEmitsEvents verifies that a single tick emits
// one market_data_event per candidate per band.
func TestRescanWorker_SingleTickEmitsEvents(t *testing.T) {
	t.Parallel()
	dtos := []contracts.MarketDataDTO{sampleDTO("0xToken1"), sampleDTO("0xToken2")}
	adapter := newRescanAdapter(dtos)
	cfg := minRescanConfig(true)

	tickTime := time.Now()
	err := runRescanTick(context.Background(), adapter, cfg, "v-test-1", tickTime, nil)
	if err != nil {
		t.Fatalf("runRescanTick error: %v", err)
	}

	// Two bands × two tokens = 4 market_data_events expected.
	events := adapter.eventsByType("market_data_event")
	if len(events) != 4 {
		t.Errorf("expected 4 events (2 bands × 2 tokens), got %d", len(events))
	}
}

// TestRescanWorker_IdempotentOnSecondTick verifies that running the same tick
// twice produces no additional events (ON CONFLICT DO NOTHING semantics).
func TestRescanWorker_IdempotentOnSecondTick(t *testing.T) {
	t.Parallel()
	dtos := []contracts.MarketDataDTO{sampleDTO("0xToken1")}
	adapter := newRescanAdapter(dtos)
	cfg := minRescanConfig(true)

	tickTime := time.Now()
	// First tick.
	if err := runRescanTick(context.Background(), adapter, cfg, "v-test-1", tickTime, nil); err != nil {
		t.Fatalf("first tick error: %v", err)
	}
	// Second tick with same tickTime — same bucket_ts → same EventIDs.
	if err := runRescanTick(context.Background(), adapter, cfg, "v-test-1", tickTime, nil); err != nil {
		t.Fatalf("second tick error: %v", err)
	}

	// Still only 2 events (1 token × 2 bands), not 4.
	events := adapter.eventsByType("market_data_event")
	if len(events) != 2 {
		t.Errorf("expected 2 events after duplicate tick, got %d", len(events))
	}
}

// TestRescanWorker_EventHasCorrectFields verifies emitted events carry
// the expected transport tag, trace IDs, and event type.
func TestRescanWorker_EventHasCorrectFields(t *testing.T) {
	t.Parallel()
	dtos := []contracts.MarketDataDTO{sampleDTO("0xTknA")}
	adapter := newRescanAdapter(dtos)
	cfg := minRescanConfig(true)
	// Only one band for easy assertion.
	cfg.Rescan.Bands = []config.RescanBand{
		{Name: "15m", MinAgeSeconds: 900, MaxAgeSeconds: 1800, Priority: 80},
	}

	tickTime := time.Now()
	if err := runRescanTick(context.Background(), adapter, cfg, "v-42", tickTime, nil); err != nil {
		t.Fatalf("tick error: %v", err)
	}

	events := adapter.eventsByType("market_data_event")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	evt := events[0]

	if evt.VersionID != "v-42" {
		t.Errorf("VersionID: want v-42, got %s", evt.VersionID)
	}
	if evt.TraceID == "" {
		t.Error("TraceID must not be empty")
	}
	if evt.CorrelationID != evt.TraceID {
		t.Errorf("CorrelationID must equal TraceID for root events")
	}
	if evt.CausationID != nil {
		t.Errorf("CausationID must be nil for Layer 0 root events, got %v", evt.CausationID)
	}

	// Decode payload and check transport tag.
	var md contracts.MarketDataDTO
	if err := json.Unmarshal(evt.Payload, &md); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if md.Transport != "rescan_15m" {
		t.Errorf("Transport: want rescan_15m, got %s", md.Transport)
	}
	if md.Priority != 80 {
		t.Errorf("Priority: want 80, got %d", md.Priority)
	}
}

// TestRescanWorker_ModeOverride_STRICT verifies that when mode=STRICT the
// narrower eligibility thresholds from ModeOverrides["STRICT"] are applied.
func TestRescanWorker_ModeOverride_STRICT(t *testing.T) {
	t.Parallel()
	cfg := minRescanConfig(true)

	// STRICT override has max_honeypot_score=0.30.
	eligibility := resolveEligibility(cfg.Rescan, "STRICT")
	if eligibility.MaxHoneypotScore == nil || *eligibility.MaxHoneypotScore != 0.30 {
		v := float64(0)
		if eligibility.MaxHoneypotScore != nil {
			v = *eligibility.MaxHoneypotScore
		}
		t.Errorf("STRICT MaxHoneypotScore: want 0.30, got %f", v)
	}
	if eligibility.IncludePassed {
		t.Error("STRICT IncludePassed must be false")
	}
}

// TestRescanWorker_UnknownModeUsesDefault verifies that an unknown mode
// string falls back to the base eligibility (not panic).
func TestRescanWorker_UnknownModeUsesDefault(t *testing.T) {
	t.Parallel()
	cfg := minRescanConfig(true)
	eligibility := resolveEligibility(cfg.Rescan, "INVALID_MODE")
	if eligibility.MaxHoneypotScore != cfg.Rescan.Eligibility.MaxHoneypotScore {
		var got, want float64
		if eligibility.MaxHoneypotScore != nil {
			got = *eligibility.MaxHoneypotScore
		}
		if cfg.Rescan.Eligibility.MaxHoneypotScore != nil {
			want = *cfg.Rescan.Eligibility.MaxHoneypotScore
		}
		t.Errorf("fallback: want %f, got %f", want, got)
	}
}

// TestRescanWorker_PerTokenErrorDoesNotAbortBand verifies that an error
// emitting one token does not prevent other tokens from being processed.
func TestRescanWorker_PerTokenErrorDoesNotAbortBand(t *testing.T) {
	t.Parallel()
	// Two tokens; perTokenFailAdapter returns error for InsertMarketData on 0xFail.
	dtos := []contracts.MarketDataDTO{sampleDTO("0xFail"), sampleDTO("0xOk")}
	adapter := &perTokenFailAdapter{
		rescanAdapter: newRescanAdapter(dtos),
		failToken:     "0xFail",
	}
	cfg := minRescanConfig(true)
	cfg.Rescan.Bands = []config.RescanBand{
		{Name: "15m", MinAgeSeconds: 900, MaxAgeSeconds: 1800, Priority: 80},
	}

	if err := runRescanTick(context.Background(), adapter, cfg, "v-1", time.Now(), nil); err != nil {
		t.Fatalf("tick should not abort on per-token error: %v", err)
	}

	// Only 0xOk should have an event.
	events := adapter.eventsByType("market_data_event")
	for _, evt := range events {
		var md contracts.MarketDataDTO
		if err := json.Unmarshal(evt.Payload, &md); err != nil {
			continue
		}
		if md.TokenAddress == "0xFail" {
			t.Error("0xFail token should have been skipped, not emitted")
		}
	}
}

// TestRescanWorker_ClosedTriggerChannel_NoHotLoop ensures a closed trigger
// channel is detached and does not cause repeated forced rescan cycles.
func TestRescanWorker_ClosedTriggerChannel_NoHotLoop(t *testing.T) {
	t.Parallel()
	adapter := newRescanAdapter([]contracts.MarketDataDTO{sampleDTO("0xToken1")})
	cfg := minRescanConfig(true)

	triggerCh := make(chan struct{})
	close(triggerCh)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	err := RunRescan(ctx, adapter, cfg, nil, triggerCh)
	if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
		t.Fatalf("RunRescan unexpected error: %v", err)
	}

	// With default interval=60s there should be no periodic tick in 120ms.
	// A hot-loop would emit many events; expect zero after proper detachment.
	if got := adapter.eventsCount(); got != 0 {
		t.Fatalf("expected 0 events (no hot-loop), got %d", got)
	}
}

// perTokenFailAdapter wraps rescanAdapter and returns an error for
// InsertMarketData when the DTO's TokenAddress matches failToken.
type perTokenFailAdapter struct {
	*rescanAdapter
	failToken string
}

func (a *perTokenFailAdapter) InsertMarketData(_ context.Context, dto contracts.MarketDataDTO) error {
	if dto.TokenAddress == a.failToken {
		return context.DeadlineExceeded // any error
	}
	return a.rescanAdapter.InsertMarketData(context.Background(), dto)
}

// TestRescanWorker_IncludeSkippedForRetryPassedToQuery verifies that the
// IncludeSkippedForRetry config flag is propagated to the RescanQuery sent
// to the adapter.
func TestRescanWorker_IncludeSkippedForRetryPassedToQuery(t *testing.T) {
	t.Parallel()
	dtos := []contracts.MarketDataDTO{sampleDTO("0xToken1")}
	adapter := newRescanAdapter(dtos)
	cfg := minRescanConfig(true)
	cfg.Rescan.IncludeSkippedForRetry = true

	if err := runRescanTick(context.Background(), adapter, cfg, "v-test-1", time.Now(), nil); err != nil {
		t.Fatalf("runRescanTick error: %v", err)
	}

	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if !adapter.lastQuery.IncludeSkippedForRetry {
		t.Error("expected IncludeSkippedForRetry=true to be passed through to RescanQuery")
	}
}

// TestRescanWorker_IncludeSkippedForRetryDefaultFalse verifies that when the
// config flag is not set (default), IncludeSkippedForRetry is false in the query.
func TestRescanWorker_IncludeSkippedForRetryDefaultFalse(t *testing.T) {
	t.Parallel()
	dtos := []contracts.MarketDataDTO{sampleDTO("0xToken1")}
	adapter := newRescanAdapter(dtos)
	cfg := minRescanConfig(true)
	// IncludeSkippedForRetry is zero-value (false) — not explicitly set.

	if err := runRescanTick(context.Background(), adapter, cfg, "v-test-1", time.Now(), nil); err != nil {
		t.Fatalf("runRescanTick error: %v", err)
	}

	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if adapter.lastQuery.IncludeSkippedForRetry {
		t.Error("expected IncludeSkippedForRetry=false (default disabled) in RescanQuery")
	}
}
