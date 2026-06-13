package probes

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"testing"

	"crypto-sniping-bot/contracts"
)

// stubSolanaRPC is a minimal SolanaProbeRPCClient for table tests.
type stubSolanaRPC struct {
	accounts map[string]*SolanaAccountData
	holders  map[string][]SolanaTokenHolder
	accErr   error
	holdErr  error
}

func (s *stubSolanaRPC) GetAccountInfo(_ context.Context, pubkey, _ string) (*SolanaAccountData, error) {
	if s.accErr != nil {
		return nil, s.accErr
	}
	return s.accounts[pubkey], nil
}

func (s *stubSolanaRPC) GetMultipleAccounts(_ context.Context, pubkeys []string, _ string) ([]*SolanaAccountData, error) {
	if s.accErr != nil {
		return nil, s.accErr
	}
	out := make([]*SolanaAccountData, len(pubkeys))
	for i, k := range pubkeys {
		out[i] = s.accounts[k]
	}
	return out, nil
}

func (s *stubSolanaRPC) GetTokenLargestAccounts(_ context.Context, mint, _ string) ([]SolanaTokenHolder, error) {
	if s.holdErr != nil {
		return nil, s.holdErr
	}
	return s.holders[mint], nil
}

func (s *stubSolanaRPC) GetDASAsset(_ context.Context, _ string) (*DASAsset, error) {
	return nil, nil // not used by authorities/holder_dist/pumpfun tests
}

// buildSPLMint builds a minimal 82-byte SPL mint account with the
// requested authority options.
func buildSPLMint(mintOpt, freezeOpt uint32, supply uint64, decimals uint8, initialized bool) []byte {
	b := make([]byte, splMintAccountSize)
	binary.LittleEndian.PutUint32(b[offsetMintAuthorityOption:], mintOpt)
	// 32 bytes of zero pubkey are fine for this layout test.
	binary.LittleEndian.PutUint64(b[offsetSupply:], supply)
	b[offsetDecimals] = decimals
	if initialized {
		b[offsetIsInitialized] = 1
	}
	binary.LittleEndian.PutUint32(b[offsetFreezeAuthorityOption:], freezeOpt)
	return b
}

func TestDecodeSPLMint_RenouncedBoth(t *testing.T) {
	b := buildSPLMint(0, 0, 1_000_000_000, 6, true)
	st, err := DecodeSPLMint(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !st.MintAuthorityRenounced || !st.FreezeAuthorityRenounced {
		t.Fatalf("expected both renounced, got %+v", st)
	}
	if !st.IsInitialized || st.Decimals != 6 || st.Supply != 1_000_000_000 {
		t.Fatalf("unexpected mint state: %+v", st)
	}
}

func TestDecodeSPLMint_BothPresent(t *testing.T) {
	b := buildSPLMint(1, 1, 0, 9, true)
	st, err := DecodeSPLMint(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.MintAuthorityRenounced || st.FreezeAuthorityRenounced {
		t.Fatalf("expected neither renounced, got %+v", st)
	}
}

func TestDecodeSPLMint_TooShort(t *testing.T) {
	if _, err := DecodeSPLMint(make([]byte, 50)); err == nil {
		t.Fatal("expected error on short buffer")
	}
}

func TestSolanaAuthoritiesProbe_HappyPath(t *testing.T) {
	mint := "So11111111111111111111111111111111111111112"
	rpc := &stubSolanaRPC{accounts: map[string]*SolanaAccountData{
		mint: {
			DataB64: base64.StdEncoding.EncodeToString(buildSPLMint(0, 1, 1_000, 6, true)),
			Owner:   "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA",
		},
	}}
	probe := NewSolanaAuthoritiesProbe(rpc, SolanaAuthoritiesConfig{Enabled: true, TimeoutMs: 100}, nil)
	out, err := probe.Probe(context.Background(), contracts.MarketDataDTO{
		Chain: "solana", TokenAddress: mint,
	})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !out.SolanaAuthoritiesKnown {
		t.Fatal("SolanaAuthoritiesKnown not flipped")
	}
	if !out.MintAuthorityRenounced {
		t.Fatal("expected MintAuthorityRenounced=true (option=0)")
	}
	if out.FreezeAuthorityRenounced {
		t.Fatal("expected FreezeAuthorityRenounced=false (option=1)")
	}
}

func TestSolanaAuthoritiesProbe_SkipsEVM(t *testing.T) {
	probe := NewSolanaAuthoritiesProbe(&stubSolanaRPC{}, SolanaAuthoritiesConfig{Enabled: true}, nil)
	in := contracts.MarketDataDTO{Chain: "eth", TokenAddress: "0xabc"}
	out, err := probe.Probe(context.Background(), in)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if out.SolanaAuthoritiesKnown {
		t.Fatal("non-solana DTOs must pass through untouched")
	}
}

func TestSolanaAuthoritiesProbe_RejectsForeignOwner(t *testing.T) {
	mint := "FooMint"
	rpc := &stubSolanaRPC{accounts: map[string]*SolanaAccountData{
		mint: {DataB64: base64.StdEncoding.EncodeToString(buildSPLMint(0, 0, 0, 0, true)), Owner: "WrongProgram"},
	}}
	probe := NewSolanaAuthoritiesProbe(rpc, SolanaAuthoritiesConfig{Enabled: true}, nil)
	out, err := probe.Probe(context.Background(), contracts.MarketDataDTO{Chain: "solana", TokenAddress: mint})
	if err == nil {
		t.Fatal("expected error for non-Token-program owner")
	}
	if out.SolanaAuthoritiesKnown {
		t.Fatal("flag must remain false on owner mismatch")
	}
}

func TestSolanaAuthoritiesProbe_RPCError(t *testing.T) {
	rpc := &stubSolanaRPC{accErr: errors.New("boom")}
	probe := NewSolanaAuthoritiesProbe(rpc, SolanaAuthoritiesConfig{Enabled: true}, nil)
	out, err := probe.Probe(context.Background(), contracts.MarketDataDTO{Chain: "solana", TokenAddress: "x"})
	if err == nil {
		t.Fatal("expected RPC error to bubble up")
	}
	if out.SolanaAuthoritiesKnown {
		t.Fatal("flag must remain false on rpc error")
	}
}
