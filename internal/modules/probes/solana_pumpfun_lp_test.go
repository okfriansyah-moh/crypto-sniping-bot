package probes

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"testing"

	"crypto-sniping-bot/contracts"
)

type stubSolUsd struct {
	price float64
	ok    bool
}

func (s stubSolUsd) SolUsd(_ context.Context) (float64, bool) { return s.price, s.ok }

func buildPumpfunCurve(vSol, rSol, vTok, rTok, supply uint64, complete bool) []byte {
	b := make([]byte, pumpfunBondingCurveSize)
	// discriminator left zero
	binary.LittleEndian.PutUint64(b[offsetVirtualTokenRes:], vTok)
	binary.LittleEndian.PutUint64(b[offsetVirtualSolRes:], vSol)
	binary.LittleEndian.PutUint64(b[offsetRealTokenRes:], rTok)
	binary.LittleEndian.PutUint64(b[offsetRealSolRes:], rSol)
	binary.LittleEndian.PutUint64(b[offsetTokenTotalSupply:], supply)
	if complete {
		b[offsetComplete] = 1
	}
	return b
}

func TestDecodePumpfunBondingCurve_FieldsAtCorrectOffsets(t *testing.T) {
	b := buildPumpfunCurve(30_000_000_000, 5_000_000_000, 800_000_000_000_000, 0, 1_000_000_000_000_000, false)
	st, err := DecodePumpfunBondingCurve(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.VirtualSolReserves != 30_000_000_000 || st.RealSolReserves != 5_000_000_000 {
		t.Fatalf("sol reserves wrong: %+v", st)
	}
	if st.VirtualTokenReserves != 800_000_000_000_000 {
		t.Fatalf("token reserves wrong: %+v", st)
	}
	if st.Complete {
		t.Fatal("complete flag wrong")
	}
}

func TestDecodePumpfunBondingCurve_TooShort(t *testing.T) {
	if _, err := DecodePumpfunBondingCurve(make([]byte, 30)); err == nil {
		t.Fatal("expected error")
	}
}

func TestSolanaPumpfunLpProbe_HappyPath_ComputesLiquidityUsd(t *testing.T) {
	pool := "Pool1"
	curve := buildPumpfunCurve(30_000_000_000, 5_000_000_000, 800_000_000_000_000, 100_000_000_000_000, 1_000_000_000_000_000, false)
	rpc := &stubSolanaRPC{accounts: map[string]*SolanaAccountData{
		pool: {DataB64: base64.StdEncoding.EncodeToString(curve)},
	}}
	probe := NewSolanaPumpfunLpProbe(rpc, stubSolUsd{price: 150.0, ok: true}, SolanaPumpfunLpConfig{Enabled: true, TimeoutMs: 100}, nil)
	in := contracts.MarketDataDTO{Chain: "solana", Market: "solana-pumpfun", PoolAddress: pool, TokenAddress: "T"}
	out, err := probe.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !out.LpStatsKnown {
		t.Fatal("expected LpStatsKnown=true with live SOL price")
	}
	// (30+5)e9 lamports / 1e9 = 35 SOL × $150 = $5250.
	if out.LiquidityUsd < 5249 || out.LiquidityUsd > 5251 {
		t.Fatalf("expected liquidity ~5250, got %f", out.LiquidityUsd)
	}
	if out.ReserveBaseRaw != "35000000000" {
		t.Fatalf("expected combined SOL reserves, got %s", out.ReserveBaseRaw)
	}
	if !out.TotalSupplyKnown {
		t.Fatal("expected TotalSupplyKnown to be set when ingestion missed it")
	}
}

func TestSolanaPumpfunLpProbe_NoSolPrice_LeavesLpStatsUnknown(t *testing.T) {
	pool := "Pool2"
	curve := buildPumpfunCurve(1, 1, 1, 1, 0, false)
	rpc := &stubSolanaRPC{accounts: map[string]*SolanaAccountData{
		pool: {DataB64: base64.StdEncoding.EncodeToString(curve)},
	}}
	probe := NewSolanaPumpfunLpProbe(rpc, stubSolUsd{ok: false}, SolanaPumpfunLpConfig{Enabled: true, TimeoutMs: 100}, nil)
	in := contracts.MarketDataDTO{Chain: "solana", Market: "solana-pumpfun", PoolAddress: pool}
	out, err := probe.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if out.LpStatsKnown {
		t.Fatal("LpStatsKnown must NOT flip true without a SOL price")
	}
	if out.ReserveBaseRaw != "2" {
		t.Fatalf("reserves should still be populated, got %s", out.ReserveBaseRaw)
	}
}

func TestSolanaPumpfunLpProbe_SkipsRaydium(t *testing.T) {
	probe := NewSolanaPumpfunLpProbe(&stubSolanaRPC{}, stubSolUsd{price: 100, ok: true}, SolanaPumpfunLpConfig{Enabled: true}, nil)
	in := contracts.MarketDataDTO{Chain: "solana", Market: "solana-raydium-v4", PoolAddress: "P"}
	out, err := probe.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if out.LpStatsKnown || out.LiquidityUsd != 0 {
		t.Fatal("raydium markets must pass through")
	}
}
