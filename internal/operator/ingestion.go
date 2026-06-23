package operator

import (
	"os"
	"strings"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

const (
	deliveryStream  = "stream"
	deliveryWebhook = "webhook"
	deliveryHybrid  = "hybrid"
)

// BuildIngestionStatus maps Solana chains config to the dashboard ingestion view.
func BuildIngestionStatus(cfg *config.Config) *contracts.IngestionStatusDTO {
	out := &contracts.IngestionStatusDTO{
		GlobalDelivery: deliveryStream,
		TransportMode:  "rpc",
		Programs:       []contracts.IngestionProgramStatusDTO{},
	}
	if cfg == nil {
		return out
	}

	sol := cfg.Solana
	global := effectiveGlobalDelivery(sol)
	out.GlobalDelivery = global
	out.WebhookActive = webhookIngressActive(sol)
	if mode := strings.TrimSpace(sol.Transport.Mode); mode != "" {
		out.TransportMode = mode
	}

	for _, prog := range sol.Programs {
		out.Programs = append(out.Programs, contracts.IngestionProgramStatusDTO{
			ProgramID: prog.ProgramID,
			Family:    prog.Family,
			Delivery:  effectiveProgramDelivery(sol, prog),
			Disabled:  prog.Disabled,
		})
	}
	return out
}

// effectiveGlobalDelivery mirrors ingestion_solana.EffectiveGlobalDelivery without
// importing sniper-bot modules from the cross-app operator layer.
func effectiveGlobalDelivery(cfg config.SolanaConfig) string {
	if v := strings.TrimSpace(os.Getenv("SOLANA_INGESTION_DELIVERY")); v != "" {
		return normalizeDelivery(v)
	}
	if v := strings.TrimSpace(cfg.Ingestion.Delivery); v != "" {
		return normalizeDelivery(v)
	}
	return deliveryStream
}

func effectiveProgramDelivery(cfg config.SolanaConfig, prog config.SolanaProgramConfig) string {
	global := effectiveGlobalDelivery(cfg)
	switch global {
	case deliveryHybrid:
		if v := strings.TrimSpace(prog.Delivery); v != "" {
			return normalizeDelivery(v)
		}
		return deliveryStream
	case deliveryWebhook:
		return deliveryWebhook
	default:
		return deliveryStream
	}
}

func webhookIngressActive(cfg config.SolanaConfig) bool {
	switch effectiveGlobalDelivery(cfg) {
	case deliveryWebhook:
		return true
	case deliveryHybrid:
		return cfg.Ingestion.Webhook.Enabled
	default:
		return false
	}
}

func normalizeDelivery(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case deliveryWebhook:
		return deliveryWebhook
	case deliveryHybrid:
		return deliveryHybrid
	default:
		return deliveryStream
	}
}
