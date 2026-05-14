package probes

// solana_metadata.go — HTTP metadata probe for pump.fun / Metaplex tokens.
//
// Pump.fun tokens store their off-chain metadata at the URI emitted in the
// CreateEvent log (field: uri). The JSON document is hosted on IPFS, Arweave,
// or a centralised HTTPS endpoint and follows the Metaplex NFT standard:
//
//	{
//	  "name": "RIBBIT",
//	  "symbol": "RIBBIT",
//	  "description": "...",
//	  "image": "...",
//	  "twitter":   "https://twitter.com/...",   ← may be absent or empty
//	  "telegram":  "https://t.me/...",           ← may be absent or empty
//	  "website":   "https://...",                ← may be absent or empty
//	  "extensions": {                            ← alternative placement
//	    "twitter":  "...",
//	    "telegram": "...",
//	    "website":  "..."
//	  }
//	}
//
// The probe checks three sources for social links, in priority order:
//  1. Top-level "twitter", "telegram", "website" keys.
//  2. "extensions" object (same keys).
//  3. Any non-empty "links" object value (catch-all for non-standard docs).
//
// If ANY non-empty social link is found → HasSocialLinks=true.
// SocialLinksKnown is always set to true on a successful fetch, even if
// HasSocialLinks=false — the absence of social links is real signal.
//
// On fetch failure (timeout, 4xx, 5xx, parse error) the probe returns
// (in, err) with SocialLinksKnown left false — the DQ layer degrades per
// the active operational-mode profile (STRICT treats unknown as half-risk).
//
// Design constraints (see probes.go):
//   - Pure-ish: no database calls.
//   - Safe with no configuration: if Enabled=false or MetadataURI="" → (in, nil).
//   - Testable with a fake HTTP client via SolanaMetadataHTTPClient.
//   - IPFS gateway is operator-configurable; defaults to the public cloudflare gateway.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"crypto-sniping-bot/contracts"
)

// SolanaMetadataHTTPClient is the minimal HTTP interface the probe needs.
// Implemented by *http.Client; injectable in tests via a fake.
type SolanaMetadataHTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// SolanaMetadataConfig configures the solana_metadata probe.
type SolanaMetadataConfig struct {
	Enabled      bool   `yaml:"enabled"`
	TimeoutMs    int    `yaml:"timeout_ms"`
	IPFSGateway  string `yaml:"ipfs_gateway"`   // e.g. "https://cloudflare-ipfs.com/ipfs/"
	MaxBodyBytes int64  `yaml:"max_body_bytes"` // defence against huge responses; default 64 KiB
}

// SolanaMetadataProbe fetches the off-chain metadata JSON for a token
// and populates SocialLinksKnown + HasSocialLinks on the DTO.
type SolanaMetadataProbe struct {
	cfg    SolanaMetadataConfig
	client SolanaMetadataHTTPClient
	logger *slog.Logger
}

// NewSolanaMetadataProbe creates a new probe. Pass nil client to use the
// default *http.Client with a per-request timeout derived from cfg.TimeoutMs.
func NewSolanaMetadataProbe(client SolanaMetadataHTTPClient, cfg SolanaMetadataConfig, logger *slog.Logger) *SolanaMetadataProbe {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.IPFSGateway == "" {
		cfg.IPFSGateway = "https://cloudflare-ipfs.com/ipfs/"
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = 64 * 1024 // 64 KiB
	}
	if client == nil {
		timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond
		if timeout <= 0 {
			timeout = 3000 * time.Millisecond
		}
		client = &http.Client{Timeout: timeout}
	}
	return &SolanaMetadataProbe{cfg: cfg, client: client, logger: logger}
}

func (p *SolanaMetadataProbe) Name() string { return "solana_metadata" }

// Probe fetches MetadataURI and populates SocialLinksKnown + HasSocialLinks.
//
//   - Non-Solana tokens: pass through unchanged (no HTTP call).
//   - Empty MetadataURI: SocialLinksKnown=true, HasSocialLinks=false.
//     An absent URI means the creator chose not to attach metadata — that is
//     itself a strong no-social signal, so we mark it as known+absent rather
//     than degrading to unknown.
//   - Fetch/parse error: return (in, err) with SocialLinksKnown=false so DQ
//     degrades per profile (STRICT treats unknown as half-risk).
func (p *SolanaMetadataProbe) Probe(ctx context.Context, in contracts.MarketDataDTO) (contracts.MarketDataDTO, error) {
	if !p.cfg.Enabled {
		return in, nil
	}
	if !strings.EqualFold(in.Chain, "solana") {
		return in, nil
	}

	// No URI on-chain → definitely no social links.
	if strings.TrimSpace(in.MetadataURI) == "" {
		p.logger.Info("solana_metadata_probe_no_uri",
			"token", in.TokenAddress,
			"social_links_known", true,
			"has_social_links", false,
		)
		out := in
		out.SocialLinksKnown = true
		out.HasSocialLinks = false
		return out, nil
	}

	url := resolveMetadataURL(in.MetadataURI, p.cfg.IPFSGateway)

	timeout := time.Duration(p.cfg.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 3000 * time.Millisecond
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return in, fmt.Errorf("probes/solana_metadata: build request for %q: %w", url, err)
	}
	req.Header.Set("User-Agent", "crypto-sniping-bot/metadata-probe")
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return in, fmt.Errorf("probes/solana_metadata: fetch %q: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return in, fmt.Errorf("probes/solana_metadata: HTTP %d for %q", resp.StatusCode, url)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, p.cfg.MaxBodyBytes))
	if err != nil {
		return in, fmt.Errorf("probes/solana_metadata: read body for %q: %w", url, err)
	}

	hasLinks, err := parseSocialLinks(body, in.Name, in.Symbol)
	if err != nil {
		return in, fmt.Errorf("probes/solana_metadata: parse %q: %w", url, err)
	}

	p.logger.Info("solana_metadata_probe",
		"token", in.TokenAddress,
		"uri", in.MetadataURI,
		"social_links_known", true,
		"has_social_links", hasLinks,
	)

	out := in
	out.SocialLinksKnown = true
	out.HasSocialLinks = hasLinks
	return out, nil
}

// resolveMetadataURL rewrites IPFS and Arweave URIs to HTTP gateway URLs.
// HTTPS and HTTP URIs are returned unchanged.
func resolveMetadataURL(rawURI, ipfsGateway string) string {
	u := strings.TrimSpace(rawURI)
	switch {
	case strings.HasPrefix(u, "ipfs://"):
		cid := strings.TrimPrefix(u, "ipfs://")
		gw := strings.TrimRight(ipfsGateway, "/")
		return gw + "/" + cid
	case strings.HasPrefix(u, "ar://"):
		ar := strings.TrimPrefix(u, "ar://")
		return "https://arweave.net/" + ar
	default:
		return u
	}
}

// pumpMetadata mirrors the top-level keys of the pump.fun / Metaplex
// off-chain metadata JSON. All social fields are optional.
type pumpMetadata struct {
	Twitter    string            `json:"twitter"`
	Telegram   string            `json:"telegram"`
	Website    string            `json:"website"`
	Extensions map[string]string `json:"extensions"`
	Links      map[string]string `json:"links"`
}

// parseSocialLinks returns true when the metadata JSON contains at least
// one non-empty, profile-level social-link field (twitter, telegram, or
// website) in any of the three known locations.
//
// A social link only qualifies when it points to an ACCOUNT PROFILE that is
// genuinely associated with this specific token. Two conditions must both hold:
//  1. The URL points to a real profile page (structural validation).
//  2. The profile handle/path contains the token name or symbol as a substring
//     (association validation). This rejects hijacked accounts (e.g. @baseapp
//     on a COOKING token) where a real company's profile is copied.
//
// tokenName and tokenSymbol are used only for the association check. When
// both are empty or too short (< 3 chars after normalisation) the association
// check is skipped to avoid false rejections on tokens with no name set.
func parseSocialLinks(body []byte, tokenName, tokenSymbol string) (bool, error) {
	var meta pumpMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return false, fmt.Errorf("unmarshal: %w", err)
	}

	// 1. Top-level keys — each link is validated as a real profile URL.
	if isSocialProfileURL("twitter", meta.Twitter, tokenName, tokenSymbol) ||
		isSocialProfileURL("telegram", meta.Telegram, tokenName, tokenSymbol) ||
		isSocialProfileURL("website", meta.Website, tokenName, tokenSymbol) {
		return true, nil
	}

	// 2. extensions object.
	for _, key := range []string{"twitter", "telegram", "website"} {
		if v, ok := meta.Extensions[key]; ok {
			if isSocialProfileURL(key, v, tokenName, tokenSymbol) {
				return true, nil
			}
		}
	}

	// 3. links object (catch-all) — only accept the known profile keys.
	// Without this filter, arbitrary keys (e.g. "discord", "github", or any
	// non-standard name) could satisfy HasSocialLinks with arbitrary values.
	for k, v := range meta.Links {
		key := strings.ToLower(strings.TrimSpace(k))
		switch key {
		case "twitter", "x", "telegram", "website":
			if isSocialProfileURL(key, v, tokenName, tokenSymbol) {
				return true, nil
			}
		}
	}

	return false, nil
}

// isSocialProfileURL returns true when rawURL is a non-empty URL that points
// to an account profile page genuinely associated with this token.
//
// Rules by socialType:
//   - "twitter" / "x": must have exactly one path segment (the username) on
//     twitter.com or x.com. Rejects posts (/status/), internal paths (/i/),
//     search (/search), intents (/intent/), and all other non-profile paths.
//   - "telegram": must be a parseable URL whose host is t.me, telegram.me, or
//     telegram.org. Any other host (including bare strings) is rejected.
//   - "website": must be a real external project website — a parseable
//     https:// URL that is not a DEX, scanner, launcher, blockchain explorer,
//     template-hosting platform, or social media platform.
//   - all other types: rejected (caller must pre-filter to a known key set).
//
// Association check: when tokenName or tokenSymbol is non-empty (≥ 3 chars
// normalised), the URL path/handle must contain the normalised token name as
// a substring. This rejects hijacked accounts from other projects/companies.
func isSocialProfileURL(socialType, rawURL, tokenName, tokenSymbol string) bool {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		return false
	}
	t := strings.ToLower(socialType)
	if t == "twitter" || t == "x" {
		if !isTwitterProfileURL(u) {
			return false
		}
		return profileAssociatedWithToken(u, tokenName, tokenSymbol)
	}

	// Telegram: must parse as URL with a known telegram host.
	if t == "telegram" {
		parsed, err := url.Parse(u)
		if err != nil {
			return false
		}
		host := strings.ToLower(parsed.Hostname())
		switch host {
		case "t.me", "telegram.me", "telegram.org":
			return true
		default:
			return false
		}
	}

	// Website: must be a parseable https URL on a non-blocked, non-social
	// host. Reject anything else (including bare strings and http URLs).
	if t == "website" {
		parsed, err := url.Parse(u)
		if err != nil {
			return false
		}
		if strings.ToLower(parsed.Scheme) != "https" {
			return false
		}
		if parsed.Hostname() == "" {
			return false
		}
		if isBlockedWebsiteDomain(u) {
			return false
		}
		if isSocialMediaWebsiteDomain(u) {
			return false
		}
		return true
	}

	// All other social types are not accepted as evidence of social presence.
	return false
}

// profileAssociatedWithToken returns true when the URL path/handle contains
// the normalised token name or symbol as a substring, confirming the social
// account belongs to this token and not a hijacked unrelated profile.
//
// Normalisation: lowercase, remove non-alphanumeric characters.
// Skip check when both identifiers normalise to fewer than 3 characters.
func profileAssociatedWithToken(rawURL, tokenName, tokenSymbol string) bool {
	normName := normaliseTokenIdentifier(tokenName)
	normSymbol := normaliseTokenIdentifier(tokenSymbol)

	// Skip association check if neither identifier is long enough — avoids
	// false rejections on tokens with very short or empty names/symbols.
	if len(normName) < 3 && len(normSymbol) < 3 {
		return true
	}

	// Normalise the URL using the same rules as token identifiers so handles
	// like "my-token" still match a token identifier normalised to "mytoken".
	normURL := normaliseTokenIdentifier(rawURL)

	if len(normName) >= 3 && strings.Contains(normURL, normName) {
		return true
	}
	if len(normSymbol) >= 3 && strings.Contains(normURL, normSymbol) {
		return true
	}
	return false
}

// normaliseTokenIdentifier lowercases and strips non-alphanumeric characters
// from a token name or symbol for fuzzy substring matching against URLs.
func normaliseTokenIdentifier(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// blockedWebsiteDomains lists DEX, scanner, launcher, explorer, and
// no-code website-builder domains that are never accepted as a real project
// website. Developers frequently copy-paste pump.fun token pages, Solscan
// links, DEX aggregator pages, or spin up generic Webflow/Carrd landing
// pages — these are not real project sites and must not satisfy the
// HasSocialLinks requirement.
var blockedWebsiteDomains = []string{
	"pump.fun",
	"solscan.io",
	"birdeye.so",
	"dexscreener.com",
	"raydium.io",
	"jup.ag",
	"jupiter.ag",
	"orca.so",
	"solanabeach.io",
	"explorer.solana.com",
	"solana.fm",
	"bubblemaps.io",
	"defined.fi",
	"geckoterminal.com",
	"coinmarketcap.com",
	"coingecko.com",
	"dextools.io",
	"ave.ai",
	"axiom.trade",
	"photon-sol.trycourier.app",
	"bullx.io",
	// No-code / template-hosting platforms: these host generic landing pages,
	// not real project websites. A webflow.io subdomain is a template, not a
	// team's own domain.
	"webflow.io",
	"carrd.co",
	"framer.app",
	"super.so",
	"notion.so",
	"my.canva.site",
}

// isBlockedWebsiteDomain returns true when rawURL's host equals or is a
// dot-bounded subdomain of any entry in blockedWebsiteDomains. Matching is
// host-only (via url.Parse) — query strings or paths that contain a blocked
// substring do not match. Malformed URLs and URLs with no host return false.
func isBlockedWebsiteDomain(rawURL string) bool {
	return hostMatchesDomainList(rawURL, blockedWebsiteDomains)
}

// socialMediaWebsiteDomains lists social-media and messaging-platform domains
// that are NOT acceptable as a token's project website. Developers sometimes
// place a Twitter profile or Telegram channel in the "website" metadata field
// instead of their actual project site. These must be rejected for the
// "website" type so that a real project URL is required.
//
// Note: Twitter/X is handled via isTwitterProfileURL for the "twitter" social
// type; this list is applied only when the metadata field type is "website".
var socialMediaWebsiteDomains = []string{
	"twitter.com",
	"x.com",
	"t.me",
	"telegram.me",
	"telegram.org",
	"discord.com",
	"discord.gg",
	"discordapp.com",
	"facebook.com",
	"fb.com",
	"instagram.com",
	"tiktok.com",
	"youtube.com",
	"youtu.be",
	"medium.com",
	"linktr.ee",
	"reddit.com",
	"bio.link",
}

// isSocialMediaWebsiteDomain returns true when rawURL's host equals or is a
// dot-bounded subdomain of any entry in socialMediaWebsiteDomains. Host-only
// match via url.Parse — substring matches in paths or query strings do not
// count. This blocks social-media URLs from satisfying the "website" metadata
// requirement.
func isSocialMediaWebsiteDomain(rawURL string) bool {
	return hostMatchesDomainList(rawURL, socialMediaWebsiteDomains)
}

// hostMatchesDomainList parses rawURL and returns true when its lowercase
// hostname is either equal to a domain in the list or a dot-bounded subdomain
// of one (e.g. host="app.dexscreener.com" matches domain="dexscreener.com",
// but host="mydexscreener.com" does not). Returns false for empty, malformed,
// or hostless URLs.
func hostMatchesDomainList(rawURL string, domains []string) bool {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		return false
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return false
	}
	for _, domain := range domains {
		d := strings.ToLower(domain)
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// isTwitterProfileURL returns true when rawURL points to a Twitter/X account
// profile page — exactly one path segment (the username) on a recognised
// Twitter/X host, not a tweet, search result, or internal redirect.
//
// Accepted:
//
//	https://twitter.com/myproject
//	https://x.com/myproject
//	https://www.twitter.com/myproject
//
// Rejected (examples):
//
//	https://x.com/elonmusk/status/1234          — tweet (two path segments)
//	https://twitter.com/search?q=bitcoin         — search results
//	https://x.com/i/web/status/123              — internal web-app redirect
//	https://x.com/intent/tweet?text=hello        — tweet compose intent
//	https://t.co/SomeHash                        — t.co short-link redirect
//	https://twitter.com/                         — root domain, no username
func isTwitterProfileURL(rawURL string) bool {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		return false
	}

	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}

	host := strings.ToLower(parsed.Hostname())

	// t.co is Twitter's own URL shortener — always a redirect to a tweet or
	// external link, never a profile page.
	if host == "t.co" {
		return false
	}

	// Must be twitter.com or x.com (accept www. prefix).
	if host != "twitter.com" && host != "www.twitter.com" &&
		host != "x.com" && host != "www.x.com" {
		return false
	}

	// Non-standard ports (e.g. :8080) are never valid Twitter profile URLs.
	// A rug-dev could set twitter="https://twitter.com:9090/fakehandle" to pass
	// host validation while the URL resolves to nothing on Twitter's network.
	if parsed.Port() != "" {
		return false
	}

	// Normalise path: strip the leading and trailing slashes.
	path := strings.Trim(parsed.Path, "/")
	if path == "" {
		// Root domain with no username — not a profile.
		return false
	}

	// A valid profile URL has EXACTLY one path segment (the username).
	// Multiple segments mean a tweet (/username/status/…), an internal path
	// (/i/web/…), or any other non-profile sub-path.
	if strings.Contains(path, "/") {
		return false
	}

	// Reject well-known reserved Twitter top-level paths that are not usernames.
	switch strings.ToLower(path) {
	case "i", "search", "intent", "explore", "hashtag", "home",
		"settings", "notifications", "messages", "help",
		"login", "signup", "logout", "about", "privacy", "tos":
		return false
	}

	// Twitter usernames never contain "@". A path like "fakeuser@evil.com"
	// has no slash so it passes the segment check, but "@" in the path
	// indicates a malformed or adversarial URL that is not a real profile.
	if strings.Contains(path, "@") {
		return false
	}

	return true
}
