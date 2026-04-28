// poller.go — Telegram incoming update handler.
//
// Polls the Telegram Bot API via getUpdates (long-polling) and dispatches
// parsed operator commands to the injected Handler.
//
// Architecture invariants:
//   - Does NOT call the database directly.
//   - Does NOT import internal/modules/.
//   - Bot token is never logged — it is masked in error strings.
//   - Unauthorised update sources are rejected before any command runs.
//
// Long-poll timeout is 30 seconds (Telegram max is 60 s; we use 30 for faster
// reconnect on network hiccups).  Between polls the bot increments the offset
// so already-seen updates are never replayed.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	pollerLongPollTimeout  = 30 // seconds (passed in getUpdates ?timeout=)
	pollerRetryDelay       = 5 * time.Second
	pollerHTTPTimeout      = 40 * time.Second // must exceed long-poll timeout
	pollerMaxResponseBytes = 1 << 20          // 1 MiB
)

// Poller polls Telegram's getUpdates endpoint and feeds operator commands to
// a Handler.
type Poller struct {
	client  *Client
	handler *Handler
	chatID  string // only accept updates originating from this chat
	logger  *slog.Logger
	offset  int64
	httpCli *http.Client
}

// NewPoller creates a Poller.
//   - client — the Telegram Bot API client (provides bot token + default chat).
//   - handler — fully initialised command handler with all fn fields set.
//   - logger — structured logger; falls back to slog.Default().
func NewPoller(client *Client, handler *Handler, logger *slog.Logger) *Poller {
	if logger == nil {
		logger = slog.Default()
	}
	return &Poller{
		client:  client,
		handler: handler,
		chatID:  client.chatID,
		logger:  logger,
		httpCli: &http.Client{Timeout: pollerHTTPTimeout},
	}
}

// Run polls getUpdates until ctx is cancelled.
// Safe to run in a goroutine; returns ctx.Err() on clean shutdown.
func (p *Poller) Run(ctx context.Context) error {
	p.logger.Info("telegram_poller_started")
	for {
		select {
		case <-ctx.Done():
			p.logger.Info("telegram_poller_stopped")
			return ctx.Err()
		default:
		}

		updates, err := p.poll(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// EOF and connection-reset are normal: Telegram's servers close idle
			// long-poll connections. Log at DEBUG to avoid noise; the poller will
			// immediately reconnect after the brief retry delay.
			if isPollerTransient(err) {
				p.logger.Debug("telegram_poller_reconnect", "reason", "connection_closed")
			} else {
				p.logger.Warn("telegram_poller_error", "error", err)
			}
			select {
			case <-time.After(pollerRetryDelay):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}

		for _, u := range updates {
			p.dispatch(ctx, u)
		}
	}
}

// ── Telegram API types ───────────────────────────────────────────────────────

type tgUpdate struct {
	UpdateID int64      `json:"update_id"`
	Message  *tgMessage `json:"message"`
}

type tgMessage struct {
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
	Chat      struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	From *struct {
		ID        int64  `json:"id"`
		Username  string `json:"username"`
		FirstName string `json:"first_name"`
	} `json:"from"`
}

type tgGetUpdatesResponse struct {
	OK     bool       `json:"ok"`
	Result []tgUpdate `json:"result"`
}

// poll calls getUpdates with long-polling and returns the received updates.
func (p *Poller) poll(ctx context.Context) ([]tgUpdate, error) {
	if p.client.botToken == "" {
		return nil, fmt.Errorf("poller: bot token not configured")
	}

	url := fmt.Sprintf(
		"%s?timeout=%d&offset=%d",
		p.client.apiURL("getUpdates"),
		pollerLongPollTimeout,
		p.offset,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("poller: build request: %w", sanitizeToken(err, p.client.botToken))
	}

	resp, err := p.httpCli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("poller: getUpdates: %w", sanitizeToken(err, p.client.botToken))
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(io.LimitReader(resp.Body, pollerMaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("poller: read response: %w", err)
	}

	var result tgGetUpdatesResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("poller: parse response: %w", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("poller: Telegram API returned ok=false")
	}

	// Advance offset past the last processed update so Telegram won't resend.
	for _, u := range result.Result {
		if u.UpdateID >= p.offset {
			p.offset = u.UpdateID + 1
		}
	}

	return result.Result, nil
}

// dispatch processes a single Telegram update: validates origin, parses
// command, calls handler, sends reply.
func (p *Poller) dispatch(ctx context.Context, u tgUpdate) {
	if u.Message == nil || u.Message.Text == "" {
		return
	}

	msg := u.Message

	// Gate: only accept messages from the configured operator chat.
	if p.chatID != "" {
		chatIDStr := strconv.FormatInt(msg.Chat.ID, 10)
		if chatIDStr != p.chatID {
			p.logger.Warn("telegram_poller_unexpected_chat",
				"chat_id", msg.Chat.ID,
				"expected", p.chatID,
			)
			return
		}
	}

	// Extract issuer ID.
	issuerID := ""
	if msg.From != nil {
		issuerID = strconv.FormatInt(msg.From.ID, 10)
	}

	// Parse the command.
	cmd, err := ParseCommand(msg.Text, issuerID)
	if err != nil {
		// Not a command (plain message) — ignore silently.
		return
	}

	p.logger.Info("telegram_command_received",
		"command", cmd.Type,
		"issuer_id", issuerID,
		"chat_id", msg.Chat.ID,
	)

	result, err := p.handler.Handle(ctx, cmd)
	replyText := ""
	if err != nil {
		p.logger.Warn("telegram_command_failed",
			"command", cmd.Type,
			"issuer_id", issuerID,
			"error", err,
		)
		replyText = fmt.Sprintf("❌ %s", err.Error())
	} else {
		replyText = result.Text
		if result.Destructive {
			p.logger.Info("telegram_destructive_command_executed",
				"command", cmd.Type,
				"issuer_id", issuerID,
			)
		}
	}

	// Send reply via the shared Client (safe concurrent use).
	if sendErr := p.client.SendMessage(ctx, replyText); sendErr != nil {
		p.logger.Warn("telegram_poller_reply_failed",
			"command", cmd.Type,
			"error", sendErr,
		)
	}
}

// isPollerTransient returns true for errors that represent a normal Telegram
// long-poll connection closure (EOF, connection reset). These are not bugs —
// Telegram and intermediate proxies routinely close idle keep-alive connections.
func isPollerTransient(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return errors.Is(err, io.EOF) ||
		strings.Contains(s, "EOF") ||
		strings.Contains(s, "connection reset by peer") ||
		strings.Contains(s, "use of closed network connection")
}

// sanitizeToken replaces the bot token in an error message so it is never logged.
func sanitizeToken(err error, token string) error {
	if err == nil || token == "" {
		return err
	}
	msg := strings.ReplaceAll(err.Error(), token, "[REDACTED]")
	if msg == err.Error() {
		return err
	}
	return fmt.Errorf("%s", msg) //nolint:err113
}

// sendMessageRequest is used by the replyRaw helper (kept for symmetry).
type replyRequest struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode"`
}

// replyRaw sends a reply directly to a specific chat ID (used internally).
func (p *Poller) replyRaw(ctx context.Context, chatID int64, text string) error {
	payload := replyRequest{
		ChatID:    strconv.FormatInt(chatID, 10),
		Text:      text,
		ParseMode: "HTML",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", p.client.botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return sanitizeToken(err, p.client.botToken)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.httpCli.Do(req)
	if err != nil {
		return sanitizeToken(err, p.client.botToken)
	}
	resp.Body.Close() //nolint:errcheck
	return nil
}
