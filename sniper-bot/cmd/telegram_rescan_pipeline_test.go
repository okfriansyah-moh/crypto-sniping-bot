package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/sniper-bot/internal/telegram"
)

// ── stub adapter ─────────────────────────────────────────────────────────────

// rescanPipelineStubAdapter implements database.Adapter (via embedding) and
// optionally satisfies rescanPipelineQueryer via GetRescanPipelineStats.
type rescanPipelineStubAdapter struct {
	database.Adapter
	stats    *database.RescanPipelineStats
	statsErr error
}

func (s *rescanPipelineStubAdapter) GetRescanPipelineStats(_ context.Context, _ int) (*database.RescanPipelineStats, error) {
	return s.stats, s.statsErr
}

// noRescanPipelineAdapter embeds database.Adapter but does NOT implement
// GetRescanPipelineStats, so the type-assertion in buildRescanPipelineFn fails.
type noRescanPipelineAdapter struct {
	database.Adapter
}

// ── buildRescanPipelineFn tests ───────────────────────────────────────────────

func TestBuildRescanPipelineFn_AdapterNotSupported(t *testing.T) {
	fn := buildRescanPipelineFn(&noRescanPipelineAdapter{})
	out, err := fn(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "not supported") {
		t.Fatalf("expected 'not supported' message, got: %q", out)
	}
}

func TestBuildRescanPipelineFn_NoTokensRescanned(t *testing.T) {
	stub := &rescanPipelineStubAdapter{
		stats: &database.RescanPipelineStats{Detected: 0, WindowHours: 24},
	}
	fn := buildRescanPipelineFn(stub)
	out, err := fn(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No tokens were rescanned") {
		t.Fatalf("expected empty-state message, got: %q", out)
	}
}

func TestBuildRescanPipelineFn_WithStats_ShowsFunnel(t *testing.T) {
	stub := &rescanPipelineStubAdapter{
		stats: &database.RescanPipelineStats{
			Detected:       50,
			DQPassed:       40,
			FeatureReady:   35,
			EdgeDetected:   20,
			Validated:      15,
			Selected:       10,
			Executed:       8,
			PositionOpen:   8,
			PositionClosed: 5,
			Evaluated:      3,
			Rejected:       10,
			Failed:         0,
			ByBand: map[string]int64{
				"15m": 20,
				"30m": 18,
				"45m": 12,
			},
			TotalEmitted: 50,
			WindowHours:  24,
		},
	}
	fn := buildRescanPipelineFn(stub)
	out, err := fn(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Funnel header
	if !strings.Contains(out, "Rescan Pipeline Funnel") {
		t.Errorf("expected funnel header, got: %q", out)
	}
	// Key funnel rows
	if !strings.Contains(out, "DETECTED") {
		t.Errorf("expected DETECTED row, got: %q", out)
	}
	if !strings.Contains(out, "DQ_PASSED") {
		t.Errorf("expected DQ_PASSED row, got: %q", out)
	}
	if !strings.Contains(out, "REJECTED") {
		t.Errorf("expected REJECTED row, got: %q", out)
	}
	// Per-band breakdown
	if !strings.Contains(out, "Emissions by band") {
		t.Errorf("expected band breakdown, got: %q", out)
	}
	if !strings.Contains(out, "15m") {
		t.Errorf("expected band '15m', got: %q", out)
	}
}

func TestBuildRescanPipelineFn_WithFailures_ShowsBreakdown(t *testing.T) {
	stub := &rescanPipelineStubAdapter{
		stats: &database.RescanPipelineStats{
			Detected:             10,
			DQPassed:             8,
			Selected:             4,
			Failed:               3,
			FailedAtSelected:     2,
			FailedAtExecuted:     1,
			FailedAtPositionOpen: 0,
			ByBand:               map[string]int64{"30m": 10},
			TotalEmitted:         10,
			WindowHours:          24,
		},
	}
	fn := buildRescanPipelineFn(stub)
	out, err := fn(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "exec fail") {
		t.Errorf("expected failure breakdown, got: %q", out)
	}
	if !strings.Contains(out, "pos-open") {
		t.Errorf("expected pos-open breakdown, got: %q", out)
	}
}

func TestBuildRescanPipelineFn_StatsError_ReturnsError(t *testing.T) {
	stub := &rescanPipelineStubAdapter{
		statsErr: errors.New("db unavailable"),
	}
	fn := buildRescanPipelineFn(stub)
	_, err := fn(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── ParseCommand tests ────────────────────────────────────────────────────────

func TestParseCommand_RescanPipeline_Accepted(t *testing.T) {
	req, err := telegram.ParseCommand("/rescan_pipeline", "123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Type != telegram.CmdRescanPipeline {
		t.Errorf("expected CmdRescanPipeline, got %q", req.Type)
	}
}

func TestParseCommand_RescanPipeline_CaseNormalised(t *testing.T) {
	// commands are lower-cased during parse
	req, err := telegram.ParseCommand("/RESCAN_PIPELINE", "123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Type != telegram.CmdRescanPipeline {
		t.Errorf("expected CmdRescanPipeline, got %q", req.Type)
	}
}

func TestParseCommand_RescanPipeline_IsReadOnly_CanRunWithoutAllowlist(t *testing.T) {
	// /rescan_pipeline is read-only: it must succeed even when AllowedUserIDs is
	// unconfigured (the handler logs a warning but does not block read-only commands).
	h := telegram.NewHandler(telegram.HandlerOptions{
		RescanPipelineFn: func(_ context.Context) (string, error) {
			return "ok", nil
		},
	})
	req, _ := telegram.ParseCommand("/rescan_pipeline", "anyone")
	result, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("read-only command must not be blocked: %v", err)
	}
	if result.Destructive {
		t.Error("/rescan_pipeline must not be flagged as destructive")
	}
}
