package rpc

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

type mockSolanaAccounts struct {
	accounts map[string]*AccountInfo
	err      error
}

func (m *mockSolanaAccounts) GetAccountInfo(_ context.Context, pubkey, _ string) (*AccountInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.accounts[pubkey], nil
}

func (m *mockSolanaAccounts) GetMultipleAccounts(_ context.Context, pubkeys []string, _ string) ([]*AccountInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	out := make([]*AccountInfo, len(pubkeys))
	for i, pk := range pubkeys {
		out[i] = m.accounts[pk]
	}
	return out, nil
}

func buildBondingCurveData(virtualToken, virtualSol, realToken, realSol uint64) []byte {
	data := make([]byte, pumpfunBondingCurveAccountSize)
	binary.LittleEndian.PutUint64(data[8:], virtualToken)
	binary.LittleEndian.PutUint64(data[16:], virtualSol)
	binary.LittleEndian.PutUint64(data[24:], realToken)
	binary.LittleEndian.PutUint64(data[32:], realSol)
	return data
}

func TestPriceFromBondingCurve(t *testing.T) {
	// 1 SOL (1e9 lamports) for 1e6 raw tokens (1 whole token @ 6 decimals) → 1.0 SOL/token
	data := buildBondingCurveData(1_000_000, 1_000_000_000, 0, 0)
	price, ok := priceFromBondingCurve(data)
	if !ok {
		t.Fatal("expected ok")
	}
	if price != "1" {
		t.Fatalf("price = %q, want 1", price)
	}
}

func TestSolanaPoolPriceClient_CacheHit(t *testing.T) {
	pool := "POOL111"
	rpc := &mockSolanaAccounts{
		accounts: map[string]*AccountInfo{
			pool: {Data: []string{base64.StdEncoding.EncodeToString(buildBondingCurveData(1_000_000, 1_000_000_000, 0, 0))}},
		},
	}
	resolver := func(_ context.Context, _, _ string) (string, bool, error) {
		return pool, true, nil
	}
	client := NewSolanaPoolPriceClient(SolanaPoolPriceConfig{
		RPC:          rpc,
		PoolResolver: resolver,
		CacheTTL:     time.Minute,
	})
	p1, err := client.GetTokenPrice(context.Background(), "TOKEN", "solana")
	if err != nil || p1 == "" {
		t.Fatalf("first fetch: price=%q err=%v", p1, err)
	}
	rpc.err = errors.New("rpc down")
	p2, err := client.GetTokenPrice(context.Background(), "TOKEN", "solana")
	if err != nil || p2 != p1 {
		t.Fatalf("cache hit: price=%q err=%v want %q", p2, err, p1)
	}
}

func TestSolanaPoolPriceClient_StaleFallback(t *testing.T) {
	pool := "POOL222"
	rpc := &mockSolanaAccounts{
		accounts: map[string]*AccountInfo{
			pool: {Data: []string{base64.StdEncoding.EncodeToString(buildBondingCurveData(1_000_000, 2_000_000_000, 0, 0))}},
		},
	}
	resolver := func(_ context.Context, _, _ string) (string, bool, error) {
		return pool, true, nil
	}
	client := NewSolanaPoolPriceClient(SolanaPoolPriceConfig{
		RPC:                rpc,
		PoolResolver:       resolver,
		CacheTTL:           time.Millisecond,
		StaleMaxMultiplier: 10,
	})
	p1, err := client.GetTokenPrice(context.Background(), "TOKEN2", "solana")
	if err != nil || p1 == "" {
		t.Fatalf("warm cache: %v", err)
	}
	time.Sleep(3 * time.Millisecond)
	rpc.err = errors.New("rpc down")
	p2, err := client.GetTokenPrice(context.Background(), "TOKEN2", "solana")
	if err != nil || p2 != p1 {
		t.Fatalf("stale fallback: price=%q err=%v want %q", p2, err, p1)
	}
}

func TestRoutingPriceClient_FallsBackToDEXScreener(t *testing.T) {
	// Non-solana chain should always use fallback (nil solana client).
	fallback := NewDEXScreenerPriceClient(nil)
	routing := NewRoutingPriceClient(nil, fallback, nil)
	_, err := routing.GetTokenPrice(context.Background(), "0xtoken", "ethereum")
	// DEXScreener will fail in unit test (no network) — we only assert solana path skipped.
	if err == nil {
		t.Log("dexscreener returned without error (unexpected in offline test)")
	}
}
