package probes

import (
	"context"
	"errors"
	"testing"

	"crypto-sniping-bot/contracts"
)

type fakeRPC struct {
	out []byte
	err error
	got []byte
}

func (f *fakeRPC) EthCall(_ context.Context, callData []byte, _ string) ([]byte, error) {
	f.got = callData
	if f.err != nil {
		return nil, f.err
	}
	return f.out, nil
}

func TestHoneypotSimProbe_NoConfigContract_ReturnsUnknown(t *testing.T) {
	p := NewHoneypotSimProbe(&fakeRPC{}, HoneypotSimConfig{Enabled: true, SimulationContract: ""}, nil)
	in := contracts.MarketDataDTO{TokenAddress: "0xabc"}
	out, err := p.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if out.HoneypotSimKnown {
		t.Fatal("expected HoneypotSimKnown=false on empty SimulationContract")
	}
	if out.BuySimSuccess || out.SellSimSuccess {
		t.Fatalf("expected sim flags zero, got buy=%v sell=%v", out.BuySimSuccess, out.SellSimSuccess)
	}
}

func TestHoneypotSimProbe_RPCSuccess_PopulatesFlags(t *testing.T) {
	// 64 bytes: bool true (LSB=1) || bool false (LSB=0)
	raw := make([]byte, 64)
	raw[31] = 1
	rpc := &fakeRPC{out: raw}
	p := NewHoneypotSimProbe(rpc, HoneypotSimConfig{
		Enabled:            true,
		SimulationContract: "0xSIM",
		TimeoutMs:          1000,
	}, nil)

	in := contracts.MarketDataDTO{TokenAddress: "0xabc"}
	out, err := p.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("Probe error: %v", err)
	}
	if !out.HoneypotSimKnown {
		t.Fatal("expected HoneypotSimKnown=true on success")
	}
	if !out.BuySimSuccess {
		t.Fatal("expected BuySimSuccess=true")
	}
	if out.SellSimSuccess {
		t.Fatal("expected SellSimSuccess=false")
	}
	// Input must not be mutated.
	if in.HoneypotSimKnown {
		t.Fatal("input DTO was mutated")
	}
}

func TestHoneypotSimProbe_RPCError_LeavesFlagFalse(t *testing.T) {
	rpc := &fakeRPC{err: errors.New("rpc down")}
	p := NewHoneypotSimProbe(rpc, HoneypotSimConfig{
		Enabled:            true,
		SimulationContract: "0xSIM",
		TimeoutMs:          1000,
	}, nil)

	in := contracts.MarketDataDTO{TokenAddress: "0xabc"}
	out, err := p.Probe(context.Background(), in)
	if err == nil {
		t.Fatal("expected error from RPC failure")
	}
	if out.HoneypotSimKnown {
		t.Fatal("expected HoneypotSimKnown=false on RPC error")
	}
}
