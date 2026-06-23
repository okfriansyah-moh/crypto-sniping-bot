package execution_solana

// jito_bundle.go — P5 Jito bundle submission client.
//
// Jito is a Solana block engine that accepts bundles (groups of up to 5
// transactions) and guarantees atomic execution in the next block. Routing
// swaps through Jito reduces sandwich attacks and provides priority
// inclusion.
//
// Security rules:
//   - JITO_BUNDLE_URL and JITO_TIP_ACCOUNT are read from env — never from
//     config files or logs.
//   - Response body is capped at 64 KiB to prevent memory exhaustion.
//   - Timeout is bounded by config (submit_timeout_ms; default 2 s).
//   - enabled:false + shadow_mode:true by default — no real submissions
//     until operator explicitly sets enabled:true.
//
// Architecture rules:
//   - Pure module — no database imports.
//   - Fail-open: submission error → log + return error; caller decides
//     whether to fall back to regular RPC send.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"crypto-sniping-bot/internal/app/config"
)

const (
	jitoMaxResponseBytes = 64 * 1024 // 64 KiB
)

// JitoClient submits Solana transactions as Jito bundles.
type JitoClient struct {
	cfg        config.JitoConfig
	bundleURL  string // from JITO_BUNDLE_URL env var
	tipAccount string // from JITO_TIP_ACCOUNT env var
	httpClient *http.Client
	logger     *slog.Logger
}

// NewJitoClient constructs a JitoClient.
// bundleURL and tipAccount are read from JITO_BUNDLE_URL and JITO_TIP_ACCOUNT
// env vars respectively. Returns an error when enabled:true but env vars are absent.
func NewJitoClient(cfg config.JitoConfig, logger *slog.Logger) (*JitoClient, error) {
	if logger == nil {
		logger = slog.Default()
	}
	bundleURL := os.Getenv("JITO_BUNDLE_URL")
	tipAccount := os.Getenv("JITO_TIP_ACCOUNT")

	if cfg.Enabled && !cfg.ShadowMode {
		if bundleURL == "" {
			return nil, fmt.Errorf("jito_bundle: JITO_BUNDLE_URL env var required when jito.enabled=true")
		}
		// HIGH-01: enforce HTTPS to prevent transaction exposure via MITM.
		// Loopback addresses (127.x, localhost) are exempt — used only in tests.
		isLoopback := strings.HasPrefix(bundleURL, "http://127.") ||
			strings.HasPrefix(bundleURL, "http://localhost")
		if !strings.HasPrefix(bundleURL, "https://") && !isLoopback {
			return nil, fmt.Errorf("jito_bundle: JITO_BUNDLE_URL must use https scheme")
		}
		if tipAccount == "" {
			return nil, fmt.Errorf("jito_bundle: JITO_TIP_ACCOUNT env var required when jito.enabled=true")
		}
	}

	timeout := time.Duration(cfg.SubmitTimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	return &JitoClient{
		cfg:        cfg,
		bundleURL:  bundleURL,
		tipAccount: tipAccount,
		httpClient: &http.Client{Timeout: timeout},
		logger:     logger,
	}, nil
}

// jitoRPCRequest is the JSON-RPC 2.0 payload for Jito's sendBundle endpoint.
type jitoRPCRequest struct {
	Jsonrpc string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

// jitoRPCResponse is a minimal JSON-RPC 2.0 response stub.
type jitoRPCResponse struct {
	Result string `json:"result"` // bundle UUID on success
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// SubmitBundle sends up to MaxBundleSize signed base64 transactions as a
// Jito bundle.
//
// Shadow mode: logs the bundle but skips the HTTP call (returns nil).
// Disabled: returns nil immediately.
//
// The caller is responsible for including a tip transaction at the start or
// end of the txns slice (transfer of TipLamports to TipAccount).
func (c *JitoClient) SubmitBundle(ctx context.Context, txns []string) error {
	if !c.cfg.Enabled {
		return nil
	}

	maxSize := c.cfg.MaxBundleSize
	if maxSize <= 0 {
		maxSize = 5
	}
	if len(txns) > maxSize {
		return fmt.Errorf("jito_bundle: bundle size %d exceeds max %d", len(txns), maxSize)
	}

	if c.cfg.ShadowMode {
		c.logger.Info("jito_bundle_shadow",
			"tx_count", len(txns),
			"tip_lamports", c.cfg.TipLamports,
			"tip_account_configured", c.tipAccount != "",
			"bundle_url_configured", c.bundleURL != "",
		)
		return nil
	}

	if c.bundleURL == "" {
		return fmt.Errorf("jito_bundle: no bundle URL configured")
	}

	payload := jitoRPCRequest{
		Jsonrpc: "2.0",
		ID:      1,
		Method:  "sendBundle",
		Params:  []interface{}{txns},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("jito_bundle: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.bundleURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("jito_bundle: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("jito_bundle: http: %w", err)
	}
	defer resp.Body.Close()

	// Cap response body to prevent memory exhaustion.
	limited := io.LimitReader(resp.Body, jitoMaxResponseBytes)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("jito_bundle: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jito_bundle: http status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	var result jitoRPCResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("jito_bundle: unmarshal response: %w", err)
	}
	if result.Error != nil {
		// LOW-03: truncate error message to avoid log injection.
		return fmt.Errorf("jito_bundle: rpc error %d: %s", result.Error.Code, truncate(result.Error.Message, 200))
	}

	c.logger.Info("jito_bundle_submitted",
		"bundle_uuid", result.Result,
		"tx_count", len(txns),
		"tip_lamports", c.cfg.TipLamports,
	)
	return nil
}

// TipAccount returns the configured tip account address from env.
func (c *JitoClient) TipAccount() string { return c.tipAccount }

// TipLamports returns the configured tip amount.
func (c *JitoClient) TipLamports() int64 { return c.cfg.TipLamports }

// truncate shortens s to at most maxLen bytes.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
