package activity_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/backend-dashboard/internal/api/activity"
)

type activityStubDB struct {
	database.Adapter
	rows []database.RecentEventRow
	err  error
}

func (s *activityStubDB) ListRecentEvents(_ context.Context, chain string, limit int) ([]database.RecentEventRow, error) {
	if s.err != nil {
		return nil, s.err
	}
	_ = chain
	_ = limit
	return s.rows, nil
}

func TestHandler_ReturnsActivityJSON(t *testing.T) {
	stub := &activityStubDB{
		rows: []database.RecentEventRow{
			{
				EventID:   "evt-1",
				EventType: "position_opened",
				Chain:     "solana",
				CreatedAt: "2026-06-13T12:00:00Z",
			},
		},
	}
	h := activity.NewHandler(stub, 50)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/activity", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var rows []contracts.ActivityEventDTO
	if err := json.NewDecoder(rec.Body).Decode(&rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 || rows[0].EventType != "position_opened" {
		t.Fatalf("got %+v", rows)
	}
}

func TestHandler_EmptyReturnsJSONArray(t *testing.T) {
	h := activity.NewHandler(&activityStubDB{}, 50)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/activity", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var rows []contracts.ActivityEventDTO
	if err := json.NewDecoder(rec.Body).Decode(&rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("want empty array, got %d", len(rows))
	}
}

func TestHandler_AdapterErrorReturns500(t *testing.T) {
	h := activity.NewHandler(&activityStubDB{err: context.Canceled}, 50)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/activity", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
