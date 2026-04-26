// Package telegram — Telegram dispatcher reads telegram_event from the event bus
// and sends formatted messages to the operator chat.
// This is the ONLY component that calls the Telegram API.
// Modules must never import this package.
package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"crypto-sniping-bot/database"
)

// TelegramEventPayload is the generic payload for telegram_event events.
// MessageType selects the alert template.
type TelegramEventPayload struct {
	MessageType string          `json:"message_type"` // e.g. "trade_opened", "trade_closed", "kill_switch"
	Text        string          `json:"text"`         // pre-formatted message (optional)
	Data        json.RawMessage `json:"data"`         // type-specific payload
}

// Dispatcher reads telegram_event rows from the event bus and sends them.
type Dispatcher struct {
	adapter  database.Adapter
	client   *Client
	workerID string
	logger   *slog.Logger
}

// NewDispatcher returns a new Telegram Dispatcher.
func NewDispatcher(adapter database.Adapter, client *Client, logger *slog.Logger) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{
		adapter:  adapter,
		client:   client,
		workerID: "telegram_dispatcher",
		logger:   logger,
	}
}

// Run starts the dispatcher polling loop.
// It exits when ctx is cancelled.
func (d *Dispatcher) Run(ctx context.Context) error {
	d.logger.Info("telegram_dispatcher_started")
	for {
		select {
		case <-ctx.Done():
			d.logger.Info("telegram_dispatcher_stopped")
			return ctx.Err()
		default:
		}

		evt, err := d.adapter.ClaimNextEvent(ctx, d.workerID, []string{"telegram_event"})
		if err != nil {
			d.logger.Warn("telegram_dispatcher_poll_failed", "error", err)
			time.Sleep(2 * time.Second)
			continue
		}

		if evt == nil {
			// No events ready — back off briefly.
			time.Sleep(500 * time.Millisecond)
			continue
		}

		if processErr := d.processEvent(ctx, evt); processErr != nil {
			d.logger.Warn("telegram_dispatcher_process_failed",
				"event_id", evt.EventID,
				"error", processErr,
			)
			// Mark processed even on send failure to avoid infinite retry on bad payloads.
		}
		if markErr := d.adapter.MarkEventProcessed(ctx, evt.EventID); markErr != nil {
			d.logger.Warn("telegram_dispatcher_mark_failed",
				"event_id", evt.EventID,
				"error", markErr,
			)
		}
	}
}

func (d *Dispatcher) processEvent(ctx context.Context, evt *database.Event) error {
	var payload TelegramEventPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return fmt.Errorf("dispatcher: unmarshal payload: %w", err)
	}

	text := payload.Text
	if text == "" {
		text = fmt.Sprintf("[%s] %s", payload.MessageType, string(payload.Data))
	}

	return d.client.SendMessage(ctx, text)
}
