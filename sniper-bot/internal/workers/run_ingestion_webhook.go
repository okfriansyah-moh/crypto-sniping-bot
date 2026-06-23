package workers

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/sniper-bot/internal/modules/ingestion_solana"
)

const defaultHeliusWebhookPath = "/webhooks/helius"

// HeliusWebhookDeps wires the webhook HTTP handler to the ingestion module.
type HeliusWebhookDeps struct {
	Module   *ingestion_solana.Module
	Cfg      config.SolanaConfig
	Logger   *slog.Logger
	MaxBytes int64
	Secret   string
}

// NewHeliusWebhookHandler returns POST handler for Helius transaction webhooks.
func NewHeliusWebhookHandler(deps HeliusWebhookDeps) http.Handler {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	maxBytes := deps.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 65536
	}
	secret := deps.Secret
	if secret == "" {
		secret = os.Getenv("HELIUS_WEBHOOK_SECRET")
	}

	var processed atomic.Int64

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if secret == "" {
			http.Error(w, "webhook not configured", http.StatusServiceUnavailable)
			return
		}
		if !authorizeHeliusWebhook(r, secret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if int64(len(body)) > maxBytes {
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
			return
		}

		notifs, err := parseHeliusWebhookBody(body)
		if err != nil {
			logger.Warn("helius_webhook_parse_failed", "error", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		emitted := 0
		for _, notif := range notifs {
			if notif.Transaction == nil {
				continue
			}
			for _, prog := range deps.Cfg.Programs {
				if prog.Disabled {
					continue
				}
				if ingestion_solana.EffectiveProgramDelivery(deps.Cfg, prog) != ingestion_solana.DeliveryWebhook {
					continue
				}
				if !transactionHasProgram(notif.Transaction, prog.ProgramID) {
					continue
				}
				if err := deps.Module.ProcessLogsNotification(ctx, notif, prog, "webhook"); err != nil {
					logger.Warn("helius_webhook_process_failed",
						"signature", notif.Signature,
						"family", prog.Family,
						"error", err,
					)
					continue
				}
				emitted++
			}
		}

		processed.Add(1)
		logger.Info("helius_webhook_delivered",
			"notifications", len(notifs),
			"programs_matched", emitted,
			"ingestion_delivery", ingestion_solana.EffectiveGlobalDelivery(deps.Cfg),
		)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func authorizeHeliusWebhook(r *http.Request, secret string) bool {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == secret {
		return true
	}
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:]) == secret
	}
	// Helius may also send the secret in a custom header.
	if h := strings.TrimSpace(r.Header.Get("X-Helius-Signature")); h == secret {
		return true
	}
	return false
}

// WebhookMaxBodyBytes returns the configured webhook body cap.
func WebhookMaxBodyBytes(cfg config.SolanaWebhookConfig) int64 {
	if cfg.MaxBodyBytes > 0 {
		return cfg.MaxBodyBytes
	}
	return 65536
}

// WebhookPath returns the configured webhook path with a safe default.
func WebhookPath(cfg config.SolanaWebhookConfig) string {
	p := strings.TrimSpace(cfg.Path)
	if p == "" {
		return defaultHeliusWebhookPath
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// ValidateWebhookBoot checks fail-fast requirements for webhook delivery mode.
func ValidateWebhookBoot(cfg config.SolanaConfig) error {
	if !ingestion_solana.WebhookIngressActive(cfg) {
		return nil
	}
	if os.Getenv("HELIUS_WEBHOOK_SECRET") == "" {
		return errMissingWebhookSecret
	}
	return nil
}

var errMissingWebhookSecret = &webhookBootError{msg: "HELIUS_WEBHOOK_SECRET required when ingestion delivery is webhook or hybrid with webhook.enabled"}

type webhookBootError struct{ msg string }

func (e *webhookBootError) Error() string { return e.msg }

func transactionHasProgram(tx *ingestion_solana.TransactionResult, programID string) bool {
	if tx == nil || programID == "" {
		return false
	}
	for _, instr := range tx.Instructions {
		if instr.ProgramID == programID {
			return true
		}
	}
	return false
}
