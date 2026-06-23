package workers

import (
	"encoding/json"
	"fmt"

	"crypto-sniping-bot/sniper-bot/internal/modules/ingestion_solana"
	"crypto-sniping-bot/sniper-bot/internal/rpc"
)

// heliusWebhookItem is one transaction delivered by a Helius raw/enhanced webhook.
type heliusWebhookItem struct {
	Signature   string          `json:"signature"`
	Slot        uint64          `json:"slot"`
	Timestamp   int64           `json:"timestamp"`
	Transaction json.RawMessage `json:"transaction"`
	Meta        json.RawMessage `json:"meta"`
}

// parseHeliusWebhookBody converts a Helius webhook POST body into LogsNotifications.
func parseHeliusWebhookBody(body []byte) ([]ingestion_solana.LogsNotification, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("webhook_ingress: empty body")
	}
	trimmed := body
	for len(trimmed) > 0 && (trimmed[0] == ' ' || trimmed[0] == '\n' || trimmed[0] == '\t') {
		trimmed = trimmed[1:]
	}
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("webhook_ingress: empty body")
	}

	var items []heliusWebhookItem
	if trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, fmt.Errorf("webhook_ingress: unmarshal array: %w", err)
		}
	} else {
		var single heliusWebhookItem
		if err := json.Unmarshal(trimmed, &single); err != nil {
			return nil, fmt.Errorf("webhook_ingress: unmarshal object: %w", err)
		}
		items = []heliusWebhookItem{single}
	}

	out := make([]ingestion_solana.LogsNotification, 0, len(items))
	for _, item := range items {
		if item.Signature == "" {
			continue
		}
		tx, err := composeHeliusWebhookTransaction(item)
		if err != nil {
			return nil, err
		}
		out = append(out, ingestion_solana.LogsNotification{
			Signature:   item.Signature,
			Slot:        item.Slot,
			Transaction: tx,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("webhook_ingress: no parseable transactions")
	}
	return out, nil
}

func composeHeliusWebhookTransaction(item heliusWebhookItem) (*ingestion_solana.TransactionResult, error) {
	if len(item.Transaction) == 0 {
		return nil, fmt.Errorf("webhook_ingress: %s missing transaction", item.Signature)
	}
	meta := item.Meta
	if len(meta) == 0 {
		meta = json.RawMessage(`{}`)
	}
	composed, err := json.Marshal(map[string]interface{}{
		"slot":        item.Slot,
		"blockTime":   item.Timestamp,
		"transaction": json.RawMessage(item.Transaction),
		"meta":        json.RawMessage(meta),
	})
	if err != nil {
		return nil, fmt.Errorf("webhook_ingress: compose %s: %w", item.Signature, err)
	}
	return rpc.ParseGetTransactionResponse(item.Signature, composed)
}
