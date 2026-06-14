package pnl_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/backend-dashboard/internal/api/pnl"
)

type pnlStubDB struct {
	database.Adapter
	closed   []contracts.PositionStateDTO
	drawdown float64
	err      error
}

func (s *pnlStubDB) ComputeDrawdown(context.Context, int) (float64, error) {
	if s.err != nil {
		return 0, s.err
	}
	return s.drawdown, nil
}

func (s *pnlStubDB) GetOpenPositions(context.Context) ([]contracts.PositionStateDTO, error) {
	if s.err != nil {
		return nil, s.err
	}
	return nil, nil
}

func (s *pnlStubDB) GetClosedPositions(context.Context, int) ([]contracts.PositionStateDTO, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.closed, nil
}

func TestHandler_ReturnsPnLJSON(t *testing.T) {
	stub := &pnlStubDB{
		drawdown: 0.05,
		closed: []contracts.PositionStateDTO{
			{PnlUsd: 10},
			{PnlUsd: -4},
		},
	}
	h := pnl.NewHandler(stub)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pnl", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var out contracts.PnLSummaryDTO
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.LookbackHours != 24 {
		t.Errorf("LookbackHours = %d, want 24", out.LookbackHours)
	}
	if out.RealizedPnLUsd != 6 {
		t.Errorf("RealizedPnLUsd = %v, want 6", out.RealizedPnLUsd)
	}
	if out.DrawdownPct != 0.05 {
		t.Errorf("DrawdownPct = %v", out.DrawdownPct)
	}
}

func TestHandler_WindowHoursDefaultAndMax(t *testing.T) {
	stub := &pnlStubDB{}
	h := pnl.NewHandler(stub)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pnl?window_hours=48", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out contracts.PnLSummaryDTO
	_ = json.NewDecoder(rec.Body).Decode(&out)
	if out.LookbackHours != 48 {
		t.Errorf("LookbackHours = %d, want 48", out.LookbackHours)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/pnl?window_hours=500", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	_ = json.NewDecoder(rec.Body).Decode(&out)
	if out.LookbackHours != 168 {
		t.Errorf("LookbackHours = %d, want max 168", out.LookbackHours)
	}
}

func TestHandler_AdapterErrorReturns500(t *testing.T) {
	h := pnl.NewHandler(&pnlStubDB{err: context.Canceled})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pnl", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
