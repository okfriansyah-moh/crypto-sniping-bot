package price_oracle
// Package price_oracle implements Layer-0/Layer-2 helper price feeds.
//
// Phase 3 (recovery): a Pyth-based SOL/USD live price feed.  The decoder
// reads a Solana getAccountInfo result (base64-encoded Pyth V2 PriceAccount
// layout) and produces a price float.  A small TTL cache fronts the
// fetcher so the rest of the pipeline (ingestion_solana LiquidityUsd,
// position monitor, slippage model) can call into it on every event
// without hammering the RPC.
//
// The decoder layout matches the canonical Pyth Solana V2 price account
// (legacy, pre-Lazer) used on mainnet for SOL/USD
// `H6ARHf6YXhGYeQfUzQNGk6rDNnLBQKrenN712K4AQJEG`:
//
//	offset 0   u32  magic   (0xa1b2c3d4 little-endian)
//	offset 4   u32  version
//	offset 16  u32  price_type
//	offset 20  i32  exponent       (typically negative)
//	offset 224 i64  aggregate.price
//	offset 240 u32  aggregate.status (1 = trading)
//
// Source: https://github.com/pyth-network/pyth-client-go (PriceAccount struct).
package price_oracle

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"time"
)

const (
	// pythMagic is the Pyth account header sentinel ("0xa1b2c3d4" LE).
	pythMagic uint32 = 0xa1b2c3d4

	// Pyth price-account V2 byte offsets.
	pythOffsetMagic     = 0
	pythOffsetVersion   = 4
	pythOffsetExponent  = 20
	pythOffsetAggPrice  = 208
	pythOffsetAggStatus = 224

	// pythStatusTrading is the only status value that yields a usable price.
	// All other statuses (unknown=0, halted=2, auction=3) return ErrPriceNotTrading.
	pythStatusTrading uint32 = 1

	// pythAccountMinBytes is the smallest account size that can satisfy our
	// offset reads. The real account is much larger (~3312 bytes); this is a
	// floor used to reject obviously-malformed payloads early.
	pythAccountMinBytes = 256
)

// PriceQuote is a decoded Pyth price snapshot.
//
// Decimals: the Pyth account stores price as int64 + exponent (typically
// negative). The float64 value is `price * 10^expo`. For SOL/USD, expo=-8,
// so an i64 of 14_523_000_000 yields 145.23.
type PriceQuote struct {
	Price     float64   // price * 10^expo
	RawPrice  int64     // raw aggregate price
	Exponent  int32     // typically negative (e.g. -8)
	Slot      uint64    // RPC context slot at fetch time
	FetchedAt time.Time // wall-clock when the RPC call returned
	Stale     bool      // true when the cache served a value past its TTL
}

// ErrPriceNotTrading is returned when the Pyth account aggregate status is
// not "trading" (1). Callers should treat this the same as a missing feed.
var ErrPriceNotTrading = fmt.Errorf("price_oracle: pyth aggregate not trading")

// ErrInvalidPythAccount is returned when the account data fails the magic
// check or is too short to contain the expected fields.
var ErrInvalidPythAccount = fmt.Errorf("price_oracle: invalid pyth account layout")

// DecodePythPrice decodes a base64-encoded Pyth V2 price account into a
// usable price.  It is pure and deterministic — same input → same output.
// fetchedAt is captured by the caller so tests stay reproducible.
func DecodePythPrice(b64 string, slot uint64, fetchedAt time.Time) (*PriceQuote, error) {
	if b64 == "" {
		return nil, ErrInvalidPythAccount
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("price_oracle: base64 decode: %w", err)
	}
	if len(raw) < pythAccountMinBytes {
		return nil, ErrInvalidPythAccount
	}

	magic := binary.LittleEndian.Uint32(raw[pythOffsetMagic : pythOffsetMagic+4])
	if magic != pythMagic {
		return nil, ErrInvalidPythAccount
	}

	expo := int32(binary.LittleEndian.Uint32(raw[pythOffsetExponent : pythOffsetExponent+4]))
	rawPrice := int64(binary.LittleEndian.Uint64(raw[pythOffsetAggPrice : pythOffsetAggPrice+8]))
	status := binary.LittleEndian.Uint32(raw[pythOffsetAggStatus : pythOffsetAggStatus+4])

	if status != pythStatusTrading {
		return nil, ErrPriceNotTrading
	}
	if rawPrice <= 0 {
		// Pyth never publishes negative prices; rawPrice<=0 means a
		// publisher absence + corrupt aggregate. Treat as not-trading.
		return nil, ErrPriceNotTrading
	}

	price := float64(rawPrice) * math.Pow10(int(expo))
	return &PriceQuote{
		Price:     price,
		RawPrice:  rawPrice,
		Exponent:  expo,
		Slot:      slot,
		FetchedAt: fetchedAt,
	}, nil
}

// AccountFetcher is the minimal interface the price oracle needs from the
// Solana RPC client.  Defined here so tests can inject a deterministic stub.
type AccountFetcher interface {
	GetAccountInfo(ctx context.Context, pubkey, commitment string) (*RawAccount, error)
}

// RawAccount mirrors the subset of internal/rpc.AccountInfo we consume.
// Duplicating the struct here avoids an import cycle (the oracle is meant
// to be reusable from any module without pulling internal/rpc).
type RawAccount struct {
	DataB64 string
	Slot    uint64
}

// SolUsdProvider is the Phase 3 entry point for "current SOL/USD price".
// Internally it caches Pyth quotes for `ttl` and falls back to the last
// successful quote (with Stale=true) when a refresh fails.  Use NewSolUsdProvider
// + Get from any module that needs USD conversion.
//
// Concurrency-safe.  Determinism: when callers want bit-for-bit replay they
// should pass a ManualClock and freeze the cache.
type SolUsdProvider struct {
	fetcher    AccountFetcher
	pubkey     string
	commitment string
	ttl        time.Duration
	staleAfter time.Duration
	now        func() time.Time

	mu       sync.RWMutex
	last     *PriceQuote
	lastErr  error
	lastUpAt time.Time
}

// SolUsdConfig configures the Pyth SOL/USD provider.
//
// Sensible defaults:
//   - ttl = 5s         (Pyth aggregate price updates ~400ms; 5s caches ~12 ticks)
//   - staleAfter = 60s (after this we still serve last-good but with Stale=true)
//   - commitment = "confirmed"
type SolUsdConfig struct {
	Pubkey     string        // Pyth SOL/USD price account
	TTL        time.Duration // cache freshness window
	StaleAfter time.Duration // grace window for serving last-good
	Commitment string        // RPC commitment level
}

// NewSolUsdProvider wires up the fetcher with caching + last-good fallback.
func NewSolUsdProvider(fetcher AccountFetcher, cfg SolUsdConfig) *SolUsdProvider {
	if cfg.TTL <= 0 {
		cfg.TTL = 5 * time.Second
	}
	if cfg.StaleAfter <= 0 {
		cfg.StaleAfter = 60 * time.Second
	}
	if cfg.Commitment == "" {
		cfg.Commitment = "confirmed"
	}
	return &SolUsdProvider{
		fetcher:    fetcher,
		pubkey:     cfg.Pubkey,
		commitment: cfg.Commitment,
		ttl:        cfg.TTL,
		staleAfter: cfg.StaleAfter,
		now:        time.Now,
	}
}

// Get returns the current SOL/USD price quote.
//
// Behaviour:
//   - Cache hit (within ttl) → fresh value, Stale=false.
//   - Cache miss → fetch + decode; on success cache the value.
//   - Fetch failure → if last quote is younger than staleAfter, return it
//     with Stale=true; otherwise propagate the underlying error.
//
// This is the single failure-mode contract that lets callers (data quality
// engine, slippage model, position monitor) decide how to weight a stale
// price — see skill price-feed-integration.
func (p *SolUsdProvider) Get(ctx context.Context) (*PriceQuote, error) {
	p.mu.RLock()
	last := p.last
	lastUp := p.lastUpAt
	p.mu.RUnlock()

	now := p.now()
	if last != nil && now.Sub(lastUp) < p.ttl {
		// Defensive copy — callers must not mutate the cached struct.
		out := *last
		return &out, nil
	}

	if p.fetcher == nil || p.pubkey == "" {
		return nil, fmt.Errorf("price_oracle: provider not configured")
	}

	acct, err := p.fetcher.GetAccountInfo(ctx, p.pubkey, p.commitment)
	if err == nil && acct != nil {
		quote, decodeErr := DecodePythPrice(acct.DataB64, acct.Slot, now)
		if decodeErr == nil {
			p.mu.Lock()
			p.last = quote
			p.lastErr = nil
			p.lastUpAt = now
			p.mu.Unlock()
			return quote, nil
		}
		err = decodeErr
	} else if err == nil && acct == nil {
		err = fmt.Errorf("price_oracle: account not found: %s", p.pubkey)
	}

	// Fetch/decode failed. Serve last-good if recent enough.
	if last != nil && now.Sub(lastUp) < p.staleAfter {
		out := *last
		out.Stale = true
		return &out, nil
	}

	p.mu.Lock()
	p.lastErr = err
	p.mu.Unlock()
	return nil, err
}
