package probes

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"testing"

	"crypto-sniping-bot/shared/contracts"
)

type batchStubRPC struct {
	stubSolanaRPC
	multiCalls int
	multiKeys  []string
}

func (s *batchStubRPC) GetMultipleAccounts(_ context.Context, pubkeys []string, _ string) ([]*SolanaAccountData, error) {
	s.multiCalls++
	s.multiKeys = append([]string(nil), pubkeys...)
	out := make([]*SolanaAccountData, len(pubkeys))
	for i, k := range pubkeys {
		out[i] = s.accounts[k]
	}
	return out, nil
}

func TestFetchBatchAccounts_PartialMissing_FailClosed(t *testing.T) {
	t.Parallel()
	mint := "TokenMintPubkey1111111111111111111111111111"
	pool := "BondingCurvePubkey111111111111111111111111"
	mintData := buildSPLMint(0, 0, 1_000_000_000, 6, true)

	rpc := &batchStubRPC{
		stubSolanaRPC: stubSolanaRPC{
			accounts: map[string]*SolanaAccountData{
				mint: {
					DataB64: base64.StdEncoding.EncodeToString(mintData),
					Owner:   "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA",
				},
			},
		},
	}

	res, err := FetchBatchAccounts(context.Background(), rpc, BatchAccountRequest{
		Mint:       mint,
		Pool:       pool,
		Commitment: "confirmed",
	})
	if err != nil {
		t.Fatalf("FetchBatchAccounts: %v", err)
	}
	if res.Mint == nil {
		t.Fatal("expected mint account")
	}
	if res.Pool != nil {
		t.Fatal("expected nil pool account for fail-closed partial response")
	}
	if rpc.multiCalls != 1 {
		t.Fatalf("expected 1 batch call, got %d", rpc.multiCalls)
	}

	in := contracts.MarketDataDTO{
		Chain:        "solana",
		Market:       "solana-pumpfun",
		TokenAddress: mint,
		PoolAddress:  pool,
	}
	out := ApplyBatchAccounts(context.Background(), in, res, nil, 0, true, true)
	if !out.SolanaAuthoritiesKnown {
		t.Fatal("expected authorities known from batch mint")
	}
	if out.LpStatsKnown {
		t.Fatal("expected LpStatsKnown=false when pool account missing")
	}
}

func TestApplyBatchAccounts_FullSuccess(t *testing.T) {
	t.Parallel()
	mint := "TokenMintPubkey1111111111111111111111111111"
	pool := "BondingCurvePubkey111111111111111111111111"
	mintData := buildSPLMint(0, 0, 1_000_000_000, 6, true)
	poolData := make([]byte, pumpfunBondingCurveSize)
	poolData[0] = 0xAA
	binary.LittleEndian.PutUint64(poolData[offsetVirtualSolRes:], 30_000_000_000)
	binary.LittleEndian.PutUint64(poolData[offsetRealSolRes:], 1_000_000_000)
	binary.LittleEndian.PutUint64(poolData[offsetTokenTotalSupply:], 1_000_000_000_000)

	res := &BatchAccountResult{
		Mint: &SolanaAccountData{
			DataB64: base64.StdEncoding.EncodeToString(mintData),
			Owner:   "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA",
		},
		Pool: &SolanaAccountData{DataB64: base64.StdEncoding.EncodeToString(poolData)},
	}
	in := contracts.MarketDataDTO{
		Chain:        "solana",
		Market:       "solana-pumpfun",
		TokenAddress: mint,
		PoolAddress:  pool,
	}
	out := ApplyBatchAccounts(context.Background(), in, res, fixedSolUsd{px: 150.0}, 0, true, true)
	if !out.SolanaAuthoritiesKnown {
		t.Fatal("expected SolanaAuthoritiesKnown")
	}
	if !out.LpStatsKnown {
		t.Fatal("expected LpStatsKnown")
	}
	if out.LiquidityUsd <= 0 {
		t.Fatalf("expected positive LiquidityUsd, got %f", out.LiquidityUsd)
	}
}

type fixedSolUsd struct {
	px float64
}

func (f fixedSolUsd) SolUsd(_ context.Context) (float64, bool) { return f.px, true }
