package probes

import (
	"context"
	"errors"
	"math"
	"testing"

	"crypto-sniping-bot/contracts"
)

func TestSolanaHolderDistProbe_TopFiveSumWithKnownSupply(t *testing.T) {
	// EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v = USDC mint (valid base58, 44 chars)
	mint := "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	rpc := &stubSolanaRPC{
		accounts: map[string]*SolanaAccountData{
			mint: {Owner: splTokenV1Program},
		},
		holders: map[string][]SolanaTokenHolder{
			mint: {
				{Address: "h1", Amount: "300", Decimals: 0},
				{Address: "h2", Amount: "200", Decimals: 0},
				{Address: "h3", Amount: "150", Decimals: 0},
				{Address: "h4", Amount: "100", Decimals: 0},
				{Address: "h5", Amount: "50", Decimals: 0},
				{Address: "h6", Amount: "25", Decimals: 0},
			},
		},
	}
	probe := NewSolanaHolderDistProbe(rpc, SolanaHolderDistConfig{Enabled: true, TimeoutMs: 100, TopK: 5}, nil)
	in := contracts.MarketDataDTO{Chain: "solana", TokenAddress: mint, TotalSupplyKnown: true, TotalSupply: 1000}
	out, err := probe.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !out.HolderDistKnown {
		t.Fatal("HolderDistKnown not flipped")
	}
	if out.HolderCount != 6 {
		t.Fatalf("expected HolderCount=6, got %d", out.HolderCount)
	}
	// (300+200+150+100+50)/1000 = 0.8.
	if math.Abs(out.Top5HolderPct-0.8) > 1e-6 {
		t.Fatalf("expected Top5HolderPct=0.8, got %f", out.Top5HolderPct)
	}
}

func TestSolanaHolderDistProbe_FallbackDenominatorWhenSupplyUnknown(t *testing.T) {
	// So11111111111111111111111111111111111111112 = wSOL mint (valid base58, 43 chars)
	mint := "So11111111111111111111111111111111111111112"
	rpc := &stubSolanaRPC{
		accounts: map[string]*SolanaAccountData{
			mint: {Owner: splTokenV1Program},
		},
		holders: map[string][]SolanaTokenHolder{
			mint: {
				{Address: "h1", Amount: "400"},
				{Address: "h2", Amount: "100"},
			},
		},
	}
	probe := NewSolanaHolderDistProbe(rpc, SolanaHolderDistConfig{Enabled: true, TopK: 5}, nil)
	in := contracts.MarketDataDTO{Chain: "solana", TokenAddress: mint}
	out, err := probe.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !out.HolderDistKnown {
		t.Fatal("HolderDistKnown should flip even with fallback denom")
	}
	if math.Abs(out.Top5HolderPct-1.0) > 1e-6 {
		t.Fatalf("with fallback denom = sum(all), top-K=all, pct=1.0; got %f", out.Top5HolderPct)
	}
}

func TestSolanaHolderDistProbe_NoHolders_LeavesUnknown(t *testing.T) {
	// DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263 = Bonk mint (valid base58, 44 chars)
	const mintC = "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263"
	rpc := &stubSolanaRPC{
		accounts: map[string]*SolanaAccountData{
			mintC: {Owner: splTokenV1Program},
		},
		holders: map[string][]SolanaTokenHolder{},
	}
	probe := NewSolanaHolderDistProbe(rpc, SolanaHolderDistConfig{Enabled: true}, nil)
	out, err := probe.Probe(context.Background(), contracts.MarketDataDTO{Chain: "solana", TokenAddress: mintC})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if out.HolderDistKnown {
		t.Fatal("empty holder list must leave HolderDistKnown=false")
	}
}

func TestSolanaHolderDistProbe_RPCError(t *testing.T) {
	// 9vMJfxuKxXBoEa7rM12mYLMwTacLMLDJqHozw96WQL8i = valid base58, 44 chars
	const mintM = "9vMJfxuKxXBoEa7rM12mYLMwTacLMLDJqHozw96WQL8i"
	rpc := &stubSolanaRPC{
		accounts: map[string]*SolanaAccountData{
			mintM: {Owner: splTokenV1Program},
		},
		holdErr: errors.New("rate limit"),
	}
	probe := NewSolanaHolderDistProbe(rpc, SolanaHolderDistConfig{Enabled: true}, nil)
	out, err := probe.Probe(context.Background(), contracts.MarketDataDTO{Chain: "solana", TokenAddress: mintM})
	if err == nil {
		t.Fatal("expected error to bubble up")
	}
	if out.HolderDistKnown {
		t.Fatal("flag must remain false on rpc error")
	}
}

// TestSolanaHolderDistProbe_SkipsPrecheckWhenAuthoritiesKnown verifies that when
// SolanaAuthoritiesKnown=true the probe skips the getAccountInfo precheck and
// calls getTokenLargestAccounts directly. The stub has no entry in accounts for
// this mint; if GetAccountInfo were called it would return nil → probe would
// return early without setting HolderDistKnown. Passing the test proves the
// fast path is taken.
func TestSolanaHolderDistProbe_SkipsPrecheckWhenAuthoritiesKnown(t *testing.T) {
	// 7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU = valid base58, 44 chars
	const mint = "7xKXtg2CW87d97TXJSDpbD5jBkheTqA83TZRuJosgAsU"
	rpc := &stubSolanaRPC{
		// Intentionally no entry in accounts — GetAccountInfo would return nil
		// and cause the precheck path to skip the probe. If fast path works,
		// this entry is never consulted.
		accounts: map[string]*SolanaAccountData{},
		holders: map[string][]SolanaTokenHolder{
			mint: {
				{Address: "h1", Amount: "600"},
				{Address: "h2", Amount: "400"},
			},
		},
	}
	probe := NewSolanaHolderDistProbe(rpc, SolanaHolderDistConfig{Enabled: true, TopK: 5}, nil)
	in := contracts.MarketDataDTO{
		Chain:                  "solana",
		TokenAddress:           mint,
		SolanaAuthoritiesKnown: true, // fast path: skip getAccountInfo precheck
	}
	out, err := probe.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !out.HolderDistKnown {
		t.Fatal("HolderDistKnown must be set when authorities already known (fast path)")
	}
	if out.HolderCount != 2 {
		t.Fatalf("expected HolderCount=2, got %d", out.HolderCount)
	}
}
