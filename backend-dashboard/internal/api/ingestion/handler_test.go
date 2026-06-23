package ingestion_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/backend-dashboard/internal/api/ingestion"
)

func TestHandler_ReturnsIngestionJSON(t *testing.T) {
	cfg := &config.Config{
		Solana: config.SolanaConfig{
			Ingestion: config.SolanaIngestionConfig{Delivery: "stream"},
		},
	}
	h := ingestion.NewHandler(cfg)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ingestion", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var out contracts.IngestionStatusDTO
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.GlobalDelivery != "stream" {
		t.Errorf("GlobalDelivery = %q", out.GlobalDelivery)
	}
}
