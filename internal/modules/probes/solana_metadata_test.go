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
