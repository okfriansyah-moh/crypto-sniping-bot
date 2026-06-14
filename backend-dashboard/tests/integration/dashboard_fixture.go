package integration

import (
	"context"
	"sync"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
)

// dashboardFixtureDB is an in-memory adapter for operator-dashboard integration tests.
type dashboardFixtureDB struct {
	noopAdapter

	mu sync.Mutex

	events      []database.Event
	claimIdx    map[string]int
	systemState *contracts.SystemStateDTO
	strategy    *database.StrategyVersion
	halted      bool
	haltReason  string
	pipeline    *database.PipelineStats
	activity    []database.RecentEventRow
}

func newDashboardFixture() *dashboardFixtureDB {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return &dashboardFixtureDB{
		claimIdx: make(map[string]int),
		systemState: &contracts.SystemStateDTO{
			Mode:             "BALANCED",
			StateVersion:     1,
			DrawdownPct:      0,
			OpenPositions:    0,
			TotalExposureUsd: 0,
			UpdatedAt:        now,
		},
		strategy: &database.StrategyVersion{
			StrategyVersionID: "strat-integration01",
			Status:            "active",
		},
		pipeline: &database.PipelineStats{Detected: 10, DQPassed: 4, Executed: 1},
		activity: []database.RecentEventRow{
			{EventID: "evt-smoke-1", EventType: "market_data_event", Chain: "solana", CreatedAt: now},
		},
	}
}

func (d *dashboardFixtureDB) InsertEvent(_ context.Context, evt database.Event) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, existing := range d.events {
		if existing.EventID == evt.EventID {
			return nil
		}
	}
	cp := evt
	d.events = append(d.events, cp)
	return nil
}

func (d *dashboardFixtureDB) ClaimNextEvent(_ context.Context, group string, eventTypes []string) (*database.Event, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	start := d.claimIdx[group]
	for i := start; i < len(d.events); i++ {
		evt := d.events[i]
		if evt.Processed {
			continue
		}
		if !containsEventType(eventTypes, evt.EventType) {
			continue
		}
		d.claimIdx[group] = i + 1
		cp := evt
		return &cp, nil
	}
	return nil, nil
}

func (d *dashboardFixtureDB) MarkEventProcessed(_ context.Context, eventID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.events {
		if d.events[i].EventID == eventID {
			d.events[i].Processed = true
			return nil
		}
	}
	return nil
}

func (d *dashboardFixtureDB) GetActiveStrategyVersion(context.Context) (*database.StrategyVersion, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.strategy == nil {
		return nil, database.ErrNotFound
	}
	cp := *d.strategy
	return &cp, nil
}

func (d *dashboardFixtureDB) GetSystemState(context.Context) (*contracts.SystemStateDTO, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.systemState == nil {
		return nil, database.ErrNotFound
	}
	cp := *d.systemState
	return &cp, nil
}

func (d *dashboardFixtureDB) UpsertSystemState(_ context.Context, state contracts.SystemStateDTO, _ int64) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	state.StateVersion++
	d.systemState = &state
	return state.StateVersion, nil
}

func (d *dashboardFixtureDB) SetSystemHalt(_ context.Context, halt bool, reason, _ string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.halted = halt
	d.haltReason = reason
	return nil
}

func (d *dashboardFixtureDB) IsSystemHalted(context.Context) (bool, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.halted, d.haltReason, nil
}

func (d *dashboardFixtureDB) GetPipelineStats(context.Context, int) (*database.PipelineStats, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pipeline == nil {
		return &database.PipelineStats{}, nil
	}
	cp := *d.pipeline
	return &cp, nil
}

func (d *dashboardFixtureDB) ListRecentEvents(_ context.Context, _ string, limit int) ([]database.RecentEventRow, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if limit <= 0 || limit >= len(d.activity) {
		out := make([]database.RecentEventRow, len(d.activity))
		copy(out, d.activity)
		return out, nil
	}
	out := make([]database.RecentEventRow, limit)
	copy(out, d.activity[:limit])
	return out, nil
}

func (d *dashboardFixtureDB) Events() []database.Event {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]database.Event, len(d.events))
	copy(out, d.events)
	return out
}

func (d *dashboardFixtureDB) Mode() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.systemState == nil {
		return ""
	}
	return d.systemState.Mode
}

func containsEventType(types []string, t string) bool {
	for _, v := range types {
		if v == t {
			return true
		}
	}
	return false
}
