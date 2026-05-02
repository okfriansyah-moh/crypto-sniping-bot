package probes

import (
	"context"
	"errors"
	"math"
	"testing"

	"crypto-sniping-bot/contracts"
)

func TestSolanaHolderDistProbe_TopFiveSumWithKnownSupply(t *testing.T) {
	mint := "MintA"
	rpc := &stubSolanaRPC{holders: map[string][]SolanaTokenHolder{
		mint: {
			{Address: "h1", Amount: "300", Decimals: 0},
			{Address: "h2", Amount: "200", Decimals: 0},
			{Address: "h3", Amount: "150", Decimals: 0},
			{Address: "h4", Amount: "100", Decimals: 0},
			{Address: "h5", Amount: "50", Decimals: 0},
			{Address: "h6", Amount: "25", Decimals: 0},
		},
	}}
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
	mint := "MintB"
	rpc := &stubSolanaRPC{holders: map[string][]SolanaTokenHolder{
		mint: {
			{Address: "h1", Amount: "400"},
			{Address: "h2", Amount: "100"},
		},
	}}
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
	rpc := &stubSolanaRPC{holders: map[string][]SolanaTokenHolder{}}
	probe := NewSolanaHolderDistProbe(rpc, SolanaHolderDistConfig{Enabled: true}, nil)
	out, err := probe.Probe(context.Background(), contracts.MarketDataDTO{Chain: "solana", TokenAddress: "MintC"})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if out.HolderDistKnown {
		t.Fatal("empty holder list must leave HolderDistKnown=false")
	}
}

func TestSolanaHolderDistProbe_RPCError(t *testing.T) {
	probe := NewSolanaHolderDistProbe(&stubSolanaRPC{holdErr: errors.New("rate limit")}, SolanaHolderDistConfig{Enabled: true}, nil)
	out, err := probe.Probe(context.Background(), contracts.MarketDataDTO{Chain: "solana", TokenAddress: "M"})
	if err == nil {
		t.Fatal("expected error to bubble up")
	}
	if out.HolderDistKnown {
		t.Fatal("flag must remain false on rpc error")
	}
}
