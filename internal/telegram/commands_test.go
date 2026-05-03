package telegram_test

import (
	"context"
	"testing"
	"time"

	"crypto-sniping-bot/internal/telegram"
)

func TestParseCommand_Known(t *testing.T) {
	cases := []struct {
		input string
		want  telegram.CommandType
	}{
		{"/status", telegram.CmdStatus},
		{"/pnl", telegram.CmdPnl},
		{"/positions", telegram.CmdPositions},
		{"/kill", telegram.CmdKill},
		{"/resume", telegram.CmdResume},
		{"/version", telegram.CmdVersion},
	}
	for _, tc := range cases {
		t.Run(string(tc.want), func(t *testing.T) {
			req, err := telegram.ParseCommand(tc.input, "user123")
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.input, err)
			}
			if req.Type != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, req.Type)
			}
			if req.IssuerID != "user123" {
				t.Fatalf("expected issuer user123, got %q", req.IssuerID)
			}
		})
	}
}

func TestParseCommand_Unknown(t *testing.T) {
	_, err := telegram.ParseCommand("/notacommand", "user1")
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
}

func TestParseCommand_NotCommand(t *testing.T) {
	_, err := telegram.ParseCommand("hello world", "user1")
	if err == nil {
		t.Fatal("expected error for non-command text")
	}
}

func TestHandler_Status(t *testing.T) {
	called := false
	h := telegram.NewHandler(telegram.HandlerOptions{
		StatusFn: func(ctx context.Context) (string, error) {
			called = true
			return "all good", nil
		},
	})
	req := &telegram.CommandRequest{
		Type:     telegram.CmdStatus,
		IssuedAt: time.Now(),
		IssuerID: "user1",
	}
	result, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("status function not called")
	}
	if result.Text != "all good" {
		t.Fatalf("expected 'all good', got %q", result.Text)
	}
}

func TestHandler_Kill_Destructive(t *testing.T) {
	h := telegram.NewHandler(telegram.HandlerOptions{
		KillFn: func(ctx context.Context) error {
			return nil
		},
		AllowedUserIDs: []string{"operator42"},
	})
	req := &telegram.CommandRequest{
		Type:     telegram.CmdKill,
		IssuedAt: time.Now(),
		IssuerID: "operator42",
	}
	result, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Destructive {
		t.Fatal("expected kill to be flagged as destructive")
	}
}

func TestHandler_Unconfigured(t *testing.T) {
	h := telegram.NewHandler(telegram.HandlerOptions{})
	req := &telegram.CommandRequest{
		Type:     telegram.CmdPnl,
		IssuedAt: time.Now(),
		IssuerID: "user1",
	}
	result, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text == "" {
		t.Fatal("expected non-empty fallback text")
	}
}

// ── Authorization security tests ──────────────────────────────────────────────

func TestHandler_EmptyIssuerID_Rejected(t *testing.T) {
	h := telegram.NewHandler(telegram.HandlerOptions{
		StatusFn: func(ctx context.Context) (string, error) { return "ok", nil },
	})
	req := &telegram.CommandRequest{
		Type:     telegram.CmdStatus,
		IssuedAt: time.Now(),
		IssuerID: "", // empty — must always be rejected
	}
	_, err := h.Handle(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for empty IssuerID")
	}
}

func TestHandler_UnauthorizedIssuer_Rejected(t *testing.T) {
	h := telegram.NewHandler(telegram.HandlerOptions{
		StatusFn:       func(ctx context.Context) (string, error) { return "ok", nil },
		AllowedUserIDs: []string{"operator1"},
	})
	req := &telegram.CommandRequest{
		Type:     telegram.CmdStatus,
		IssuedAt: time.Now(),
		IssuerID: "attacker99",
	}
	_, err := h.Handle(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for unauthorized issuer")
	}
}

func TestHandler_DestructiveCommand_NoAllowlist_Rejected(t *testing.T) {
	// Fail-closed: destructive commands must be rejected when AllowedUserIDs is empty.
	h := telegram.NewHandler(telegram.HandlerOptions{
		KillFn: func(ctx context.Context) error { return nil },
		// No AllowedUserIDs configured.
	})
	req := &telegram.CommandRequest{
		Type:     telegram.CmdKill,
		IssuedAt: time.Now(),
		IssuerID: "anyone",
	}
	_, err := h.Handle(context.Background(), req)
	if err == nil {
		t.Fatal("expected error: destructive commands must be rejected when allowlist is not configured")
	}
}

func TestHandler_ReadOnlyCommand_NoAllowlist_Allowed(t *testing.T) {
	// Read-only commands pass even when AllowedUserIDs is empty.
	h := telegram.NewHandler(telegram.HandlerOptions{
		StatusFn: func(ctx context.Context) (string, error) { return "ok", nil },
	})
	req := &telegram.CommandRequest{
		Type:     telegram.CmdStatus,
		IssuedAt: time.Now(),
		IssuerID: "user1",
	}
	_, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error for read-only command without allowlist: %v", err)
	}
}

func TestHandler_AuthorizedIssuer_Allowed(t *testing.T) {
	h := telegram.NewHandler(telegram.HandlerOptions{
		KillFn:         func(ctx context.Context) error { return nil },
		AllowedUserIDs: []string{"operator1", "operator2"},
	})
	req := &telegram.CommandRequest{
		Type:     telegram.CmdKill,
		IssuedAt: time.Now(),
		IssuerID: "operator2",
	}
	result, err := h.Handle(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error for authorized issuer: %v", err)
	}
	if !result.Destructive {
		t.Error("expected destructive flag on kill")
	}
}
