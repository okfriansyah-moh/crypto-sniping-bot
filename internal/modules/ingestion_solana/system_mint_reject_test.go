package ingestion_solana

import (
	"testing"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

func TestApplySystemMintReject_DropsConfiguredWSOL(t *testing.T) {
	t.Parallel()
	cfg := config.SolanaConfig{
		SystemMintReject: config.SystemMintRejectConfig{
			Enabled: true,
			Mints:   []string{WrappedSOLMint},
		},
	}
	m := New(cfg, "v1", nil, nil)
	dto := &contracts.MarketDataDTO{
		Chain:        "solana",
		Market:       "solana-pumpfun-amm",
		TokenAddress: WrappedSOLMint,
		TxHash:       "sig",
	}
	if !m.applySystemMintReject(dto) {
		t.Fatal("expected WSOL token to be rejected at L0")
	}
	if m.systemMintRejected.Load() != 1 {
		t.Fatalf("systemMintRejected = %d, want 1", m.systemMintRejected.Load())
	}
}

func TestApplySystemMintReject_DisabledPassesThrough(t *testing.T) {
	t.Parallel()
	cfg := config.SolanaConfig{
		SystemMintReject: config.SystemMintRejectConfig{
			Enabled: false,
			Mints:   []string{WrappedSOLMint},
		},
	}
	m := New(cfg, "v1", nil, nil)
	dto := &contracts.MarketDataDTO{TokenAddress: WrappedSOLMint}
	if m.applySystemMintReject(dto) {
		t.Fatal("expected WSOL to pass when system_mint_reject.enabled=false")
	}
}

func TestApplySystemMintReject_AllowsMemeMint(t *testing.T) {
	t.Parallel()
	cfg := config.SolanaConfig{
		SystemMintReject: config.SystemMintRejectConfig{Enabled: true, Mints: []string{WrappedSOLMint}},
	}
	m := New(cfg, "v1", nil, nil)
	dto := &contracts.MarketDataDTO{
		TokenAddress: "K93mdxqMgivPNTFEXnoUmN8WH5tNzrSJfaguevQpump",
	}
	if m.applySystemMintReject(dto) {
		t.Fatal("expected valid meme mint to pass system mint guard")
	}
}
