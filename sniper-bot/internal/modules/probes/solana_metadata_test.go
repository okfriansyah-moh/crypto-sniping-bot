package probes

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"crypto-sniping-bot/shared/contracts"
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
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false for Twitter post URL (not a profile)")
	}
}

func TestParseSocialLinks_TwitterProfileIsAccepted(t *testing.T) {
	body := `{"twitter":"https://twitter.com/myproject"}`
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatal("want HasSocialLinks=true for valid Twitter profile URL")
	}
}

func TestParseSocialLinks_XProfileIsAccepted(t *testing.T) {
	body := `{"twitter":"https://x.com/myproject"}`
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatal("want HasSocialLinks=true for valid X.com profile URL")
	}
}

func TestParseSocialLinks_TcoShortlinkIsRejected(t *testing.T) {
	body := `{"twitter":"https://t.co/abc123"}`
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false for t.co shortlink (tweet redirect, not profile)")
	}
}

func TestParseSocialLinks_ExtensionsTwitterPostIsRejected(t *testing.T) {
	body := `{"extensions":{"twitter":"https://x.com/user/status/9999"}}`
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false when extensions.twitter is a post URL")
	}
}

func TestParseSocialLinks_TelegramProfileIsAccepted(t *testing.T) {
	body := `{"telegram":"https://t.me/myproject_official"}`
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatal("want HasSocialLinks=true for Telegram channel link")
	}
}

func TestParseSocialLinks_WebsiteIsAccepted(t *testing.T) {
	body := `{"website":"https://myproject.io"}`
	has, err := parseSocialLinks([]byte(body), "", "")
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
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false when website is a pump.fun token page (not a project)")
	}
}

func TestParseSocialLinks_DEXScreenerWebsiteIsRejected(t *testing.T) {
	body := `{"website":"https://dexscreener.com/solana/ABCDEF"}`
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false when website is a DEX scanner page")
	}
}

func TestParseSocialLinks_BirdeyeWebsiteIsRejected(t *testing.T) {
	body := `{"website":"https://birdeye.so/token/ABCDEF"}`
	has, err := parseSocialLinks([]byte(body), "", "")
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
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false when all social URLs are post links")
	}
}

func TestIsTwitterProfileURL(t *testing.T) {
	cases := []struct {
		url   string
		want  bool
		label string
	}{
		// ── Valid profile URLs ────────────────────────────────────────────
		{"https://twitter.com/myproject", true, "twitter.com profile"},
		{"https://x.com/myproject", true, "x.com profile"},
		{"https://www.twitter.com/myproject", true, "www.twitter.com profile"},
		{"https://www.x.com/myproject", true, "www.x.com profile"},

		// ── Tweet / post URLs ─────────────────────────────────────────────
		{"https://x.com/elonmusk/status/1920908498099810362", false, "x.com tweet (status)"},
		{"https://twitter.com/user/status/123456", false, "twitter.com tweet (status)"},

		// ── Twitter internal / redirect paths ────────────────────────────
		// x.com/i/web/... is the Twitter web-app internal redirect URL pattern,
		// NOT a user profile. Previously passed the string-based check.
		{"https://x.com/i/web/status/123456789", false, "x.com /i/web/ redirect"},
		{"https://x.com/i/web/", false, "x.com /i/ root"},
		{"https://x.com/i", false, "x.com /i reserved path"},

		// ── Search and intent URLs ────────────────────────────────────────
		// twitter.com/search?q=... is a search results page, not a profile.
		// Previously passed the string-based check.
		{"https://twitter.com/search?q=bitcoin", false, "twitter.com search page"},
		{"https://x.com/search?q=crypto", false, "x.com search page"},
		// x.com/intent/tweet is the tweet-compose intent URL.
		{"https://x.com/intent/tweet?text=hello", false, "x.com intent URL"},
		{"https://twitter.com/intent/follow?screen_name=user", false, "twitter.com intent URL"},

		// ── Other reserved / non-profile paths ───────────────────────────
		{"https://twitter.com/explore", false, "explore page"},
		{"https://x.com/home", false, "home page"},
		{"https://x.com/settings/account", false, "settings page (sub-path)"},
		{"https://twitter.com/hashtag/bitcoin", false, "hashtag timeline (sub-path)"},

		// ── Root domain without username ──────────────────────────────────
		{"https://twitter.com/", false, "twitter.com root (no username)"},
		{"https://x.com/", false, "x.com root (no username)"},
		{"https://twitter.com", false, "twitter.com bare root"},

		// ── Security: port bypass (BLOCKER fix) ──────────────────────────
		// A non-standard port must never pass profile validation.
		// Attacker could use twitter.com:8080/fakehandle to pass host checks
		// while the URL resolves to nothing on Twitter's network.
		{"https://twitter.com:8080/fakehandle", false, "non-standard port bypass"},
		{"https://x.com:443/myproject", false, "explicit port (even 443) rejected"},

		// ── Security: @ in path (BLOCKER fix) ────────────────────────────
		// Twitter usernames never contain @. A path like /fakeuser@evil.com
		// has one segment but is not a real Twitter handle.
		{"https://twitter.com/fakeprofile@evil.com", false, "@ in path segment"},
		{"https://x.com/user@attacker.io", false, "@ in path (x.com)"},

		// ── Non-Twitter domains ───────────────────────────────────────────
		{"https://t.co/SomeHash", false, "t.co short-link"},
		{"https://telegram.org/something", false, "telegram domain (not twitter)"},
		{"", false, "empty URL"},
	}
	for _, c := range cases {
		got := isTwitterProfileURL(c.url)
		if got != c.want {
			t.Errorf("isTwitterProfileURL(%q) [%s] = %v, want %v", c.url, c.label, got, c.want)
		}
	}
}

// TestParseSocialLinks_XcomInternalURLIsRejected covers the x.com/i/web/...
// pattern — the Twitter web-app internal redirect URL that was previously
// accepted by the string-based profile check.
func TestParseSocialLinks_XcomInternalURLIsRejected(t *testing.T) {
	body := `{"twitter":"https://x.com/i/web/status/1920908498099810362"}`
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false for x.com/i/web/ internal redirect (not a profile)")
	}
}

// TestParseSocialLinks_TwitterSearchIsRejected covers twitter.com/search?q=...
// which is a search results page, not an account profile.
func TestParseSocialLinks_TwitterSearchIsRejected(t *testing.T) {
	body := `{"twitter":"https://twitter.com/search?q=bitcoin"}`
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false for twitter.com search URL (not a profile)")
	}
}

// TestParseSocialLinks_TwitterIntentIsRejected covers x.com/intent/tweet?...
// which is a tweet-compose intent URL, not an account profile.
func TestParseSocialLinks_TwitterIntentIsRejected(t *testing.T) {
	body := `{"twitter":"https://x.com/intent/tweet?text=hello"}`
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false for x.com/intent/ URL (not a profile)")
	}
}

// TestParseSocialLinks_WebsiteAsTwitterIsRejected verifies that setting
// "website" to a Twitter profile URL does NOT satisfy the website requirement.
// A project's website must be its own domain, not a social media platform.
func TestParseSocialLinks_WebsiteAsTwitterIsRejected(t *testing.T) {
	body := `{"website":"https://twitter.com/myproject"}`
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false when website field points to twitter.com (social media, not a project site)")
	}
}

// TestParseSocialLinks_WebsiteAsXcomIsRejected covers x.com URLs used as website.
func TestParseSocialLinks_WebsiteAsXcomIsRejected(t *testing.T) {
	body := `{"website":"https://x.com/myproject"}`
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false when website field points to x.com (social media, not a project site)")
	}
}

// TestParseSocialLinks_WebsiteAsTelegramIsRejected covers t.me URLs used as website.
func TestParseSocialLinks_WebsiteAsTelegramIsRejected(t *testing.T) {
	body := `{"website":"https://t.me/myproject_official"}`
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false when website field points to t.me (social media, not a project site)")
	}
}

// TestParseSocialLinks_WebsiteAsDiscordIsRejected covers discord.gg URLs used as website.
func TestParseSocialLinks_WebsiteAsDiscordIsRejected(t *testing.T) {
	body := `{"website":"https://discord.gg/myproject"}`
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false when website field points to discord.gg (social media, not a project site)")
	}
}

// TestParseSocialLinks_TelegramFieldStillAccepted verifies that a t.me link
// in the TELEGRAM field (not the website field) is still accepted as a valid
// social presence — only the website field blocks t.me links.
func TestParseSocialLinks_TelegramFieldStillAccepted(t *testing.T) {
	body := `{"telegram":"https://t.me/myproject_official"}`
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatal("want HasSocialLinks=true for valid Telegram channel in telegram field")
	}
}

// TestParseSocialLinks_TwitterProfileValidAndWebsiteValid verifies the ideal
// case: a real Twitter profile + a real project website both pass.
func TestParseSocialLinks_TwitterProfileValidAndWebsiteValid(t *testing.T) {
	body := `{"twitter":"https://x.com/myproject","website":"https://myproject.io"}`
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatal("want HasSocialLinks=true for real profile + real website")
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

// ── Token-name association tests ─────────────────────────────────────────────

// TestParseSocialLinks_HijackedTwitterRejected is the canonical regression for
// the COOKING token (EPD6yc…RUYv): the dev set twitter to @baseapp, the real
// Base App company account. The profile URL is structurally valid but belongs
// to a completely different project. The token name "COOKING" does not appear
// in "https://x.com/baseapp" → rejected.
func TestParseSocialLinks_HijackedTwitterRejected(t *testing.T) {
	body := `{"twitter":"https://x.com/baseapp"}`
	has, err := parseSocialLinks([]byte(body), "COOKING", "COOKING")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false: @baseapp is not associated with COOKING token")
	}
}

// TestParseSocialLinks_MatchingTwitterAccepted verifies that a profile whose
// handle contains the token name is accepted: https://x.com/lolonsollol
// contains "lol" which is the token symbol.
func TestParseSocialLinks_MatchingTwitterAccepted(t *testing.T) {
	body := `{"twitter":"https://x.com/lolonsollol"}`
	has, err := parseSocialLinks([]byte(body), "lol", "LOL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatal("want HasSocialLinks=true: handle 'lolonsollol' contains 'lol'")
	}
}

// TestParseSocialLinks_EmptyTokenNameSkipsAssociation confirms the skip-path:
// when both tokenName and tokenSymbol are empty, the association check is
// skipped and a structurally valid URL is accepted.
func TestParseSocialLinks_EmptyTokenNameSkipsAssociation(t *testing.T) {
	body := `{"twitter":"https://x.com/someproject"}`
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatal("want HasSocialLinks=true when no token name to match against")
	}
}

// TestParseSocialLinks_VeryShortTokenNameSkipsAssociation confirms the skip
// when normalised name is < 3 chars (would cause false positive matches on
// common two-letter substrings in unrelated handles).
func TestParseSocialLinks_VeryShortTokenNameSkipsAssociation(t *testing.T) {
	body := `{"twitter":"https://x.com/someproject"}`
	has, err := parseSocialLinks([]byte(body), "AI", "AI")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatal("want HasSocialLinks=true when name is too short to match (<3 chars)")
	}
}

// TestParseSocialLinks_SymbolMatchAccepted verifies that matching on symbol
// (when name doesn't match) still accepts the link. Token: name="cat", symbol="MEOW",
// handle "@meowonsol" contains "meow".
func TestParseSocialLinks_SymbolMatchAccepted(t *testing.T) {
	body := `{"twitter":"https://x.com/meowonsol"}`
	has, err := parseSocialLinks([]byte(body), "cat", "MEOW")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatal("want HasSocialLinks=true when symbol 'meow' appears in handle")
	}
}

// ── No-code / template platform blocking ─────────────────────────────────────

// TestParseSocialLinks_WebflowWebsiteRejected is the COOKING regression: the
// token had "https://an-electric-mind.webflow.io" as website. Webflow is a
// no-code template platform — not a real project site.
func TestParseSocialLinks_WebflowWebsiteRejected(t *testing.T) {
	body := `{"website":"https://an-electric-mind.webflow.io"}`
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false for webflow.io template page")
	}
}

func TestParseSocialLinks_CarrdWebsiteRejected(t *testing.T) {
	body := `{"website":"https://mytoken.carrd.co"}`
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false for carrd.co template page")
	}
}

func TestParseSocialLinks_FramerWebsiteRejected(t *testing.T) {
	body := `{"website":"https://mytoken.framer.app"}`
	has, err := parseSocialLinks([]byte(body), "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("want HasSocialLinks=false for framer.app template page")
	}
}

func TestNormaliseTokenIdentifier(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"COOKING", "cooking"},
		{"LOL", "lol"},
		{"My Token 2025!", "mytoken2025"},
		{"", ""},
		{"  RIBBIT  ", "ribbit"},
		{"$PEPE", "pepe"},
	}
	for _, c := range cases {
		got := normaliseTokenIdentifier(c.input)
		if got != c.want {
			t.Errorf("normaliseTokenIdentifier(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestProfileAssociatedWithToken(t *testing.T) {
	cases := []struct {
		rawURL string
		name   string
		symbol string
		want   bool
		label  string
	}{
		{"https://x.com/baseapp", "COOKING", "COOKING", false, "hijacked account"},
		{"https://x.com/lolonsollol", "lol", "LOL", true, "handle contains symbol"},
		{"https://x.com/cookingonsol", "COOKING", "COOKING", true, "handle contains name"},
		{"https://x.com/someproject", "", "", true, "empty name — skip check"},
		{"https://x.com/ai_project", "AI", "AI", true, "short name (<3) — skip check"},
		{"https://x.com/meowonsol", "cat", "MEOW", true, "symbol match"},
	}
	for _, c := range cases {
		got := profileAssociatedWithToken(c.rawURL, c.name, c.symbol)
		if got != c.want {
			t.Errorf("profileAssociatedWithToken(%q, %q, %q) [%s] = %v, want %v",
				c.rawURL, c.name, c.symbol, c.label, got, c.want)
		}
	}
}
