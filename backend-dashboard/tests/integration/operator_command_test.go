package integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/operator"
)

func TestOperatorCommand_ModeRoundTrip(t *testing.T) {
	db := newDashboardFixture()
	srv := newDashboardTestServer(t, db)
	defer srv.Close()

	body := `{"command_type":"mode","issuer_id":"integration-operator","args":{"mode":"EXPLORATION"}}`
	req := dashboardAuthRequest(t, http.MethodPost, srv.URL+"/api/v1/commands", []byte(body))
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST commands: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("status = %d, body = %s", res.StatusCode, raw)
	}

	events := db.Events()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].EventType != contracts.OperatorCommandEventType {
		t.Fatalf("event type = %q", events[0].EventType)
	}

	var cmd contracts.OperatorCommandDTO
	if err := json.Unmarshal(events[0].Payload, &cmd); err != nil {
		t.Fatalf("unmarshal command: %v", err)
	}
	if err := operator.ExecuteCommand(context.Background(), db, slog.Default(), cmd, operator.CommandSourceDashboard); err != nil {
		t.Fatalf("execute command: %v", err)
	}
	if db.Mode() != "EXPLORATION" {
		t.Fatalf("mode = %q, want EXPLORATION", db.Mode())
	}
}

func TestOperatorCommand_KillConfirmRoundTrip(t *testing.T) {
	db := newDashboardFixture()
	srv := newDashboardTestServer(t, db)
	defer srv.Close()
	client := srv.Client()

	submitBody := `{"command_type":"kill","issuer_id":"integration-operator"}`
	submitReq := dashboardAuthRequest(t, http.MethodPost, srv.URL+"/api/v1/commands", []byte(submitBody))
	submitRes, err := client.Do(submitReq)
	if err != nil {
		t.Fatalf("submit kill: %v", err)
	}
	defer submitRes.Body.Close()
	if submitRes.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(submitRes.Body)
		t.Fatalf("submit status = %d, body = %s", submitRes.StatusCode, raw)
	}

	var challenge struct {
		Status       string `json:"status"`
		ConfirmToken string `json:"confirm_token"`
	}
	if err := json.NewDecoder(submitRes.Body).Decode(&challenge); err != nil {
		t.Fatalf("decode challenge: %v", err)
	}
	if challenge.Status != "confirmation_required" || challenge.ConfirmToken == "" {
		t.Fatalf("challenge = %+v", challenge)
	}

	confirmPayload := `{"confirm_token":"` + challenge.ConfirmToken +
		`","issuer_id":"integration-operator","command_type":"kill"}`
	confirmReq := dashboardAuthRequest(t, http.MethodPost, srv.URL+"/api/v1/commands/confirm", []byte(confirmPayload))
	confirmRes, err := client.Do(confirmReq)
	if err != nil {
		t.Fatalf("confirm kill: %v", err)
	}
	defer confirmRes.Body.Close()
	if confirmRes.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(confirmRes.Body)
		t.Fatalf("confirm status = %d, body = %s", confirmRes.StatusCode, raw)
	}

	events := db.Events()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1 emitted command event", len(events))
	}

	var cmd contracts.OperatorCommandDTO
	if err := json.Unmarshal(events[0].Payload, &cmd); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cmd.ConfirmToken == "" {
		t.Fatal("expected confirm_token on emitted kill command")
	}

	if err := operator.ExecuteCommand(context.Background(), db, slog.Default(), cmd, operator.CommandSourceDashboard); err != nil {
		t.Fatalf("execute kill: %v", err)
	}
	halted, reason, err := db.IsSystemHalted(context.Background())
	if err != nil {
		t.Fatalf("halt state: %v", err)
	}
	if !halted {
		t.Fatal("expected system halted after kill command")
	}
	if !strings.Contains(reason, "kill") {
		t.Fatalf("halt reason = %q", reason)
	}
}

func TestOperatorCommand_ForbiddenIssuer(t *testing.T) {
	db := newDashboardFixture()
	srv := newDashboardTestServer(t, db)
	defer srv.Close()

	body := `{"command_type":"mode","issuer_id":"not-allowed","args":{"mode":"STRICT"}}`
	req := dashboardAuthRequest(t, http.MethodPost, srv.URL+"/api/v1/commands", []byte(body))
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", res.StatusCode)
	}
}

func TestOperatorCommand_IdempotentEventInsert(t *testing.T) {
	db := newDashboardFixture()
	ts := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	cmd, err := contracts.NewOperatorCommandDTO(contracts.CommandTypeMode, "integration-operator", "", ts, map[string]string{"mode": "STRICT"})
	if err != nil {
		t.Fatalf("new command: %v", err)
	}
	payload, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	evt := database.Event{
		EventID:       cmd.CommandID,
		EventType:     contracts.OperatorCommandEventType,
		Payload:       payload,
		TraceID:       cmd.CommandID,
		CorrelationID: cmd.CommandID,
		VersionID:     "strat-integration01",
		CreatedAt:     ts,
		Consumer:      "operator_command_worker",
	}
	for i := 0; i < 2; i++ {
		if err := db.InsertEvent(context.Background(), evt); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	if len(db.Events()) != 1 {
		t.Fatalf("duplicate insert created %d events", len(db.Events()))
	}
}
