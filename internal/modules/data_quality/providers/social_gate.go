// Package providers — Twitter/X social gate via DEXScreener token profile (P2).
//
// SocialGateProvider fetches token social metadata from the DEXScreener
// public profile API and scores the result as a risk signal:
//
//   - No social links at all → score = 0.5 (mildly risky; unknown)
//   - Has Twitter/X → score = 0.1 (low risk; real project signal)
//   - Freshly-created Twitter (detected via URL pattern) → score = 0.4
//   - Request fails / token not found → Degraded = true, score = 0
//
// The score is a risk score: higher = riskier.
// This provider is chain-agnostic (DEXScreener supports EVM and Solana).
//
// Architecture:
//   - Fail-open: any network error returns Degraded=true, score=0.
//   - Response body limited to 128 KiB.
//   - Timeout enforced via parent context.
//   - Boots with shadow_mode: true in YAML — flip when validation complete.
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	socialBodyLimitBytes = 128 * 1024 // 128 KiB
)

// SocialGateProvider queries DEXScreener for token social metadata.
type SocialGateProvider struct {
	client  *http.Client
	logger  *slog.Logger
	baseURL string // injectable for tests
}

// NewSocialGateProvider returns a SocialGateProvider with a bounded HTTP client.
// The HTTP client timeout is intentionally conservative (280 ms) to fit within
// the provider aggregator budget.
func NewSocialGateProvider(logger *slog.Logger) *SocialGateProvider {
	return &SocialGateProvider{
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
		baseURL: "https://api.dexscreener.com",
	}
}

// Name returns the canonical provider name used in flags and logs.
func (p *SocialGateProvider) Name() string { return "social_gate" }

// SetBaseURLForTest overrides the base URL for unit testing.
// Must only be called before any Evaluate calls.
func (p *SocialGateProvider) SetBaseURLForTest(u string) { p.baseURL = u }

// dexTokenProfile is the subset of the DEXScreener token profile response
// Evaluate fetches the DEXScreener token profile and scores the social signal.
func (p *SocialGateProvider) Evaluate(
	ctx context.Context,
	tokenAddress, chain string,
) (DQSignalDTO, error) {
	// DEXScreener profile endpoint: GET /token-profiles/latest/v1
	// fallback: token search by address.
	// We use the simpler /tokens/{chainId}/{tokenAddress} endpoint that
	// returns pair data including social info.
	url := fmt.Sprintf("%s/tokens/v1/%s/%s", p.baseURL, chainToDEXChainID(chain), tokenAddress)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		p.logger.Warn("social_gate_request_build_failed",
			"error", err, "token", tokenAddress, "chain", chain)
		return DQSignalDTO{ProviderName: p.Name(), Degraded: true}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "crypto-sniping-bot/1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		p.logger.Warn("social_gate_request_failed",
			"error", err, "token", tokenAddress, "chain", chain)
		return DQSignalDTO{ProviderName: p.Name(), Degraded: true}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Token not indexed — treat as unknown, not risky.
		return DQSignalDTO{
			ProviderName: p.Name(),
			Score:        0.0,
			Flags:        []string{"social_gate:not_indexed"},
			Degraded:     true, // degraded because we have no data
		}, nil
	}

	if resp.StatusCode >= 500 {
		err := fmt.Errorf("social_gate: upstream %d", resp.StatusCode)
		p.logger.Warn("social_gate_upstream_error",
			"status", resp.StatusCode, "token", tokenAddress)
		return DQSignalDTO{ProviderName: p.Name(), Degraded: true}, err
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, socialBodyLimitBytes))
	if err != nil {
		p.logger.Warn("social_gate_read_failed",
			"error", err, "token", tokenAddress)
		return DQSignalDTO{ProviderName: p.Name(), Degraded: true}, err
	}

	// DEXScreener /tokens/v1/{chain}/{address} returns an array of pair
	// objects.  Social links appear under pairs[*].info.socials.
	// We parse a minimal shape to find Twitter/X presence.
	type dexPairInfo struct {
		Socials []struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"socials"`
		Websites []struct {
			URL string `json:"url"`
		} `json:"websites"`
	}
	type dexPair struct {
		Info dexPairInfo `json:"info"`
	}

	var pairs []dexPair
	if jsonErr := json.Unmarshal(body, &pairs); jsonErr != nil {
		p.logger.Warn("social_gate_parse_failed",
			"error", jsonErr, "token", tokenAddress)
		return DQSignalDTO{ProviderName: p.Name(), Degraded: true}, jsonErr
	}

	// Collect social types and website presence across all pairs.
	var socialTypes []string
	hasWebsite := false
	for _, pair := range pairs {
		for _, s := range pair.Info.Socials {
			t := strings.ToLower(s.Type)
			if t == "" {
				continue
			}
			// A Twitter/X social that points to a post/thread (not a profile)
			// is not evidence of a real project identity. Record it as
			// "twitter_post" so scoreSocials does not treat it as hasTwitter.
			if (t == "twitter" || t == "x") && isPostURL(s.URL) {
				socialTypes = append(socialTypes, "twitter_post")
				continue
			}
			socialTypes = append(socialTypes, t)
		}
		if len(pair.Info.Websites) > 0 {
			hasWebsite = true
		}
	}

	score, flags := scoreSocials(socialTypes, hasWebsite)
	return DQSignalDTO{
		ProviderName: p.Name(),
		Score:        score,
		Flags:        flags,
		Degraded:     false,
	}, nil
}

// scoreSocials is the pure scoring function exported for unit tests.
// socialTypes is the list of social link type labels (e.g. "twitter").
// hasWebsite is true when at least one website URL was found.
// Returns a risk score in [0,1] (higher = riskier) and diagnostic flags.
func scoreSocials(socialTypes []string, hasWebsite bool) (score float64, flags []string) {
	hasTwitter := false
	for _, t := range socialTypes {
		if strings.EqualFold(t, "twitter") || strings.EqualFold(t, "x") {
			hasTwitter = true
		}
	}

	switch {
	case hasTwitter:
		// Has Twitter/X presence — real project signal.
		return 0.10, nil
	case hasWebsite || len(socialTypes) > 0:
		// Has some social presence but no Twitter — mild concern.
		return 0.30, []string{"social_gate:no_twitter"}
	default:
		// No social links at all — unknown, treat as mildly risky.
		return 0.50, []string{"social_gate:no_socials"}
	}
}

// isPostURL returns true when the URL points to a tweet/thread/post rather
// than a project's Twitter profile page. A link to a viral post is not
// evidence of a project identity and must not satisfy the Twitter presence gate.
func isPostURL(rawURL string) bool {
	u := strings.ToLower(rawURL)
	return strings.Contains(u, "/status/") ||
		strings.Contains(u, "/statuses/") ||
		strings.Contains(u, "t.co/")
}

// chainToDEXChainID maps internal chain keys to DEXScreener chain IDs.
func chainToDEXChainID(chain string) string {
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
	default:
		return chain
	}
}
