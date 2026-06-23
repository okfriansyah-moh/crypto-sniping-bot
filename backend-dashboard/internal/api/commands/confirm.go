package commands

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// pendingConfirm holds a destructive command awaiting operator confirmation.
type pendingConfirm struct {
	CommandType string
	IssuerID    string
	Args        map[string]string
	ExpiresAt   time.Time
}

// PendingStore holds short-lived confirmation tokens (in-process; dashboard is single-process).
type PendingStore struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[string]pendingConfirm
}

// NewPendingStore creates a token store with the given TTL.
func NewPendingStore(ttl time.Duration) *PendingStore {
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	return &PendingStore{
		ttl:   ttl,
		items: make(map[string]pendingConfirm),
	}
}

// Issue stores a destructive command and returns a single-use confirmation token.
func (s *PendingStore) Issue(commandType, issuerID string, args map[string]string, now time.Time) (token string, expiresAt time.Time, err error) {
	if s == nil {
		return "", time.Time{}, fmt.Errorf("confirm store not configured")
	}
	token, err = newConfirmToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt = now.UTC().Add(s.ttl)
	entry := pendingConfirm{
		CommandType: commandType,
		IssuerID:    issuerID,
		Args:        copyArgs(args),
		ExpiresAt:   expiresAt,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpiredLocked(now)
	s.items[token] = entry
	return token, expiresAt, nil
}

// Redeem validates a token and returns the pending command. The token is consumed.
func (s *PendingStore) Redeem(token, issuerID, commandType string, args map[string]string, now time.Time) error {
	if s == nil {
		return fmt.Errorf("confirm store not configured")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("confirm_token required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpiredLocked(now)
	entry, ok := s.items[token]
	if !ok {
		return fmt.Errorf("invalid or expired confirm_token")
	}
	delete(s.items, token)
	if now.UTC().After(entry.ExpiresAt) {
		return fmt.Errorf("invalid or expired confirm_token")
	}
	if entry.IssuerID != issuerID {
		return fmt.Errorf("confirm_token issuer mismatch")
	}
	if entry.CommandType != commandType {
		return fmt.Errorf("confirm_token command_type mismatch")
	}
	if !argsEqual(entry.Args, args) {
		return fmt.Errorf("confirm_token args mismatch")
	}
	return nil
}

func (s *PendingStore) purgeExpiredLocked(now time.Time) {
	now = now.UTC()
	for token, entry := range s.items {
		if now.After(entry.ExpiresAt) {
			delete(s.items, token)
		}
	}
}

func newConfirmToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate confirm token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func copyArgs(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func argsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
