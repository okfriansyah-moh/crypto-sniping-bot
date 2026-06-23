package ingestion_solana

import (
	"os"
	"strings"

	"crypto-sniping-bot/internal/app/config"
)

const (
	DeliveryStream  = "stream"
	DeliveryWebhook = "webhook"
	DeliveryHybrid  = "hybrid"
)

// EffectiveGlobalDelivery resolves the global ingestion delivery mode.
// Precedence: SOLANA_INGESTION_DELIVERY env → chains.yaml → default stream.
func EffectiveGlobalDelivery(cfg config.SolanaConfig) string {
	if v := strings.TrimSpace(os.Getenv("SOLANA_INGESTION_DELIVERY")); v != "" {
		return normalizeDelivery(v)
	}
	if v := strings.TrimSpace(cfg.Ingestion.Delivery); v != "" {
		return normalizeDelivery(v)
	}
	return DeliveryStream
}

// EffectiveProgramDelivery returns stream or webhook for a single program.
// Precedence: hybrid per-program override → global mode.
func EffectiveProgramDelivery(cfg config.SolanaConfig, prog config.SolanaProgramConfig) string {
	global := EffectiveGlobalDelivery(cfg)
	switch global {
	case DeliveryHybrid:
		if v := strings.TrimSpace(prog.Delivery); v != "" {
			return normalizeDelivery(v)
		}
		return DeliveryStream
	case DeliveryWebhook:
		return DeliveryWebhook
	default:
		return DeliveryStream
	}
}

// ProgramUsesStream reports whether the program should run WS subscription loops.
func ProgramUsesStream(cfg config.SolanaConfig, prog config.SolanaProgramConfig) bool {
	return EffectiveProgramDelivery(cfg, prog) != DeliveryWebhook
}

// WebhookIngressActive reports whether HTTP webhook ingress should be registered.
func WebhookIngressActive(cfg config.SolanaConfig) bool {
	switch EffectiveGlobalDelivery(cfg) {
	case DeliveryWebhook:
		return true
	case DeliveryHybrid:
		return cfg.Ingestion.Webhook.Enabled
	default:
		return false
	}
}

func normalizeDelivery(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case DeliveryWebhook:
		return DeliveryWebhook
	case DeliveryHybrid:
		return DeliveryHybrid
	default:
		return DeliveryStream
	}
}
