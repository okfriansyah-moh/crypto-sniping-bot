package telegram_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crypto-sniping-bot/internal/telegram"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// tgUpdateResp builds a minimal Telegram getUpdates JSON response.
func tgUpdateResp(updates []map[string]any) string {
	b, _ := json.Marshal(map[string]any{"ok": true, "result": updates})
	return string(b)
}

// newTestPoller creates a Poller backed by a fake Telegram API server.
// The server is controlled by responseFunc which receives the request and
// returns the body to send back.  The Poller is configured with the
// returned chatID as the allowed operator chat.
func newTestPoller(
	t *testing.T,
	chatID string,
	handler *telegram.Handler,
	responseFunc func(r *http.Request) string,
) (*telegram.Poller, *httptest.Server) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, responseFunc(r))
	}))

	// Build a Client pointing at the fake server instead of api.telegram.org.
	client := telegram.NewClientWithBaseURL("fake-token", chatID, srv.URL+"/bot%s")
	poller := telegram.NewPoller(client, handler, nil)
	return poller, srv
}

// ── offset advancement ────────────────────────────────────────────────────────

// TestPoller_OffsetAdvances verifies that after receiving an update with
// update_id N the poller sends the next request with offset = N+1, preventing
// re-delivery of already-seen updates.
func TestPoller_OffsetAdvances(t *testing.T) {
	var receivedOffsets []string

	handler := telegram.NewHandler(telegram.HandlerOptions{
		StatusFn: func(ctx context.Context) (string, error) {
			return "ok", nil
		},
	})

	chatID := "999"
	firstDone := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Capture offsets only from getUpdates requests.
		if strings.Contains(r.URL.Path, "getUpdates") {
			receivedOffsets = append(receivedOffsets, r.URL.Query().Get("offset"))
			if !firstDone {
				firstDone = true
				// First poll: return one update with update_id=42.
				fmt.Fprint(w, tgUpdateResp([]map[string]any{
					{
						"update_id": 42,
						"message": map[string]any{
							"message_id": 1,
							"text":       "/status",
							"chat":       map[string]any{"id": 999},
							"from":       map[string]any{"id": 6573930967, "username": "operator"},
						},
					},
				}))
				return
			}
			// Subsequent polls: empty result.
			fmt.Fprint(w, tgUpdateResp(nil))
			return
		}

		// sendMessage reply — return ok so the poller doesn't log a warning.
		fmt.Fprint(w, `{"ok":true,"result":{}}`)
	}))
	defer srv.Close()

	client := telegram.NewClientWithBaseURL("fake-token", chatID, srv.URL+"/bot%s")
	poller := telegram.NewPoller(client, handler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = poller.Run(ctx)

	if len(receivedOffsets) < 2 {
		t.Fatalf("expected at least 2 getUpdates calls, got %d", len(receivedOffsets))
	}
	// After seeing update_id=42 the next offset must be "43".
	if receivedOffsets[1] != "43" {
		t.Errorf("expected offset=43 on second getUpdates call, got %q", receivedOffsets[1])
	}
}

// ── chat-id gate ──────────────────────────────────────────────────────────────

// TestPoller_ChatIDMismatch_CommandNotExecuted verifies that a message from a
// chat that does not match the configured operator chat is silently ignored
// and the handler is never called.
func TestPoller_ChatIDMismatch_CommandNotExecuted(t *testing.T) {
	called := false
	handler := telegram.NewHandler(telegram.HandlerOptions{
		StatusFn: func(ctx context.Context) (string, error) {
			called = true
			return "ok", nil
		},
	})

	// Poller is configured for chat 999; message arrives from chat 1234.
	chatID := "999"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, tgUpdateResp([]map[string]any{
			{
				"update_id": 10,
				"message": map[string]any{
					"message_id": 1,
					"text":       "/status",
					"chat":       map[string]any{"id": 1234}, // wrong chat
					"from":       map[string]any{"id": 6573930967, "username": "attacker"},
				},
			},
		}))
	}))
	defer srv.Close()

	client := telegram.NewClientWithBaseURL("fake-token", chatID, srv.URL+"/bot%s")
	poller := telegram.NewPoller(client, handler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_ = poller.Run(ctx)

	if called {
		t.Fatal("handler must not be called when chat_id does not match")
	}
}

// ── handler invocation ────────────────────────────────────────────────────────

// TestPoller_CorrectChat_HandlerInvoked verifies that a message from the
// configured operator chat causes the handler to be called and a reply to be
// sent back.
func TestPoller_CorrectChat_HandlerInvoked(t *testing.T) {
	called := false
	handler := telegram.NewHandler(telegram.HandlerOptions{
		StatusFn: func(ctx context.Context) (string, error) {
			called = true
			return "pipeline healthy", nil
		},
	})

	chatID := "999"
	var sentMessages []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		if strings.Contains(path, "sendMessage") {
			// Capture the reply body.
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if text, ok := body["text"].(string); ok {
				sentMessages = append(sentMessages, text)
			}
			fmt.Fprint(w, `{"ok":true,"result":{}}`)
			return
		}

		// getUpdates — return one message then empty.
		if len(sentMessages) == 0 {
			fmt.Fprint(w, tgUpdateResp([]map[string]any{
				{
					"update_id": 1,
					"message": map[string]any{
						"message_id": 1,
						"text":       "/status",
						"chat":       map[string]any{"id": 999},
						"from":       map[string]any{"id": 6573930967, "username": "operator"},
					},
				},
			}))
		} else {
			fmt.Fprint(w, tgUpdateResp(nil))
		}
	}))
	defer srv.Close()

	client := telegram.NewClientWithBaseURL("fake-token", chatID, srv.URL+"/bot%s")
	poller := telegram.NewPoller(client, handler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = poller.Run(ctx)

	if !called {
		t.Fatal("expected handler to be called for message from correct chat")
	}
	if len(sentMessages) == 0 {
		t.Fatal("expected a reply to be sent via sendMessage")
	}
	if !strings.Contains(sentMessages[0], "pipeline healthy") {
		t.Errorf("expected reply to contain handler output, got: %q", sentMessages[0])
	}
}

// ── token sanitization ────────────────────────────────────────────────────────

// TestPoller_TokenNotLeaked verifies that even when the API returns a
// non-200 response that produces an error, the bot token does not appear in
// the error message returned by Run (it would propagate to logs otherwise).
func TestPoller_TokenNotLeaked(t *testing.T) {
	const fakeToken = "SECRET123TOKEN"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a Telegram API failure with bad JSON.
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal error")
	}))
	defer srv.Close()

	handler := telegram.NewHandler(telegram.HandlerOptions{})
	client := telegram.NewClientWithBaseURL(fakeToken, "999", srv.URL+"/bot%s")
	poller := telegram.NewPoller(client, handler, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	err := poller.Run(ctx)

	// Run returns ctx.Err() on clean shutdown — that's fine.
	// What we must ensure is that the token does not appear in any error text.
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	if strings.Contains(errStr, fakeToken) {
		t.Errorf("bot token leaked in error string: %q", errStr)
	}
}
