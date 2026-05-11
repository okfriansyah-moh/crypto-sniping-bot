package probes

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"crypto-sniping-bot/contracts"
)

// fakeHTTPClient implements SolanaMetadataHTTPClient for tests.
type fakeHTTPClient struct {
	statusCode int
	body       string
	err        error
}

func (f *fakeHTTPClient) Do(_ *http.Request) (*http.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{
		StatusCode: f.statusCode,
		Body:       io.NopCloser(strings.NewReader(f.body)),
	}, nil
}

func defaultMetadataCfg() SolanaMetadataConfig {
	return SolanaMetadataConfig{
		Enabled:      true,
		TimeoutMs:    500,
		IPFSGateway:  "https://cloudflare-ipfs.com/ipfs/",
		MaxBodyBytes: 65536,
	}
}

func solanaDTO(uri string) contracts.MarketDataDTO {
	return contracts.MarketDataDTO{
		Chain:        "solana",
		TokenAddress: "So1Token1111111111111111111111111111111",
		MetadataURI:  uri,
	}
}

func TestSolanaMetadataProbe_Disabled(t *testing.T) {
	cfg := defaultMetadataCfg()
	cfg.Enabled = false
	p := NewSolanaMetadataProbe(&fakeHTTPClient{statusCode: 200, body: `{}`}, cfg, nil)
	in := solanaDTO("https://example.com/meta.json")
	out, err := p.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("want nil err when disabled, got %v", err)
	}
	if out.SocialLinksKnown {
		t.Fatal("want SocialLinksKnown=false when probe disabled")
	}
}

func TestSolanaMetadataProbe_NonSolanaPassthrough(t *testing.T) {
	p := NewSolanaMetadataProbe(&fakeHTTPClient{statusCode: 200, body: `{}`}, defaultMetadataCfg(), nil)
	in := contracts.MarketDataDTO{Chain: "eth", TokenAddress: "0xABC", MetadataURI: "https://example.com/meta.json"}
	out, err := p.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("want nil err for non-solana, got %v", err)
	}
	if out.SocialLinksKnown {
		t.Fatal("want SocialLinksKnown=false for non-solana passthrough")
	}
}

func TestSolanaMetadataProbe_EmptyURIKnownNoSocial(t *testing.T) {
	p := NewSolanaMetadataProbe(&fakeHTTPClient{statusCode: 200, body: `{}`}, defaultMetadataCfg(), nil)
	in := solanaDTO("") // no URI at all
	out, err := p.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("want nil err for empty URI, got %v", err)
	}
	if !out.SocialLinksKnown {
		t.Fatal("want SocialLinksKnown=true (no URI = known absent)")
	}
	if out.HasSocialLinks {
		t.Fatal("want HasSocialLinks=false (no URI = no socials)")
	}
}

func TestSolanaMetadataProbe_TopLevelTwitterPresent(t *testing.T) {
	body := `{"name":"TEST","twitter":"https://twitter.com/test"}`
	p := NewSolanaMetadataProbe(&fakeHTTPClient{statusCode: 200, body: body}, defaultMetadataCfg(), nil)
	out, err := p.Probe(context.Background(), solanaDTO("https://example.com/m.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.SocialLinksKnown {
		t.Fatal("want SocialLinksKnown=true")
	}
	if !out.HasSocialLinks {
		t.Fatal("want HasSocialLinks=true when twitter present")
	}
}

func TestSolanaMetadataProbe_ExtensionsTelegram(t *testing.T) {
	body := `{"name":"TEST","extensions":{"telegram":"https://t.me/test"}}`
	p := NewSolanaMetadataProbe(&fakeHTTPClient{statusCode: 200, body: body}, defaultMetadataCfg(), nil)
	out, err := p.Probe(context.Background(), solanaDTO("https://example.com/m.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.HasSocialLinks {
		t.Fatal("want HasSocialLinks=true when extensions.telegram present")
	}
}

func TestSolanaMetadataProbe_NoSocialLinksKnownFalse(t *testing.T) {
	body := `{"name":"RIBBIT","symbol":"RIBBIT","description":"frog coin"}`
	p := NewSolanaMetadataProbe(&fakeHTTPClient{statusCode: 200, body: body}, defaultMetadataCfg(), nil)
	out, err := p.Probe(context.Background(), solanaDTO("https://example.com/m.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.SocialLinksKnown {
		t.Fatal("want SocialLinksKnown=true after successful fetch")
	}
	if out.HasSocialLinks {
		t.Fatal("want HasSocialLinks=false when no social fields present")
	}
}

func TestSolanaMetadataProbe_HTTP404ReturnsError(t *testing.T) {
	p := NewSolanaMetadataProbe(&fakeHTTPClient{statusCode: 404, body: "not found"}, defaultMetadataCfg(), nil)
	out, err := p.Probe(context.Background(), solanaDTO("https://example.com/m.json"))
	if err == nil {
		t.Fatal("want error on HTTP 404")
	}
	if out.SocialLinksKnown {
		t.Fatal("want SocialLinksKnown=false on fetch error")
	}
}

func TestSolanaMetadataProbe_IPFSURLRewritten(t *testing.T) {
	var capturedURL string
	fake := &capturingHTTPClient{
		body:    `{"twitter":"https://twitter.com/test"}`,
		status:  200,
		capture: func(u string) { capturedURL = u },
	}
	p := NewSolanaMetadataProbe(fake, defaultMetadataCfg(), nil)
	_, _ = p.Probe(context.Background(), solanaDTO("ipfs://QmABCDEF1234"))
	if !strings.HasPrefix(capturedURL, "https://cloudflare-ipfs.com/ipfs/") {
		t.Fatalf("want IPFS gateway URL, got %q", capturedURL)
	}
}

func TestSolanaMetadataProbe_ArweaveURLRewritten(t *testing.T) {
	var capturedURL string
	fake := &capturingHTTPClient{
		body:    `{}`,
		status:  200,
		capture: func(u string) { capturedURL = u },
	}
	p := NewSolanaMetadataProbe(fake, defaultMetadataCfg(), nil)
	_, _ = p.Probe(context.Background(), solanaDTO("ar://SomeArweaveTxID"))
	if !strings.HasPrefix(capturedURL, "https://arweave.net/") {
		t.Fatalf("want Arweave gateway URL, got %q", capturedURL)
	}
}

func TestSolanaMetadataProbe_Determinism(t *testing.T) {
	body := `{"name":"T","twitter":"https://twitter.com/t"}`
	p := NewSolanaMetadataProbe(&fakeHTTPClient{statusCode: 200, body: body}, defaultMetadataCfg(), nil)
	in := solanaDTO("https://example.com/m.json")
	first, _ := p.Probe(context.Background(), in)
	second, _ := p.Probe(context.Background(), in)
	if first.HasSocialLinks != second.HasSocialLinks || first.SocialLinksKnown != second.SocialLinksKnown {
		t.Fatal("non-deterministic probe results")
	}
}

// ── Profile URL validation tests ──────────────────────────────────────────────

// TestParseSocialLinks_TwitterPostIsRejected verifies that a Twitter URL
// pointing to a post/tweet (containing "/status/") is NOT counted as a valid
// social link. This was the exact exploit vector for the "test" rug token that
// set twitter to an Elon Musk tweet to pass the HasSocialLinks check.
func TestParseSocialLinks_TwitterPostIsRejected(t *testing.T) {
	// Exactly the pattern observed in the gate evidence: viral Elon tweet used
	// as the "twitter" metadata field.
	body := `{"twitter":"https://x.com/elonmusk/status/1920908498099810362"}`
	has, err := parseSocialLinks([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false for Twitter post URL (not a profile)")
	}
}

func TestParseSocialLinks_TwitterProfileIsAccepted(t *testing.T) {
	body := `{"twitter":"https://twitter.com/myproject"}`
	has, err := parseSocialLinks([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatal("want HasSocialLinks=true for valid Twitter profile URL")
	}
}

func TestParseSocialLinks_XProfileIsAccepted(t *testing.T) {
	body := `{"twitter":"https://x.com/myproject"}`
	has, err := parseSocialLinks([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatal("want HasSocialLinks=true for valid X.com profile URL")
	}
}

func TestParseSocialLinks_TcoShortlinkIsRejected(t *testing.T) {
	body := `{"twitter":"https://t.co/abc123"}`
	has, err := parseSocialLinks([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false for t.co shortlink (tweet redirect, not profile)")
	}
}

func TestParseSocialLinks_ExtensionsTwitterPostIsRejected(t *testing.T) {
	body := `{"extensions":{"twitter":"https://x.com/user/status/9999"}}`
	has, err := parseSocialLinks([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false when extensions.twitter is a post URL")
	}
}

func TestParseSocialLinks_TelegramProfileIsAccepted(t *testing.T) {
	body := `{"telegram":"https://t.me/myproject_official"}`
	has, err := parseSocialLinks([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatal("want HasSocialLinks=true for Telegram channel link")
	}
}

func TestParseSocialLinks_WebsiteIsAccepted(t *testing.T) {
	body := `{"website":"https://myproject.io"}`
	has, err := parseSocialLinks([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatal("want HasSocialLinks=true for project website")
	}
}

// TestParseSocialLinks_PumpFunWebsiteIsRejected verifies that a "website" field
// pointing to the pump.fun launcher (a common pattern in rug tokens that use
// their own pump.fun token page as their "project website") is NOT counted as
// a valid social link. This closes the fake-website exploit vector observed in
// the pussy/pussycoin token (7a7Kukc9mnsjvt5RwuRTG2WX4kXvhmnBNQRNF7AYpum).
func TestParseSocialLinks_PumpFunWebsiteIsRejected(t *testing.T) {
	body := `{"website":"https://pump.fun/coin/7a7Kukc9mnsjvt5RwuRTG2WX4kXvhmnBNQRNF7AYpum"}`
	has, err := parseSocialLinks([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false when website is a pump.fun token page (not a project)")
	}
}

func TestParseSocialLinks_DEXScreenerWebsiteIsRejected(t *testing.T) {
	body := `{"website":"https://dexscreener.com/solana/ABCDEF"}`
	has, err := parseSocialLinks([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false when website is a DEX scanner page")
	}
}

func TestParseSocialLinks_BirdeyeWebsiteIsRejected(t *testing.T) {
	body := `{"website":"https://birdeye.so/token/ABCDEF"}`
	has, err := parseSocialLinks([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false when website is birdeye.so scanner link")
	}
}

func TestIsBlockedWebsiteDomain(t *testing.T) {
	cases := []struct {
		url   string
		want  bool
		label string
	}{
		{"https://pump.fun/coin/abc", true, "pump.fun token page"},
		{"https://dexscreener.com/solana/abc", true, "dexscreener"},
		{"https://birdeye.so/token/abc", true, "birdeye"},
		{"https://solscan.io/token/abc", true, "solscan explorer"},
		{"https://raydium.io/swap", true, "raydium DEX"},
		{"https://jup.ag/swap", true, "jupiter aggregator"},
		{"https://geckoterminal.com/sol/pools/abc", true, "geckoterminal"},
		{"https://axiom.trade/t/abc", true, "axiom trade"},
		{"https://myproject.io", false, "real project site"},
		{"https://projectname.xyz/token", false, "custom project domain"},
		{"https://github.com/myproject", false, "github project"},
		{"https://t.me/myproject", false, "telegram link (not a website type)"},
		{"", false, "empty URL"},
	}
	for _, c := range cases {
		got := isBlockedWebsiteDomain(c.url)
		if got != c.want {
			t.Errorf("isBlockedWebsiteDomain(%q) [%s] = %v, want %v", c.url, c.label, got, c.want)
		}
	}
}

func TestParseSocialLinks_AllPostURLsReturnFalse(t *testing.T) {
	// All three fields present but ALL are post/redirect URLs — still false.
	body := `{
		"twitter":"https://x.com/elonmusk/status/1234",
		"telegram":"",
		"website":""
	}`
	has, err := parseSocialLinks([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false when all social URLs are post links")
	}
}

func TestIsTwitterProfileURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://twitter.com/myproject", true},
		{"https://x.com/myproject", true},
		{"https://x.com/elonmusk/status/1920908498099810362", false},
		{"https://twitter.com/user/status/123456", false},
		{"https://t.co/SomeHash", false},
		{"", false},
		{"https://telegram.org/something", false}, // not twitter domain
	}
	for _, c := range cases {
		got := isTwitterProfileURL(c.url)
		if got != c.want {
			t.Errorf("isTwitterProfileURL(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

type capturingHTTPClient struct {
	body    string
	status  int
	capture func(string)
}

func (c *capturingHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if c.capture != nil {
		c.capture(req.URL.String())
	}
	return &http.Response{
		StatusCode: c.status,
		Body:       io.NopCloser(strings.NewReader(c.body)),
	}, nil
}
