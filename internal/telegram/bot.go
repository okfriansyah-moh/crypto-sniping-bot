// Package telegram — event-bus-only Telegram dispatcher.
// This package MUST NOT be imported by any module under internal/modules/.
// All user-facing events arrive via the event bus (telegram_event type).
// Operator commands (/status, /pnl, /positions, /kill, /resume, /version)
// are handled here; destructive actions require logged confirmation.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxResponseBytes caps the Telegram API response size to prevent unbounded reads.
const maxResponseBytes = 1 << 20 // 1 MiB

// Client is a minimal Telegram Bot API client.
// Only SendMessage is needed for the dispatcher.
type Client struct {
	botToken   string
	chatID     string
	httpClient *http.Client
}

// NewClient returns a new Telegram client.
// botToken is sourced from config (never hardcoded).
// chatID is the operator chat or group ID.
func NewClient(botToken, chatID string) *Client {
	return &Client{
		botToken: botToken,
		chatID:   chatID,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type sendMessageRequest struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode"`
}

type telegramResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
}

// SendMessage sends a text message to the configured chat.
// ParseMode is "HTML" by default (safe subset).
func (c *Client) SendMessage(ctx context.Context, text string) error {
	if c.botToken == "" || c.chatID == "" {
		return fmt.Errorf("telegram: client not configured (empty bot_token or chat_id)")
	}

	reqBody := sendMessageRequest{
		ChatID:    c.chatID,
		Text:      text,
		ParseMode: "HTML",
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("telegram: marshal request: %w", err)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", c.botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("telegram: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Mask the bot token from the error string: net/http errors include the
		// request URL which embeds the token (e.g. "/botTOKEN/sendMessage").
		sanitized := strings.ReplaceAll(err.Error(), c.botToken, "[REDACTED]")
		return fmt.Errorf("telegram: send: %s", sanitized)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("telegram: read response: %w", err)
	}

	var tResp telegramResponse
	if jsonErr := json.Unmarshal(respBody, &tResp); jsonErr != nil {
		return fmt.Errorf("telegram: parse response: %w", jsonErr)
	}
	if !tResp.OK {
		return fmt.Errorf("telegram: api error: %s", tResp.Description)
	}
	return nil
}
