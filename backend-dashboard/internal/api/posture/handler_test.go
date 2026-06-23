package posture_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/backend-dashboard/internal/api/posture"
)

type postureStubDB struct {
	database.Adapter
	state    *contracts.SystemStateDTO
	pipeline *database.PipelineStats
	dq       *database.DQBreakdown
	shadow   *database.ShadowGateStats
}

func (s *postureStubDB) GetSystemState(context.Context) (*contracts.SystemStateDTO, error) {
	return s.state, nil
}

func (s *postureStubDB) GetPipelineStats(context.Context, int) (*database.PipelineStats, error) {
	if s.pipeline == nil {
		return &database.PipelineStats{}, nil
	}
	return s.pipeline, nil
}

func (s *postureStubDB) GetDQBreakdown(context.Context, int, string) (*database.DQBreakdown, error) {
	if s.dq == nil {
		return &database.DQBreakdown{}, nil
	}
	return s.dq, nil
}

func (s *postureStubDB) GetShadowGateStats(context.Context, int) (*database.ShadowGateStats, error) {
	if s.shadow == nil {
		return &database.ShadowGateStats{}, nil
	}
	return s.shadow, nil
}

func TestHandler_ReturnsPostureJSON(t *testing.T) {
	stub := &postureStubDB{
		state: &contracts.SystemStateDTO{
			Mode:      "BALANCED",
			UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
		pipeline: &database.PipelineStats{Detected: 10, Evaluated: 1},
		dq:       &database.DQBreakdown{PassCount: 2},
		shadow: &database.ShadowGateStats{
			TradeCount:      35,
			AggregatePnlBps: 50,
		},
	}
	cfg := &config.Config{
		Execution: config.ExecutionConfig{
			Mode: "shadow",
			ShadowGate: config.ShadowGateConfig{
				MinTrades:          30,
				MinWindowDays:      14,
				MinAggregatePnlBps: 0,
			},
		},
		Solana: config.SolanaConfig{
			Ingestion: config.SolanaIngestionConfig{Delivery: "hybrid"},
		},
	}

	h := posture.NewHandler(stub, cfg, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/posture", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out contracts.FortressPostureDTO
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Mode != "BALANCED" {
		t.Errorf("Mode = %q", out.Mode)
	}
	if out.IngestionDelivery != "hybrid" {
		t.Errorf("IngestionDelivery = %q", out.IngestionDelivery)
	}
	if out.ReadinessState == "" {
		t.Fatal("readiness_state required")
	}
}
