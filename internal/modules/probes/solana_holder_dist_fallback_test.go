package probes

import (
	"context"
	"errors"
	"testing"

	"crypto-sniping-bot/contracts"
)

// fallbackStubRPC implements both SolanaProbeRPCClient and holderDistFallbackClient
// so the type assertion in SolanaHolderDistProbe.Probe() succeeds and the fallback
// path is exercised. Existing stubSolanaRPC (in solana_authorities_test.go) does
// NOT implement holderDistFallbackClient, so existing tests are unaffected.
type fallbackStubRPC struct {
	// Primary call — controls getTokenLargestAccounts response.
	primaryErr error

	// Fallback calls.
	supplyAmount   uint64
	supplyDecimals int
	supplyErr      error

	programAccounts []SolanaTokenAccount
	programErr      error
}

// SolanaProbeRPCClient methods.
func (s *fallbackStubRPC) GetAccountInfo(_ context.Context, _, _ string) (*SolanaAccountData, error) {
	// Return a mock SPL v1 account so the precheck passes without a real RPC call.
	return &SolanaAccountData{Owner: splTokenV1Program}, nil
}

func (s *fallbackStubRPC) GetTokenLargestAccounts(_ context.Context, _, _ string) ([]SolanaTokenHolder, error) {
	if s.primaryErr != nil {
		return nil, s.primaryErr
	}
	return nil, nil
}

func (s *fallbackStubRPC) GetDASAsset(_ context.Context, _ string) (*DASAsset, error) {
	return nil, nil
}

// holderDistFallbackClient methods.
func (s *fallbackStubRPC) GetTokenSupply(_ context.Context, _, _ string) (uint64, int, error) {
	return s.supplyAmount, s.supplyDecimals, s.supplyErr
}

func (s *fallbackStubRPC) GetProgramAccounts(_ context.Context, _, _ string, _ []RPCProgramAccountsFilter) ([]SolanaTokenAccount, error) {
	return s.programAccounts, s.programErr
}

// TestSolanaHolderDist_FallbackOnPrimaryTimeout verifies that when
// getTokenLargestAccounts fails (simulating a timeout) and FallbackEnabled=true,
// the probe calls getTokenSupply + getProgramAccounts and sets HolderDistKnown=true.
func TestSolanaHolderDist_FallbackOnPrimaryTimeout(t *testing.T) {
	const testMint = "So11111111111111111111111111111111111111112"

	// Primary times out; fallback returns 10 accounts with known supply.
	stub := &fallbackStubRPC{
		primaryErr: errors.New("context deadline exceeded"),
		// Supply: 1_000_000_000 (raw atomic units)
		supplyAmount:   1_000_000_000,
		supplyDecimals: 9,
		// 10 holders: accounts 0–4 have 100M each (500M total = 50% top-5),
		//              accounts 5–9 have 10M each.
		programAccounts: []SolanaTokenAccount{
			{Pubkey: "addr1", Amount: 100_000_000},
			{Pubkey: "addr2", Amount: 100_000_000},
			{Pubkey: "addr3", Amount: 100_000_000},
			{Pubkey: "addr4", Amount: 100_000_000},
			{Pubkey: "addr5", Amount: 100_000_000},
			{Pubkey: "addr6", Amount: 10_000_000},
			{Pubkey: "addr7", Amount: 10_000_000},
			{Pubkey: "addr8", Amount: 10_000_000},
			{Pubkey: "addr9", Amount: 10_000_000},
			{Pubkey: "addrA", Amount: 10_000_000},
		},
	}

	probe := NewSolanaHolderDistProbe(stub, SolanaHolderDistConfig{
		Enabled:                    true,
		TimeoutMs:                  100,
		Commitment:                 "confirmed",
		TopK:                       5,
		CacheTTLSec:                -1, // disable cache for test isolation
		FallbackEnabled:            true,
		FallbackTimeoutMs:          500,
		FallbackMaxProgramAccounts: 200,
	}, nil)

	in := contracts.MarketDataDTO{
		Chain:        "solana",
		TokenAddress: testMint,
	}
	out, err := probe.Probe(context.Background(), in)

	if err != nil {
		t.Fatalf("Probe() unexpected error: %v", err)
	}
	if !out.HolderDistKnown {
		t.Fatal("HolderDistKnown should be true after successful fallback")
	}
	if out.HolderCount != 10 {
		t.Errorf("HolderCount = %d, want 10", out.HolderCount)
	}
	// Top-5 each have 100M, supply = 1B → 500M/1B = 0.5
	const wantPct = 0.5
	if out.Top5HolderPct < wantPct-0.001 || out.Top5HolderPct > wantPct+0.001 {
		t.Errorf("Top5HolderPct = %.4f, want %.4f", out.Top5HolderPct, wantPct)
	}
}

// TestSolanaHolderDist_FailClosedOnBothFail verifies that when both the primary
// and the fallback fail, the probe returns HolderDistKnown=false and no error
// (fail-closed semantics).
func TestSolanaHolderDist_FailClosedOnBothFail(t *testing.T) {
	const testMint = "So11111111111111111111111111111111111111112"

	stub := &fallbackStubRPC{
		primaryErr: errors.New("context deadline exceeded"),
		supplyErr:  errors.New("rpc error -32000: server unavailable"),
	}

	probe := NewSolanaHolderDistProbe(stub, SolanaHolderDistConfig{
		Enabled:                    true,
		TimeoutMs:                  100,
		Commitment:                 "confirmed",
		TopK:                       5,
		CacheTTLSec:                -1,
		FallbackEnabled:            true,
		FallbackTimeoutMs:          500,
		FallbackMaxProgramAccounts: 200,
	}, nil)

	in := contracts.MarketDataDTO{
		Chain:        "solana",
		TokenAddress: testMint,
	}
	out, err := probe.Probe(context.Background(), in)

	if err != nil {
		t.Fatalf("Probe() unexpected error: %v", err)
	}
	if out.HolderDistKnown {
		t.Fatal("HolderDistKnown should be false when both primary and fallback fail")
	}
}

// TestSolanaHolderDist_FallbackDisabledByConfig verifies that when FallbackEnabled=false,
// the fallback is never invoked even if the primary times out, and the probe returns
// an error wrapping the primary failure.
func TestSolanaHolderDist_FallbackDisabledByConfig(t *testing.T) {
	const testMint = "So11111111111111111111111111111111111111112"

	stub := &fallbackStubRPC{
		primaryErr: errors.New("context deadline exceeded"),
		// These would succeed if called — but they must NOT be called.
		supplyAmount:    1_000_000_000,
		programAccounts: []SolanaTokenAccount{{Pubkey: "addr1", Amount: 500_000_000}},
	}

	probe := NewSolanaHolderDistProbe(stub, SolanaHolderDistConfig{
		Enabled:                    true,
		TimeoutMs:                  100,
		Commitment:                 "confirmed",
		TopK:                       5,
		CacheTTLSec:                -1,
		FallbackEnabled:            false, // disabled
		FallbackTimeoutMs:          500,
		FallbackMaxProgramAccounts: 200,
	}, nil)

	in := contracts.MarketDataDTO{
		Chain:        "solana",
		TokenAddress: testMint,
	}
	out, err := probe.Probe(context.Background(), in)

	if err == nil {
		t.Fatal("Probe() expected an error when fallback disabled and primary fails")
	}
	if out.HolderDistKnown {
		t.Fatal("HolderDistKnown should remain false when fallback is disabled")
	}
}
