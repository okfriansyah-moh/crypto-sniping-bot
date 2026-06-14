package positions_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/backend-dashboard/internal/api/positions"
)

type positionsStubDB struct {
	database.Adapter
	open []contracts.PositionStateDTO
	err  error
}

func (s *positionsStubDB) GetOpenPositions(context.Context) ([]contracts.PositionStateDTO, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.open, nil
}

func TestHandler_ReturnsPositionRowsJSON(t *testing.T) {
	stub := &positionsStubDB{
		open: []contracts.PositionStateDTO{
			{
				PositionID:   "pos-1",
				TokenAddress: "So111",
				Chain:        "solana",
				EntrySizeUsd: 25,
				OpenedAt:     "2026-06-13T12:00:00Z",
				TraceID:      "trace-abc123",
				VersionID:    "strat-v1",
			},
		},
	}
	h := positions.NewHandler(stub)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/positions", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var rows []contracts.PositionRowDTO
	if err := json.NewDecoder(rec.Body).Decode(&rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len = %d, want 1", len(rows))
	}
	if rows[0].TraceID != "trace-abc123" {
		t.Errorf("TraceID = %q", rows[0].TraceID)
	}
	if rows[0].StrategyVersionID != "strat-v1" {
		t.Errorf("StrategyVersionID = %q", rows[0].StrategyVersionID)
	}
}

func TestHandler_ChainFilter(t *testing.T) {
	stub := &positionsStubDB{
		open: []contracts.PositionStateDTO{
			{PositionID: "p1", Chain: "solana", OpenedAt: "2026-06-13T12:00:00Z"},
			{PositionID: "p2", Chain: "eth", OpenedAt: "2026-06-13T12:00:00Z"},
		},
	}
	h := positions.NewHandler(stub)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/positions?chain=solana", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var rows []contracts.PositionRowDTO
	if err := json.NewDecoder(rec.Body).Decode(&rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 || rows[0].Chain != "solana" {
		t.Fatalf("got %+v, want single solana row", rows)
	}
}

func TestHandler_EmptyReturnsJSONArray(t *testing.T) {
	h := positions.NewHandler(&positionsStubDB{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/positions", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Body.String() != "[]\n" && rec.Body.String() != "[]" {
		t.Errorf("body = %q, want empty JSON array", rec.Body.String())
	}
}

func TestHandler_AdapterErrorReturns500(t *testing.T) {
	h := positions.NewHandler(&positionsStubDB{err: context.Canceled})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/positions", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
