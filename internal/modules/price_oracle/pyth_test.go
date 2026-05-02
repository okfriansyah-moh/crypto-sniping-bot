package price_oracle
package price_oracle

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// buildPythAccount synthesises a 3312-byte Pyth V2 PriceAccount payload with
// the magic, exponent, aggregate price, and aggregate status fields filled
// in. All other bytes are zero — sufficient for the decoder which only
// reads the four documented offsets.
func buildPythAccount(rawPrice int64, expo int32, status uint32) []byte {
	buf := make([]byte, 3312)
	binary.LittleEndian.PutUint32(buf[pythOffsetMagic:], pythMagic)
	binary.LittleEndian.PutUint32(buf[pythOffsetVersion:], 2)
	binary.LittleEndian.PutUint32(buf[pythOffsetExponent:], uint32(expo)) //nolint:gosec
	binary.LittleEndian.PutUint64(buf[pythOffsetAggPrice:], uint64(rawPrice))
	binary.LittleEndian.PutUint32(buf[pythOffsetAggStatus:], status)
	return buf
}

func TestDecodePythPrice_TradingHappyPath(t *testing.T) {
	// SOL/USD on mainnet: expo=-8, rawPrice=14523000000 → 145.23 USD
	buf := buildPythAccount(14_523_000_000, -8, pythStatusTrading)
	b64 := base64.StdEncoding.EncodeToString(buf)

	now := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	quote, err := DecodePythPrice(b64, 12345, now)
	if err != nil {
		t.Fatalf("DecodePythPrice returned error: %v", err)
	}
	if quote.Price < 145.22 || quote.Price > 145.24 {
		t.Errorf("expected price ≈145.23, got %f", quote.Price)
	}
	if quote.Exponent != -8 {
		t.Errorf("expected expo=-8, got %d", quote.Exponent)
	}
	if quote.Slot != 12345 {
		t.Errorf("expected slot=12345, got %d", quote.Slot)
	}
	if !quote.FetchedAt.Equal(now) {
		t.Errorf("expected FetchedAt=%v, got %v", now, quote.FetchedAt)
	}
	if quote.Stale {
		t.Error("fresh decode must not be marked Stale")
	}
}

func TestDecodePythPrice_RejectsBadMagic(t *testing.T) {
	buf := buildPythAccount(1, -8, pythStatusTrading)
	binary.LittleEndian.PutUint32(buf[pythOffsetMagic:], 0xdeadbeef) // wrong magic
	b64 := base64.StdEncoding.EncodeToString(buf)

	_, err := DecodePythPrice(b64, 0, time.Now())
	if !errors.Is(err, ErrInvalidPythAccount) {
		t.Fatalf("expected ErrInvalidPythAccount, got %v", err)
	}
}

func TestDecodePythPrice_RejectsNonTradingStatus(t *testing.T) {
	for _, status := range []uint32{0, 2, 3, 99} {
		buf := buildPythAccount(14_523_000_000, -8, status)
		b64 := base64.StdEncoding.EncodeToString(buf)
		_, err := DecodePythPrice(b64, 0, time.Now())
		if !errors.Is(err, ErrPriceNotTrading) {
			t.Errorf("status=%d: expected ErrPriceNotTrading, got %v", status, err)
		}
	}
}

func TestDecodePythPrice_RejectsTruncated(t *testing.T) {
	short := base64.StdEncoding.EncodeToString(make([]byte, 100))
	_, err := DecodePythPrice(short, 0, time.Now())
	if !errors.Is(err, ErrInvalidPythAccount) {
		t.Fatalf("expected ErrInvalidPythAccount on short payload, got %v", err)
	}
}

// stubFetcher feeds canned account data + error per call sequence.
type stubFetcher struct {
	calls    atomic.Int32
	accounts []*RawAccount
	errs     []error
}

func (s *stubFetcher) GetAccountInfo(_ context.Context, _, _ string) (*RawAccount, error) {
	idx := int(s.calls.Add(1)) - 1
	var acct *RawAccount
	var err error
	if idx < len(s.accounts) {
		acct = s.accounts[idx]
	}
	if idx < len(s.errs) {
		err = s.errs[idx]
	}
	return acct, err
}

func newStubAccount(rawPrice int64, expo int32, slot uint64) *RawAccount {
	buf := buildPythAccount(rawPrice, expo, pythStatusTrading)
	return &RawAccount{DataB64: base64.StdEncoding.EncodeToString(buf), Slot: slot}
}

func TestSolUsdProvider_CacheHitWithinTTL(t *testing.T) {
	// Phase 3 invariant: a cache hit must not call the RPC again.
	clock := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	stub := &stubFetcher{accounts: []*RawAccount{newStubAccount(14_500_000_000, -8, 1)}}
	p := NewSolUsdProvider(stub, SolUsdConfig{Pubkey: "PYTH_SOL_USD", TTL: 5 * time.Second})
	p.now = func() time.Time { return clock }

	q1, err := p.Get(context.Background())
	if err != nil {
		t.Fatalf("first Get error: %v", err)
	}
	if q1.Stale {
		t.Error("first quote must be fresh")
	}

	clock = clock.Add(2 * time.Second) // still within TTL
	q2, err := p.Get(context.Background())
	if err != nil {
		t.Fatalf("second Get error: %v", err)
	}
	if got := stub.calls.Load(); got != 1 {
		t.Errorf("expected 1 RPC call (cache hit), got %d", got)
	}
	if q2.Stale {
		t.Error("cache hit must not be Stale")
	}
}

func TestSolUsdProvider_LastGoodFallbackOnError(t *testing.T) {
	// Phase 3 invariant: when the RPC is briefly down but a recent
	// quote exists, return it with Stale=true rather than failing.
	clock := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	stub := &stubFetcher{
		accounts: []*RawAccount{newStubAccount(14_500_000_000, -8, 1), nil},
		errs:     []error{nil, fmt.Errorf("rpc unreachable")},
	}
	p := NewSolUsdProvider(stub, SolUsdConfig{
		Pubkey:     "PYTH_SOL_USD",
		TTL:        1 * time.Second,
		StaleAfter: 30 * time.Second,
	})
	p.now = func() time.Time { return clock }

	if _, err := p.Get(context.Background()); err != nil {
		t.Fatalf("seed Get error: %v", err)
	}

	clock = clock.Add(5 * time.Second) // past TTL but inside StaleAfter
	q, err := p.Get(context.Background())
	if err != nil {
		t.Fatalf("expected stale fallback, got error %v", err)
	}
	if !q.Stale {
		t.Error("expected Stale=true when serving last-good")
	}
}

func TestSolUsdProvider_HardFailWhenLastGoodTooOld(t *testing.T) {
	// Phase 3 invariant: do not return arbitrarily old prices. After
	// StaleAfter elapses with no successful refresh, propagate the error.
	clock := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	stub := &stubFetcher{
		accounts: []*RawAccount{newStubAccount(14_500_000_000, -8, 1), nil},
		errs:     []error{nil, fmt.Errorf("rpc unreachable")},
	}
	p := NewSolUsdProvider(stub, SolUsdConfig{
		Pubkey:     "PYTH_SOL_USD",
		TTL:        1 * time.Second,
		StaleAfter: 10 * time.Second,
	})
	p.now = func() time.Time { return clock }

	if _, err := p.Get(context.Background()); err != nil {
		t.Fatalf("seed Get error: %v", err)
	}

	clock = clock.Add(60 * time.Second) // way past StaleAfter
	if _, err := p.Get(context.Background()); err == nil {
		t.Fatal("expected error when last-good is too old, got nil")
	}
}
