package ingestion_solana

import (
	"os"
	"testing"

	"crypto-sniping-bot/internal/app/config"
)

func TestEffectiveGlobalDelivery_EnvOverridesYAML(t *testing.T) {
	t.Setenv("SOLANA_INGESTION_DELIVERY", "webhook")
	cfg := config.SolanaConfig{Ingestion: config.SolanaIngestionConfig{Delivery: "stream"}}
	if got := EffectiveGlobalDelivery(cfg); got != DeliveryWebhook {
		t.Fatalf("want webhook, got %q", got)
	}
}

func TestEffectiveProgramDelivery_HybridPerProgram(t *testing.T) {
	cfg := config.SolanaConfig{Ingestion: config.SolanaIngestionConfig{Delivery: DeliveryHybrid}}
	streamProg := config.SolanaProgramConfig{Family: "raydium-v4"}
	webhookProg := config.SolanaProgramConfig{Family: "pumpfun-amm", Delivery: DeliveryWebhook}
	if EffectiveProgramDelivery(cfg, streamProg) != DeliveryStream {
		t.Fatal("expected default stream in hybrid")
	}
	if EffectiveProgramDelivery(cfg, webhookProg) != DeliveryWebhook {
		t.Fatal("expected per-program webhook override")
	}
}

func TestProgramUsesStream_WebhookSkipsWS(t *testing.T) {
	cfg := config.SolanaConfig{Ingestion: config.SolanaIngestionConfig{Delivery: DeliveryWebhook}}
	prog := config.SolanaProgramConfig{Family: "pumpfun-amm"}
	if ProgramUsesStream(cfg, prog) {
		t.Fatal("webhook delivery must not start WS loops")
	}
}

func TestWebhookIngressActive_HybridRequiresEnabled(t *testing.T) {
	cfg := config.SolanaConfig{Ingestion: config.SolanaIngestionConfig{
		Delivery: DeliveryHybrid,
		Webhook:  config.SolanaWebhookConfig{Enabled: false},
	}}
	if WebhookIngressActive(cfg) {
		t.Fatal("hybrid without webhook.enabled should not register ingress")
	}
	os.Unsetenv("SOLANA_INGESTION_DELIVERY")
}
