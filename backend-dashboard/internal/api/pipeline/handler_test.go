package pipeline_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/backend-dashboard/internal/api/pipeline"
)

type pipelineStubDB struct {
	database.Adapter
	stats *database.PipelineStats
	err   error
}

func (s *pipelineStubDB) GetPipelineStats(context.Context, int) (*database.PipelineStats, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.stats == nil {
		return &database.PipelineStats{}, nil
	}
	return s.stats, nil
}

func (s *pipelineStubDB) GetProbePendingStats(context.Context) (*database.ProbePendingStats, error) {
	return &database.ProbePendingStats{}, nil
}

func TestHandler_ReturnsPipelineJSON(t *testing.T) {
	stub := &pipelineStubDB{
		stats: &database.PipelineStats{
			Detected: 50,
			DQPassed: 20,
			Executed: 3,
		},
	}
	h := pipeline.NewHandler(stub)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipeline", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var out contracts.PipelineStatsResponseDTO
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.WindowHours != 24 {
		t.Errorf("WindowHours = %d, want default 24", out.WindowHours)
	}
	if out.Funnel.Detected != 50 || out.Funnel.DQPassed != 20 {
		t.Errorf("funnel = %+v", out.Funnel)
	}
	if out.LayerHeartbeats == nil {
		t.Fatal("layer_heartbeats must be non-nil")
	}
}

func TestHandler_WindowHoursClamped(t *testing.T) {
	stub := &pipelineStubDB{stats: &database.PipelineStats{Detected: 1}}
	h := pipeline.NewHandler(stub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipeline?window_hours=999", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var out contracts.PipelineStatsResponseDTO
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.WindowHours != 168 {
		t.Errorf("WindowHours = %d, want max 168", out.WindowHours)
	}
}

func TestHandler_AdapterErrorReturns500(t *testing.T) {
	h := pipeline.NewHandler(&pipelineStubDB{err: context.Canceled})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pipeline", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
