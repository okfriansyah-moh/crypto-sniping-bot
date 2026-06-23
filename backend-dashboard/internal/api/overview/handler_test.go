package overview_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/backend-dashboard/internal/api/overview"
)

type handlerStubDB struct {
	database.Adapter
	state    *contracts.SystemStateDTO
	strategy *database.StrategyVersion
	closed   []contracts.PositionStateDTO
	drawdown float64
}

func (s *handlerStubDB) GetSystemState(context.Context) (*contracts.SystemStateDTO, error) {
	return s.state, nil
}

func (s *handlerStubDB) GetActiveStrategyVersion(context.Context) (*database.StrategyVersion, error) {
	return s.strategy, nil
}

func (s *handlerStubDB) IsSystemHalted(context.Context) (bool, string, error) {
	return false, "", nil
}

func (s *handlerStubDB) ComputeDrawdown(context.Context, int) (float64, error) {
	return s.drawdown, nil
}

func (s *handlerStubDB) GetOpenPositions(context.Context) ([]contracts.PositionStateDTO, error) {
	return nil, nil
}

func (s *handlerStubDB) GetClosedPositions(context.Context, int) ([]contracts.PositionStateDTO, error) {
	return s.closed, nil
}

func (s *handlerStubDB) GetShadowGateStats(context.Context, int) (*database.ShadowGateStats, error) {
	return &database.ShadowGateStats{}, nil
}

func (s *handlerStubDB) GetPipelineStats(context.Context, int) (*database.PipelineStats, error) {
	return &database.PipelineStats{}, nil
}

func (s *handlerStubDB) GetDQBreakdown(context.Context, int, string) (*database.DQBreakdown, error) {
	return &database.DQBreakdown{}, nil
}

func TestHandler_ReturnsOverviewJSON(t *testing.T) {
	now := time.Now().UTC()
	stub := &handlerStubDB{
		state: &contracts.SystemStateDTO{
			Mode:             "BALANCED",
			DrawdownPct:      0.02,
			OpenPositions:    1,
			TotalExposureUsd: 10,
			UpdatedAt:        now.Format(time.RFC3339Nano),
		},
		strategy: &database.StrategyVersion{StrategyVersionID: "strat0000000000001"},
		closed: []contracts.PositionStateDTO{
			{PnlUsd: 2},
			{PnlUsd: -1},
		},
	}
	cfg := &config.Config{
		Execution: config.ExecutionConfig{Mode: "shadow"},
		Capital:   config.CapitalConfig{MaxTotalExposureUsd: 100},
	}

	h := overview.NewHandler(stub, cfg, now.Add(-time.Hour), "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want json", ct)
	}

	var out contracts.OverviewResponseDTO
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Mode != "BALANCED" {
		t.Errorf("Mode = %q, want BALANCED", out.Mode)
	}
	if out.ExecutionMode != "shadow" {
		t.Errorf("ExecutionMode = %q, want shadow", out.ExecutionMode)
	}
	if out.StrategyVersionID != "strat0000000000001" {
		t.Errorf("StrategyVersionID = %q", out.StrategyVersionID)
	}
	if out.ChainStatuses == nil {
		t.Fatal("chain_statuses must be non-nil JSON array")
	}
}

func TestHandler_ChainQueryFiltersChainStatuses(t *testing.T) {
	now := time.Now().UTC()
	stub := &handlerStubDB{
		state: &contracts.SystemStateDTO{
			Mode:      "BALANCED",
			UpdatedAt: now.Format(time.RFC3339Nano),
		},
	}
	cfg := &config.Config{Execution: config.ExecutionConfig{Mode: "shadow"}}

	h := overview.NewHandler(stub, cfg, now, "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview?chain=solana", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var out contracts.OverviewResponseDTO
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.ChainStatuses) != 1 || out.ChainStatuses[0].Chain != "solana" {
		t.Fatalf("expected single solana chain_status, got %+v", out.ChainStatuses)
	}
}

func TestHandler_SystemStateErrorReturns500(t *testing.T) {
	stub := &handlerStubDB{state: nil}
	h := overview.NewHandler(stub, &config.Config{}, time.Now(), "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/overview", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	h := overview.NewHandler(&handlerStubDB{}, nil, time.Now(), "")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/overview", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
