package probes

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"crypto-sniping-bot/contracts"
)

type fakeEVMRPC struct {
	out []byte
	err error
	// captured args
	to   string
	data []byte
}

func (f *fakeEVMRPC) EthCall(_ context.Context, to string, data []byte, _ string) ([]byte, error) {
	f.to = to
	f.data = data
	return f.out, f.err
}

func encodeReservesReturn(r0, r1 uint64, ts uint32) []byte {
	b := make([]byte, 96)
	// uint112 padded to 32-byte word; uint64 fits within uint112.
	binary.BigEndian.PutUint64(b[24:32], r0)
	binary.BigEndian.PutUint64(b[56:64], r1)
	binary.BigEndian.PutUint32(b[92:96], ts)
	return b
}

func TestDecodeUniswapV2Reserves_HappyPath(t *testing.T) {
	in := encodeReservesReturn(123_456, 789_012, 1_700_000_000)
	out, err := DecodeUniswapV2Reserves(in)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Reserve0.Uint64() != 123_456 {
		t.Fatalf("r0=%s", out.Reserve0)
	}
	if out.Reserve1.Uint64() != 789_012 {
		t.Fatalf("r1=%s", out.Reserve1)
	}
	if out.BlockTimestamp != 1_700_000_000 {
		t.Fatalf("ts=%d", out.BlockTimestamp)
	}
}

func TestDecodeUniswapV2Reserves_TooShort(t *testing.T) {
	if _, err := DecodeUniswapV2Reserves(make([]byte, 50)); err == nil {
		t.Fatal("expected error")
	}
}

func TestEVMPairReservesProbe_BaseSelectsCorrectReserve(t *testing.T) {
	rpc := &fakeEVMRPC{out: encodeReservesReturn(1_000_000, 9_999_999, 0)}
	p := NewEVMPairReservesProbe(rpc, EVMPairReservesConfig{Enabled: true, TimeoutMs: 100}, nil)

	in := contracts.MarketDataDTO{
		Chain:         "eth",
		PoolAddress:   "0xpool",
		Token0Address: "0xWETH",
		Token1Address: "0xTKN",
		BaseAddress:   "0xWETH", // base is token0
	}
	out, err := p.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if out.ReserveBaseRaw != "1000000" {
		t.Fatalf("expected base=reserve0, got base=%s token=%s", out.ReserveBaseRaw, out.ReserveTokenRaw)
	}
	if out.ReserveTokenRaw != "9999999" {
		t.Fatalf("token reserve wrong: %s", out.ReserveTokenRaw)
	}
	if rpc.to != "0xpool" {
		t.Fatalf("eth_call to=%s", rpc.to)
	}
}

func TestEVMPairReservesProbe_BaseIsToken1(t *testing.T) {
	rpc := &fakeEVMRPC{out: encodeReservesReturn(7, 13, 0)}
	p := NewEVMPairReservesProbe(rpc, EVMPairReservesConfig{Enabled: true}, nil)
	in := contracts.MarketDataDTO{
		Chain: "bsc", PoolAddress: "0xp",
		Token0Address: "0xTKN", Token1Address: "0xWBNB", BaseAddress: "0xWBNB",
	}
	out, err := p.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if out.ReserveBaseRaw != "13" || out.ReserveTokenRaw != "7" {
		t.Fatalf("base/token swap wrong: base=%s token=%s", out.ReserveBaseRaw, out.ReserveTokenRaw)
	}
}

func TestEVMPairReservesProbe_SkipsSolana(t *testing.T) {
	p := NewEVMPairReservesProbe(&fakeEVMRPC{}, EVMPairReservesConfig{Enabled: true}, nil)
	in := contracts.MarketDataDTO{Chain: "solana", PoolAddress: "P", Token0Address: "A", Token1Address: "B", BaseAddress: "A", ReserveBaseRaw: "untouched"}
	out, err := p.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if out.ReserveBaseRaw != "untouched" {
		t.Fatal("solana DTOs must pass through")
	}
}

func TestEVMPairReservesProbe_BaseNotInPair(t *testing.T) {
	rpc := &fakeEVMRPC{out: encodeReservesReturn(1, 2, 0)}
	p := NewEVMPairReservesProbe(rpc, EVMPairReservesConfig{Enabled: true}, nil)
	in := contracts.MarketDataDTO{
		Chain: "eth", PoolAddress: "0xp",
		Token0Address: "0xA", Token1Address: "0xB", BaseAddress: "0xC",
	}
	if _, err := p.Probe(context.Background(), in); err == nil {
		t.Fatal("expected error when base not in pair")
	}
}

func TestEVMPairReservesProbe_RPCError(t *testing.T) {
	p := NewEVMPairReservesProbe(&fakeEVMRPC{err: errors.New("boom")}, EVMPairReservesConfig{Enabled: true}, nil)
	in := contracts.MarketDataDTO{Chain: "eth", PoolAddress: "P", Token0Address: "A", Token1Address: "B", BaseAddress: "A"}
	if _, err := p.Probe(context.Background(), in); err == nil {
		t.Fatal("expected eth_call error to bubble up")
	}
}
