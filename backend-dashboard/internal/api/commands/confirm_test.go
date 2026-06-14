package commands_test

import (
	"testing"
	"time"

	"crypto-sniping-bot/backend-dashboard/internal/api/commands"
)

func TestPendingStore_IssueAndRedeem(t *testing.T) {
	store := commands.NewPendingStore(60 * time.Second)
	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	args := map[string]string{"position_id": "pos-1"}

	token, expiresAt, err := store.Issue("force_close", "op-1", args, now)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if token == "" || !expiresAt.After(now) {
		t.Fatalf("token=%q expiresAt=%v", token, expiresAt)
	}
	if err := store.Redeem(token, "op-1", "force_close", args, now.Add(time.Second)); err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if err := store.Redeem(token, "op-1", "force_close", args, now.Add(2*time.Second)); err == nil {
		t.Fatal("expected error on second redeem")
	}
}

func TestPendingStore_ExpiredTokenRejected(t *testing.T) {
	store := commands.NewPendingStore(5 * time.Second)
	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	token, _, err := store.Issue("kill", "op-1", nil, now)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	later := now.Add(10 * time.Second)
	if err := store.Redeem(token, "op-1", "kill", map[string]string{}, later); err == nil {
		t.Fatal("expected expired token error")
	}
}

func TestPendingStore_ArgsMismatch(t *testing.T) {
	store := commands.NewPendingStore(60 * time.Second)
	now := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	token, _, err := store.Issue("kill", "op-1", nil, now)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if err := store.Redeem(token, "op-1", "kill", map[string]string{"x": "y"}, now); err == nil {
		t.Fatal("expected args mismatch error")
	}
}
