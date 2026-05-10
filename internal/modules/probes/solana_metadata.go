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

	hasLinks, err := parseSocialLinks(body)
	if err != nil {
		return in, fmt.Errorf("probes/solana_metadata: parse %q: %w", url, err)
	}

	p.logger.Debug("solana_metadata_probe",
		"token", in.TokenAddress,
		"uri", in.MetadataURI,
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
// one non-empty social-link field (twitter, telegram, or website) in any
// of the three known locations.
func parseSocialLinks(body []byte) (bool, error) {
	var meta pumpMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return false, fmt.Errorf("unmarshal: %w", err)
	}

	// 1. Top-level keys.
	if strings.TrimSpace(meta.Twitter) != "" ||
		strings.TrimSpace(meta.Telegram) != "" ||
		strings.TrimSpace(meta.Website) != "" {
		return true, nil
	}

	// 2. extensions object.
	for _, key := range []string{"twitter", "telegram", "website"} {
		if v, ok := meta.Extensions[key]; ok && strings.TrimSpace(v) != "" {
			return true, nil
		}
	}

	// 3. links object (catch-all).
	for _, v := range meta.Links {
		if strings.TrimSpace(v) != "" {
			return true, nil
		}
	}

	return false, nil
}
