package dq_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/backend-dashboard/internal/api/dq"
)

type dqStubDB struct {
	database.Adapter
	breakdown *database.DQBreakdown
	err       error
}

func (s *dqStubDB) GetDQBreakdown(context.Context, int, string) (*database.DQBreakdown, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.breakdown, nil
}

func TestHandler_ReturnsDQBreakdownJSON(t *testing.T) {
	stub := &dqStubDB{
		breakdown: &database.DQBreakdown{
			WindowHours:    24,
			TotalDecisions: 100,
			PassCount:      40,
			RejectCount:    50,
			PassRatePct:    40,
			TopRejectReasons: []database.DQRejectReasonCount{
				{Reason: "serial_launcher", Count: 20},
			},
		},
	}
	h := dq.NewHandler(stub)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dq", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var out contracts.DQBreakdownResponseDTO
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.WindowHours != 24 {
		t.Errorf("WindowHours = %d, want 24", out.WindowHours)
	}
	if out.TotalDecisions != 100 || out.PassCount != 40 {
		t.Errorf("breakdown = %+v", out)
	}
	if len(out.TopRejectReasons) != 1 || out.TopRejectReasons[0].Reason != "serial_launcher" {
		t.Errorf("top rejects = %+v", out.TopRejectReasons)
	}
}

func TestHandler_AdapterErrorReturns500(t *testing.T) {
	h := dq.NewHandler(&dqStubDB{err: context.Canceled})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dq", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
