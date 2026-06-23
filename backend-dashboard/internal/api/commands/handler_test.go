package commands_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crypto-sniping-bot/backend-dashboard/internal/api/commands"
	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
)

type commandsStubDB struct {
	database.Adapter
	events      []database.Event
	insertErr   error
	strategyVer *database.StrategyVersion
}

func (s *commandsStubDB) InsertEvent(_ context.Context, evt database.Event) error {
	if s.insertErr != nil {
		return s.insertErr
	}
	s.events = append(s.events, evt)
	return nil
}

func (s *commandsStubDB) GetActiveStrategyVersion(_ context.Context) (*database.StrategyVersion, error) {
	if s.strategyVer != nil {
		return s.strategyVer, nil
	}
	return &database.StrategyVersion{StrategyVersionID: "sv-test-1"}, nil
}

func withAllowedOperators(t *testing.T, ids ...string) {
	t.Helper()
	t.Setenv("DASHBOARD_ALLOWED_OPERATORS", strings.Join(ids, ","))
}

func TestHandler_ModeCommandEmitsEvent(t *testing.T) {
	withAllowedOperators(t, "op-1")
	stub := &commandsStubDB{}
	store := commands.NewPendingStore(60 * time.Second)
	h := commands.NewHandler(stub, &config.DashboardConfig{}, store)

	body := `{"command_type":"mode","issuer_id":"op-1","args":{"mode":"BALANCED"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/commands", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp commands.Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "accepted" || resp.CommandID == "" {
		t.Fatalf("got %+v", resp)
	}
	if len(stub.events) != 1 {
		t.Fatalf("events = %d, want 1", len(stub.events))
	}
	evt := stub.events[0]
	if evt.EventType != contracts.OperatorCommandEventType {
		t.Fatalf("event type = %q", evt.EventType)
	}
	if evt.EventID != resp.CommandID {
		t.Fatalf("event id %q != command id %q", evt.EventID, resp.CommandID)
	}
	if evt.Consumer != "operator_command_worker" {
		t.Fatalf("consumer = %q", evt.Consumer)
	}
}

func TestHandler_AllowlistFailClosed(t *testing.T) {
	t.Setenv("DASHBOARD_ALLOWED_OPERATORS", "")
	h := commands.NewHandler(&commandsStubDB{}, &config.DashboardConfig{}, commands.NewPendingStore(60*time.Second))
	body := `{"command_type":"mode","issuer_id":"op-1","args":{"mode":"BALANCED"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/commands", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestHandler_UnauthorizedIssuer(t *testing.T) {
	withAllowedOperators(t, "allowed-op")
	h := commands.NewHandler(&commandsStubDB{}, &config.DashboardConfig{}, commands.NewPendingStore(60*time.Second))
	body := `{"command_type":"mode","issuer_id":"other-op","args":{"mode":"BALANCED"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/commands", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestHandler_KillReturnsConfirmationChallenge(t *testing.T) {
	withAllowedOperators(t, "op-1")
	store := commands.NewPendingStore(60 * time.Second)
	h := commands.NewHandler(&commandsStubDB{}, &config.DashboardConfig{}, store)

	body := `{"command_type":"kill","issuer_id":"op-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/commands", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp commands.Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "confirmation_required" || resp.ConfirmToken == "" || resp.ExpiresAt == "" {
		t.Fatalf("got %+v", resp)
	}
}

func TestConfirmHandler_RedeemsAndEmits(t *testing.T) {
	withAllowedOperators(t, "op-1")
	stub := &commandsStubDB{}
	store := commands.NewPendingStore(60 * time.Second)
	submit := commands.NewHandler(stub, &config.DashboardConfig{}, store)

	submitBody := `{"command_type":"kill","issuer_id":"op-1"}`
	submitReq := httptest.NewRequest(http.MethodPost, "/api/v1/commands", strings.NewReader(submitBody))
	submitRec := httptest.NewRecorder()
	submit.ServeHTTP(submitRec, submitReq)
	if submitRec.Code != http.StatusAccepted {
		t.Fatalf("submit status = %d", submitRec.Code)
	}
	var challenge commands.Response
	if err := json.NewDecoder(submitRec.Body).Decode(&challenge); err != nil {
		t.Fatalf("decode challenge: %v", err)
	}

	confirm := commands.NewConfirmHandler(stub, &config.DashboardConfig{}, store)
	confirmPayload, _ := json.Marshal(commands.ConfirmRequest{
		ConfirmToken: challenge.ConfirmToken,
		IssuerID:     "op-1",
		CommandType:  "kill",
	})
	confirmReq := httptest.NewRequest(http.MethodPost, "/api/v1/commands/confirm", bytes.NewReader(confirmPayload))
	confirmRec := httptest.NewRecorder()
	confirm.ServeHTTP(confirmRec, confirmReq)

	if confirmRec.Code != http.StatusAccepted {
		t.Fatalf("confirm status = %d, body = %s", confirmRec.Code, confirmRec.Body.String())
	}
	var accepted commands.Response
	if err := json.NewDecoder(confirmRec.Body).Decode(&accepted); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if accepted.Status != "accepted" || accepted.CommandID == "" {
		t.Fatalf("got %+v", accepted)
	}
	if len(stub.events) != 1 {
		t.Fatalf("events = %d, want 1", len(stub.events))
	}
}

func TestHandler_RequestBodyTooLarge(t *testing.T) {
	withAllowedOperators(t, "op-1")
	h := commands.NewHandler(&commandsStubDB{}, &config.DashboardConfig{}, commands.NewPendingStore(60*time.Second))
	padding := strings.Repeat("x", 5000)
	body := `{"command_type":"mode","issuer_id":"op-1","args":{"mode":"BALANCED","note":"` + padding + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/commands", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestHandler_InvalidCommandType(t *testing.T) {
	withAllowedOperators(t, "op-1")
	h := commands.NewHandler(&commandsStubDB{}, &config.DashboardConfig{}, commands.NewPendingStore(60*time.Second))
	body := `{"command_type":"reboot","issuer_id":"op-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/commands", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
