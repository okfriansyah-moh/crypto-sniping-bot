package check_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-sniping-bot/internal/modules/health/feature/check"
)

// ── Handler.Handle ────────────────────────────────────────────────────────────

func TestHandler_Handle_Returns200(t *testing.T) {
	// Arrange
	svc := check.NewService()
	h := check.NewHandler(svc)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	// Act
	h.Handle(rec, req)

	// Assert
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestHandler_Handle_ContentTypeJSON(t *testing.T) {
	// Arrange
	svc := check.NewService()
	h := check.NewHandler(svc)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	// Act
	h.Handle(rec, req)

	// Assert
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
}

func TestHandler_Handle_BodyDecodesCorrectly(t *testing.T) {
	// Arrange
	svc := check.NewService()
	h := check.NewHandler(svc)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	// Act
	h.Handle(rec, req)

	// Assert
	var resp check.Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode JSON body: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("expected status 'ok', got %q", resp.Status)
	}
	if resp.Version == "" {
		t.Error("expected non-empty Version in response")
	}
}

func TestHandler_Handle_DifferentRequestsMirrorSameResult(t *testing.T) {
	// Two identical GET requests must yield identical (deterministic) responses.
	// Arrange
	svc := check.NewService()
	h := check.NewHandler(svc)

	call := func() check.Response {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rec := httptest.NewRecorder()
		h.Handle(rec, req)
		var resp check.Response
		_ = json.NewDecoder(rec.Body).Decode(&resp)
		return resp
	}

	// Act
	r1 := call()
	r2 := call()

	// Assert
	if r1.Status != r2.Status || r1.Version != r2.Version {
		t.Errorf("handler non-deterministic: %+v vs %+v", r1, r2)
	}
}
