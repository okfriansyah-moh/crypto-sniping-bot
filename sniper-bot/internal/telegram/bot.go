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
	apiBase    string // base URL without trailing slash, e.g. "https://api.telegram.org/botTOKEN"
	httpClient *http.Client
}

const defaultTelegramBase = "https://api.telegram.org"

// NewClient returns a new Telegram client.
// botToken is sourced from config (never hardcoded).
// chatID is the operator chat or group ID.
func NewClient(botToken, chatID string) *Client {
	return newClient(botToken, chatID, defaultTelegramBase)
}

// NewClientWithBaseURL creates a Client that sends requests to a custom base
// URL instead of api.telegram.org.  The format string must contain a single
// %s placeholder that will be substituted with the bot token, e.g.
// "http://localhost:PORT/bot%s".  Intended only for tests.
func NewClientWithBaseURL(botToken, chatID, baseURLFmt string) *Client {
	base := fmt.Sprintf(baseURLFmt, botToken)
	return newClient(botToken, chatID, base)
}

func newClient(botToken, chatID, apiBase string) *Client {
	return &Client{
		botToken: botToken,
		chatID:   chatID,
		apiBase:  apiBase,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// apiURL returns the full URL for a Telegram Bot API method (e.g. "sendMessage").
func (c *Client) apiURL(method string) string {
	if c.apiBase == defaultTelegramBase {
		return fmt.Sprintf("%s/bot%s/%s", c.apiBase, c.botToken, method)
	}
	// Custom base already embeds the token (set by NewClientWithBaseURL).
	return c.apiBase + "/" + method
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

	url := c.apiURL("sendMessage")
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
