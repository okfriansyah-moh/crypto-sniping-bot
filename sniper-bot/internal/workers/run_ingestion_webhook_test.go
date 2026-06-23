package workers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/sniper-bot/internal/modules/ingestion_solana"
)

func TestParseHeliusWebhookBody_Array(t *testing.T) {
	body := []byte(`[{
		"signature": "Sig1111111111111111111111111111111111111111",
		"slot": 42,
		"timestamp": 1700000000,
		"transaction": {
			"message": {
				"accountKeys": ["pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA"],
				"recentBlockhash": "hash",
				"instructions": [{
					"programIdIndex": 0,
					"accounts": [],
					"data": ""
				}]
			}
		},
		"meta": {}
	}]`)
	notifs, err := parseHeliusWebhookBody(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(notifs) != 1 {
		t.Fatalf("want 1 notification, got %d", len(notifs))
	}
	if notifs[0].Signature != "Sig1111111111111111111111111111111111111111" {
		t.Fatalf("signature not preserved: %q", notifs[0].Signature)
	}
	if notifs[0].Transaction == nil {
		t.Fatal("expected embedded transaction")
	}
}

func TestParseHeliusWebhookBody_EmptyRejected(t *testing.T) {
	if _, err := parseHeliusWebhookBody([]byte{}); err == nil {
		t.Fatal("expected error for empty body")
	}
}

func TestParseHeliusWebhookBody_InvalidJSON(t *testing.T) {
	if _, err := parseHeliusWebhookBody([]byte(`{`)); err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestHeliusWebhookHandler_Unauthorized(t *testing.T) {
	t.Setenv("HELIUS_WEBHOOK_SECRET", "secret")
	h := NewHeliusWebhookHandler(HeliusWebhookDeps{
		Module: ingestion_solana.New(config.SolanaConfig{}, "v1", nil, nil),
		Cfg:    config.SolanaConfig{},
	})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/helius", bytes.NewReader([]byte(`[]`)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestHeliusWebhookHandler_OversizeBody(t *testing.T) {
	t.Setenv("HELIUS_WEBHOOK_SECRET", "secret")
	h := NewHeliusWebhookHandler(HeliusWebhookDeps{
		Module:   ingestion_solana.New(config.SolanaConfig{}, "v1", nil, nil),
		Cfg:      config.SolanaConfig{},
		MaxBytes: 8,
		Secret:   "secret",
	})
	body := bytes.Repeat([]byte("x"), 16)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/helius", bytes.NewReader(body))
	req.Header.Set("Authorization", "secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d", rec.Code)
	}
}

func TestValidateWebhookBoot_MissingSecretFails(t *testing.T) {
	os.Unsetenv("HELIUS_WEBHOOK_SECRET")
	cfg := config.SolanaConfig{Ingestion: config.SolanaIngestionConfig{Delivery: "webhook"}}
	if err := ValidateWebhookBoot(cfg); err == nil {
		t.Fatal("expected boot validation error without secret")
	}
}

func TestValidateWebhookBoot_StreamModeNoSecretOK(t *testing.T) {
	os.Unsetenv("HELIUS_WEBHOOK_SECRET")
	cfg := config.SolanaConfig{Ingestion: config.SolanaIngestionConfig{Delivery: "stream"}}
	if err := ValidateWebhookBoot(cfg); err != nil {
		t.Fatalf("stream mode should not require secret: %v", err)
	}
}
