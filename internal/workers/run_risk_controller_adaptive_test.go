package workers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
)

// ── adaptive test adapter ─────────────────────────────────────────────────────

// adaptiveAdapter is a focused mock for the adaptive risk-appetite tests.
// It records every UpsertSystemState and InsertEvent call so tests can assert
// on the resulting transitions and emitted system_events.
type adaptiveAdapter struct {
	*stubAdapter

	mu    sync.Mutex
	state contracts.SystemStateDTO

	upserts []contracts.SystemStateDTO
	events  []database.Event
}

func newAdaptiveAdapter(initial contracts.SystemStateDTO) *adaptiveAdapter {
	return &adaptiveAdapter{
		stubAdapter: &stubAdapter{},
		state:       initial,
	}
}

func (a *adaptiveAdapter) GetSystemState(_ context.Context) (*contracts.SystemStateDTO, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cp := a.state
	return &cp, nil
}

func (a *adaptiveAdapter) UpsertSystemState(_ context.Context, s contracts.SystemStateDTO, _ int64) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state = s
	a.state.StateVersion++
	a.upserts = append(a.upserts, s)
	return a.state.StateVersion, nil
}

func (a *adaptiveAdapter) InsertEvent(_ context.Context, evt database.Event) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, evt)
	return nil
}

// GetActiveStrategyVersion returns ErrNotFound — adaptive logic must not depend on it.
func (a *adaptiveAdapter) GetActiveStrategyVersion(_ context.Context) (*database.StrategyVersion, error) {
	return nil, database.ErrNotFound
}

func (a *adaptiveAdapter) lastUpsert() (contracts.SystemStateDTO, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.upserts) == 0 {
		return contracts.SystemStateDTO{}, false
	}
	return a.upserts[len(a.upserts)-1], true
}

func (a *adaptiveAdapter) hasEventWithSubstr(s string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, e := range a.events {
		if strings.Contains(string(e.Payload), s) {
			return true
		}
	}
	return false
}

// ── helpers ───────────────────────────────────────────────────────────────────

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func adaptiveCfg() *config.Config {
	return &config.Config{
		ModeAdaptive: config.ModeAdaptiveConfig{
			Enabled:              true,
			AdaptiveWindowSec:    1800,
			StarvationTriggerSec: 1800,
			RugRateAutoDowngrade: 0.15,
			FPRateAutoDowngrade:  0.25,
			TransitionWindowSec:  3600,
			DefaultStartupMode:   "BALANCED",
		},
	}
}

// fixedNow gives a deterministic UTC clock.
func fixedNow() time.Time {
	return time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestAdaptive_ColdStart_DefaultsToBalanced(t *testing.T) {
	// Arrange — empty Mode simulating a fresh DB row.
	adapter := newAdaptiveAdapter(contracts.SystemStateDTO{Mode: ""})
	cfg := adaptiveCfg()

	// Act — cold-start path lives in runRiskCheck.
	if _, err := runRiskCheck(context.Background(), adapter, cfg, discardLogger(), 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert
	got, ok := adapter.lastUpsert()
	if !ok {
		t.Fatal("expected an upsert from cold-start path")
	}
	if got.Mode != "BALANCED" {
		t.Errorf("Mode: got %q want BALANCED", got.Mode)
	}
	if got.LastTransitionReason != "cold_start_default" {
		t.Errorf("LastTransitionReason: got %q want cold_start_default", got.LastTransitionReason)
	}
	if !adapter.hasEventWithSubstr("cold_start_default") {
		t.Error("expected system_event payload to contain cold_start_default")
	}
}

func TestAdaptive_StarvationFromStrict_UpgradesToBalanced(t *testing.T) {
	// Arrange — STRICT, last edge 2h ago, no rug spike.
	adapter := newAdaptiveAdapter(contracts.SystemStateDTO{Mode: "STRICT"})
	cfg := adaptiveCfg()
	last := &adaptiveTransition{}
	now := fixedNow()
	lastEdge := now.Add(-2 * time.Hour)

	// Act
	state, _ := adapter.GetSystemState(context.Background())
	newVersion, err := runAdaptiveRiskAppetite(
		context.Background(), adapter, cfg, discardLogger(),
		state, 0, last, now, lastEdge, 0.0, 0.0,
	)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newVersion == 0 {
		t.Fatal("expected a transition; got 0")
	}
	got, _ := adapter.lastUpsert()
	if got.Mode != "BALANCED" {
		t.Errorf("Mode: got %q want BALANCED", got.Mode)
	}
	if got.LastTransitionReason != "starvation" {
		t.Errorf("Reason: got %q want starvation", got.LastTransitionReason)
	}
}

func TestAdaptive_StarvationFromBalanced_UpgradesToExploration(t *testing.T) {
	adapter := newAdaptiveAdapter(contracts.SystemStateDTO{Mode: "BALANCED"})
	cfg := adaptiveCfg()
	last := &adaptiveTransition{}
	now := fixedNow()

	state, _ := adapter.GetSystemState(context.Background())
	if _, err := runAdaptiveRiskAppetite(
		context.Background(), adapter, cfg, discardLogger(),
		state, 0, last, now, now.Add(-2*time.Hour), 0.0, 0.0,
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := adapter.lastUpsert()
	if got.Mode != "EXPLORATION" {
		t.Errorf("Mode: got %q want EXPLORATION", got.Mode)
	}
	if got.LastTransitionReason != "starvation" {
		t.Errorf("Reason: got %q want starvation", got.LastTransitionReason)
	}
}

func TestAdaptive_StarvationInExploration_EmitsCriticalAlert(t *testing.T) {
	adapter := newAdaptiveAdapter(contracts.SystemStateDTO{Mode: "EXPLORATION"})
	cfg := adaptiveCfg()
	last := &adaptiveTransition{}
	now := fixedNow()

	state, _ := adapter.GetSystemState(context.Background())
	newVersion, err := runAdaptiveRiskAppetite(
		context.Background(), adapter, cfg, discardLogger(),
		state, 0, last, now, now.Add(-2*time.Hour), 0.0, 0.0,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newVersion != 0 {
		t.Errorf("expected no transition; got version %d", newVersion)
	}
	if _, ok := adapter.lastUpsert(); ok {
		t.Error("expected no upsert; got one")
	}
	if !adapter.hasEventWithSubstr("starvation_critical") {
		t.Error("expected starvation_critical alert event; not emitted")
	}
}

func TestAdaptive_RugRateHigh_DowngradesOneNotch(t *testing.T) {
	// Arrange — BALANCED with high rug rate. fp_rate below threshold.
	adapter := newAdaptiveAdapter(contracts.SystemStateDTO{Mode: "BALANCED"})
	cfg := adaptiveCfg()
	last := &adaptiveTransition{}
	now := fixedNow()

	state, _ := adapter.GetSystemState(context.Background())
	if _, err := runAdaptiveRiskAppetite(
		context.Background(), adapter, cfg, discardLogger(),
		state, 0, last, now, now, /* lastEdge fresh */
		0.30, 0.10, /* rug, fp */
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, ok := adapter.lastUpsert()
	if !ok {
		t.Fatal("expected a downgrade upsert")
	}
	if got.Mode != "STRICT" {
		t.Errorf("Mode: got %q want STRICT", got.Mode)
	}
	if got.LastTransitionReason != "high_rug_rate" {
		t.Errorf("Reason: got %q want high_rug_rate", got.LastTransitionReason)
	}
}

func TestAdaptive_BoundedOnePerWindow(t *testing.T) {
	adapter := newAdaptiveAdapter(contracts.SystemStateDTO{Mode: "STRICT"})
	cfg := adaptiveCfg()
	last := &adaptiveTransition{}
	now := fixedNow()
	state, _ := adapter.GetSystemState(context.Background())

	// First call applies the transition.
	if _, err := runAdaptiveRiskAppetite(
		context.Background(), adapter, cfg, discardLogger(),
		state, 0, last, now, now.Add(-2*time.Hour), 0.0, 0.0,
	); err != nil {
		t.Fatalf("first call: %v", err)
	}
	first := len(adapter.upserts)

	// Re-read state after transition; second call within same window must no-op.
	state2, _ := adapter.GetSystemState(context.Background())
	if _, err := runAdaptiveRiskAppetite(
		context.Background(), adapter, cfg, discardLogger(),
		state2, 0, last, now.Add(1*time.Minute), now.Add(-2*time.Hour), 0.0, 0.0,
	); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(adapter.upserts) != first {
		t.Errorf("expected no additional upsert in same window; got %d -> %d",
			first, len(adapter.upserts))
	}
}

func TestAdaptive_RespectsManualOverride(t *testing.T) {
	now := fixedNow()
	adapter := newAdaptiveAdapter(contracts.SystemStateDTO{
		Mode:                 "STRICT",
		LastTransitionReason: "manual_telegram",
		UpdatedAt:            now.Format(time.RFC3339Nano),
	})
	cfg := adaptiveCfg()
	last := &adaptiveTransition{}
	state, _ := adapter.GetSystemState(context.Background())

	newVersion, err := runAdaptiveRiskAppetite(
		context.Background(), adapter, cfg, discardLogger(),
		state, 0, last, now, now.Add(-3*time.Hour), 0.0, 0.0,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newVersion != 0 {
		t.Errorf("expected adaptive to skip; got version %d", newVersion)
	}
	if _, ok := adapter.lastUpsert(); ok {
		t.Error("expected no upsert when manual override is active in window")
	}
}

func TestAdaptive_DegradedModeNotTouched(t *testing.T) {
	adapter := newAdaptiveAdapter(contracts.SystemStateDTO{Mode: "DEGRADED"})
	cfg := adaptiveCfg()
	last := &adaptiveTransition{}
	now := fixedNow()

	state, _ := adapter.GetSystemState(context.Background())
	newVersion, err := runAdaptiveRiskAppetite(
		context.Background(), adapter, cfg, discardLogger(),
		state, 0, last, now, now.Add(-3*time.Hour), 0.5, 0.5,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newVersion != 0 {
		t.Errorf("expected no transition in DEGRADED; got version %d", newVersion)
	}
	if _, ok := adapter.lastUpsert(); ok {
		t.Error("expected no upsert in DEGRADED mode")
	}
	if len(adapter.events) != 0 {
		t.Errorf("expected no system_events in DEGRADED mode; got %d", len(adapter.events))
	}
}

// dqStatsAdaptiveAdapter overrides GetAdaptiveDQStats to inject the rug-rate
// numerator/denominator that the Tick path uses to drive the safety downgrade.
// It also stubs GetLastEventTimestamp to a recent time so the starvation path
// does NOT fire — this isolates the assertion to the rug-rate code path.
type dqStatsAdaptiveAdapter struct {
	*adaptiveAdapter
	total   int
	rejects int
	now     time.Time
}

func (a *dqStatsAdaptiveAdapter) GetAdaptiveDQStats(_ context.Context, _ int) (int, int, error) {
	return a.total, a.rejects, nil
}
func (a *dqStatsAdaptiveAdapter) GetLastEventTimestamp(_ context.Context, _ string) (time.Time, error) {
	return a.now.Add(-30 * time.Second), nil
}

// TestAdaptive_UsesGetAdaptiveDQStats_ForRugRate proves the Tick path reads
// rug-rate from the adapter (no longer hardcoded 0.0) and downgrades when
// rug_rate > RugRateAutoDowngrade.
func TestAdaptive_UsesGetAdaptiveDQStats_ForRugRate(t *testing.T) {
	now := fixedNow()
	base := newAdaptiveAdapter(contracts.SystemStateDTO{
		Mode:         modeBalanced,
		StateVersion: 1,
		UpdatedAt:    now.Add(-2 * time.Hour).Format(time.RFC3339Nano),
	})
	adapter := &dqStatsAdaptiveAdapter{
		adaptiveAdapter: base,
		total:           10,
		rejects:         3, // rugRate = 0.3 > threshold 0.15
		now:             now,
	}

	cfg := adaptiveCfg()

	if _, err := runAdaptiveRiskAppetiteTick(
		context.Background(), adapter, cfg, discardLogger(), 1, nil, now,
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, ok := base.lastUpsert()
	if !ok {
		t.Fatal("expected an upsert from adaptive Tick path")
	}
	if got.Mode != modeStrict {
		t.Errorf("expected downgrade to STRICT; got %q", got.Mode)
	}
	if got.LastTransitionReason != "high_rug_rate" {
		t.Errorf("expected reason=high_rug_rate; got %q", got.LastTransitionReason)
	}
}

// TestAdaptive_GetAdaptiveDQStats_ZeroTotal_NoDowngrade proves zero total
// decisions yields rug_rate=0.0 (no division-by-zero, no spurious transition).
func TestAdaptive_GetAdaptiveDQStats_ZeroTotal_NoDowngrade(t *testing.T) {
	now := fixedNow()
	base := newAdaptiveAdapter(contracts.SystemStateDTO{
		Mode:         modeBalanced,
		StateVersion: 1,
		UpdatedAt:    now.Add(-2 * time.Hour).Format(time.RFC3339Nano),
	})
	adapter := &dqStatsAdaptiveAdapter{
		adaptiveAdapter: base,
		total:           0,
		rejects:         0,
		now:             now,
	}

	cfg := adaptiveCfg()

	if _, err := runAdaptiveRiskAppetiteTick(
		context.Background(), adapter, cfg, discardLogger(), 1, nil, now,
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, ok := base.lastUpsert(); ok {
		t.Errorf("expected no upsert with zero DQ decisions; got mode=%q reason=%q",
			got.Mode, got.LastTransitionReason)
	}
}

// silence unused import warnings in case build tags trim the file.
var _ = errors.Is
