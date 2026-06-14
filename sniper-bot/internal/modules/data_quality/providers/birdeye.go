// Package providers — BirdEye token security provider (P3).
//
// BirdEyeProvider calls the BirdEye public API to fetch token security metadata:
//   - Creator wallet holding percentage → creator dump risk
//   - Top-10 holder concentration → market manipulation risk
//   - Mint / freeze authority presence → on-chain risk
//   - LP lock percentage → rug pull risk
//
// Risk score (higher = riskier):
//
//	score = 0.40 × concentrationRisk + 0.35 × creatorRisk + 0.25 × authorityRisk
//
// API key is required and must be set via environment variable BIRDEYE_API_KEY.
// If the key is absent or empty, the provider returns Degraded=true without panic.
//
// Architecture:
//   - Fail-open: any network or parse error returns Degraded=true, score=0.
//   - Response body limited to 128 KiB.
//   - Timeout enforced via parent context.
//   - Boots with shadow_mode: true in config — flip when shadow validation passes.
//   - API key is NEVER logged, stored, or included in error messages.
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	birdeyeBodyLimitBytes = 128 * 1024 // 128 KiB
	birdeyeAPIKeyEnv      = "BIRDEYE_API_KEY"
)

// BirdEyeProvider queries the BirdEye API for token security metadata.
type BirdEyeProvider struct {
	client  *http.Client
	logger  *slog.Logger
	baseURL string // injectable for tests
	apiKey  string // from env BIRDEYE_API_KEY, never logged
}

// NewBirdEyeProvider returns a BirdEyeProvider.
// The API key is loaded from os.Getenv("BIRDEYE_API_KEY").
// If no API key is configured the provider returns Degraded=true on every call
// without causing a build failure or panic.
func NewBirdEyeProvider(logger *slog.Logger) *BirdEyeProvider {
	if logger == nil {
		logger = slog.Default()
	}
	apiKey := os.Getenv(birdeyeAPIKeyEnv)
	if apiKey == "" {
		logger.Warn("birdeye_no_api_key",
			"env", birdeyeAPIKeyEnv,
			"hint", "set BIRDEYE_API_KEY to enable this provider",
		)
	}
	return &BirdEyeProvider{
		client: &http.Client{
			Timeout: 280 * time.Millisecond,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout:     30 * time.Second,
				DisableKeepAlives:   false,
			},
		},
		logger:  logger,
		baseURL: "https://public-api.birdeye.so",
		apiKey:  apiKey,
	}
}

// Name returns the canonical provider name.
func (p *BirdEyeProvider) Name() string { return "birdeye" }

// SetBaseURLForTest overrides the base URL for unit testing.
func (p *BirdEyeProvider) SetBaseURLForTest(u string) { p.baseURL = u }

// SetAPIKeyForTest injects a test API key.
// Must only be called before any Evaluate calls.
func (p *BirdEyeProvider) SetAPIKeyForTest(key string) { p.apiKey = key }

// birdeyeSecurityResponse is the minimal shape of the BirdEye token_security API.
type birdeyeSecurityResponse struct {
	Success bool              `json:"success"`
	Data    birdeyeSecDataRaw `json:"data"`
}

type birdeyeSecDataRaw struct {
	OwnerPercentage    float64 `json:"ownerPercentage"`
	CreatorPercentage  float64 `json:"creatorPercentage"`
	Top10HolderPercent float64 `json:"top10HolderPercent"`
	FreezeAuthority    *string `json:"freezeAuthority"` // nil when not set
	MintAuthority      *string `json:"mintAuthority"`   // nil when not set
	IsMutable          bool    `json:"isMutable"`

	// Markets contains DEX pair data including LP lock info.
	// We read only the first entry's LP lock percentage.
	Markets []birdeyeMarketRaw `json:"markets"`
}

type birdeyeMarketRaw struct {
	LP struct {
		LpLockedPct float64 `json:"lpLockedPct"`
	} `json:"lp"`
}

// Evaluate fetches the BirdEye token security profile and returns a risk signal.
func (p *BirdEyeProvider) Evaluate(
	ctx context.Context,
	tokenAddress, chain string,
) (DQSignalDTO, error) {
	if p.apiKey == "" {
		// No API key — degrade silently rather than waste HTTP budget.
		return DQSignalDTO{
			ProviderName: p.Name(),
			Score:        0,
			Flags:        []string{"birdeye:no_api_key"},
			Degraded:     true,
		}, nil
	}

	chainID := birdeyeChainID(chain)
	url := fmt.Sprintf("%s/defi/token_security?address=%s", p.baseURL, tokenAddress)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		p.logger.Warn("birdeye_request_build_failed",
			"error", err, "token", tokenAddress, "chain", chain)
		return DQSignalDTO{ProviderName: p.Name(), Degraded: true}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "crypto-sniping-bot/1.0")
	req.Header.Set("X-API-KEY", p.apiKey) // API key sent in header, never logged
	req.Header.Set("x-chain", chainID)

	resp, err := p.client.Do(req)
	if err != nil {
		p.logger.Warn("birdeye_request_failed",
			"error", err, "token", tokenAddress, "chain", chain)
		return DQSignalDTO{ProviderName: p.Name(), Degraded: true}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return DQSignalDTO{
			ProviderName: p.Name(),
			Score:        0,
			Flags:        []string{"birdeye:not_found"},
			Degraded:     true,
		}, nil
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		// Do NOT log the key — just signal degraded.
		p.logger.Warn("birdeye_auth_failed",
			"status", resp.StatusCode,
			"hint", "check BIRDEYE_API_KEY",
		)
		return DQSignalDTO{
			ProviderName: p.Name(),
			Flags:        []string{"birdeye:auth_failed"},
			Degraded:     true,
		}, fmt.Errorf("birdeye: auth failed (%d)", resp.StatusCode)
	}

	if resp.StatusCode >= 500 {
		p.logger.Warn("birdeye_upstream_error",
			"status", resp.StatusCode, "token", tokenAddress)
		return DQSignalDTO{ProviderName: p.Name(), Degraded: true},
			fmt.Errorf("birdeye: upstream %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, birdeyeBodyLimitBytes))
	if err != nil {
		p.logger.Warn("birdeye_read_failed",
			"error", err, "token", tokenAddress)
		return DQSignalDTO{ProviderName: p.Name(), Degraded: true}, err
	}

	var apiResp birdeyeSecurityResponse
	if jsonErr := json.Unmarshal(body, &apiResp); jsonErr != nil {
		p.logger.Warn("birdeye_parse_failed",
			"error", jsonErr, "token", tokenAddress)
		return DQSignalDTO{ProviderName: p.Name(), Degraded: true}, jsonErr
	}

	if !apiResp.Success {
		p.logger.Warn("birdeye_api_not_success",
			"token", tokenAddress, "chain", chain)
		return DQSignalDTO{
			ProviderName: p.Name(),
			Flags:        []string{"birdeye:api_not_success"},
			Degraded:     true,
		}, nil
	}

	score, flags, creatorRisk, lpLock := scoreBirdEye(apiResp.Data)

	return DQSignalDTO{
		ProviderName:     p.Name(),
		Score:            score,
		Flags:            flags,
		Degraded:         false,
		CreatorRiskScore: creatorRisk,
		LpLockPct:        lpLock,
	}, nil
}

// scoreBirdEye converts raw BirdEye security data to a risk score.
// Returns: score [0,1], flags, creatorRiskScore [0,1], lpLockPct [0,100].
//
// Scoring weights:
//
//	concentrationRisk  × 0.40
//	creatorRisk        × 0.35
//	authorityRisk      × 0.25
func scoreBirdEye(d birdeyeSecDataRaw) (score float64, flags []string, creatorRisk float64, lpLockPct float64) {
	// ── 1. Concentration risk (top10 holder %) ─────────────────────────
	var concentrationRisk float64
	switch {
	case d.Top10HolderPercent >= 0.80:
		concentrationRisk = 0.90
		flags = append(flags, "birdeye:concentration_critical")
	case d.Top10HolderPercent >= 0.60:
		concentrationRisk = 0.70
		flags = append(flags, "birdeye:concentration_high")
	case d.Top10HolderPercent >= 0.40:
		concentrationRisk = 0.40
		flags = append(flags, "birdeye:concentration_medium")
	default:
		concentrationRisk = 0.15
	}

	// ── 2. Creator risk (creator holding %) ────────────────────────────
	// Use the max of ownerPercentage and creatorPercentage.
	maxCreatorPct := d.CreatorPercentage
	if d.OwnerPercentage > maxCreatorPct {
		maxCreatorPct = d.OwnerPercentage
	}
	switch {
	case maxCreatorPct >= 0.20:
		creatorRisk = 0.90
		flags = append(flags, "birdeye:creator_high")
	case maxCreatorPct >= 0.10:
		creatorRisk = 0.70
		flags = append(flags, "birdeye:creator_medium")
	case maxCreatorPct >= 0.05:
		creatorRisk = 0.50
		flags = append(flags, "birdeye:creator_elevated")
	default:
		creatorRisk = 0.10
	}

	// ── 3. Authority risk ───────────────────────────────────────────────
	var authorityRisk float64
	if d.MintAuthority != nil && *d.MintAuthority != "" {
		authorityRisk = 0.80
		flags = append(flags, "birdeye:mint_authority_set")
	} else if d.FreezeAuthority != nil && *d.FreezeAuthority != "" {
		authorityRisk = 0.60
		flags = append(flags, "birdeye:freeze_authority_set")
	} else if d.IsMutable {
		authorityRisk = 0.30
		flags = append(flags, "birdeye:metadata_mutable")
	}

	// ── 4. LP lock percentage ───────────────────────────────────────────
	if len(d.Markets) > 0 {
		lpLockPct = d.Markets[0].LP.LpLockedPct
		if lpLockPct < 50 {
			flags = append(flags, "birdeye:lp_unlocked")
		}
	}

	// ── 5. Composite score ──────────────────────────────────────────────
	score = 0.40*concentrationRisk + 0.35*creatorRisk + 0.25*authorityRisk
	if score > 1.0 {
		score = 1.0
	}
	if score < 0 {
		score = 0
	}

	return score, flags, creatorRisk, lpLockPct
}

// birdeyeChainID maps internal chain keys to BirdEye chain IDs.
func birdeyeChainID(chain string) string {
	switch strings.ToLower(chain) {
	case "eth", "ethereum":
		return "ethereum"
	case "bsc", "binance":
		return "bsc"
	case "solana", "sol":
		return "solana"
	case "polygon", "matic":
		return "polygon"
	case "arbitrum", "arb":
		return "arbitrum"
	case "base":
		return "base"
	case "avalanche", "avax":
		return "avalanche"
	case "optimism", "op":
		return "optimism"
	case "sui":
		return "sui"
	default:
		return chain
	}
}
