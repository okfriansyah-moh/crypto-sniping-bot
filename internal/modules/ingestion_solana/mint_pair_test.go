package ingestion_solana_test

import (
	"testing"

	"crypto-sniping-bot/internal/modules/ingestion_solana"
)

func TestResolveTradableMint(t *testing.T) {
	const wsol = ingestion_solana.WrappedSOLMint
	const pump = "K93mdxqMgivPNTFEXnoUmN8WH5tNzrSJfaguevQpump"

	tests := []struct {
		name      string
		base      string
		quote     string
		wantToken string
		wantBase  string
		wantOK    bool
	}{
		{
			name:      "base WSOL quote pump",
			base:      wsol,
			quote:     pump,
			wantToken: pump,
			wantBase:  wsol,
			wantOK:    true,
		},
		{
			name:      "base pump quote WSOL",
			base:      pump,
			quote:     wsol,
			wantToken: pump,
			wantBase:  wsol,
			wantOK:    true,
		},
		{
			name:   "both WSOL",
			base:   wsol,
			quote:  wsol,
			wantOK: false,
		},
		{
			name:   "empty base",
			base:   "",
			quote:  pump,
			wantOK: false,
		},
		{
			name:      "neither WSOL IDL default",
			base:      "baseMintAddr",
			quote:     "quoteMintAddr",
			wantToken: "baseMintAddr",
			wantBase:  "quoteMintAddr",
			wantOK:    true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			token, base, ok := ingestion_solana.ResolveTradableMint(tc.base, tc.quote)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if token != tc.wantToken {
				t.Errorf("tokenMint = %q, want %q", token, tc.wantToken)
			}
			if base != tc.wantBase {
				t.Errorf("baseAddr = %q, want %q", base, tc.wantBase)
			}
		})
	}
}

func TestIsSystemMint_WSOLOnly(t *testing.T) {
	// Cannot t.Parallel() — modifies global state via ConfigureStableMints()
	ingestion_solana.ConfigureStableMints([]string{ingestion_solana.WrappedSOLMint})
	if !ingestion_solana.IsSystemMint(ingestion_solana.WrappedSOLMint) {
		t.Fatal("expected WSOL to be system mint")
	}
	if ingestion_solana.IsSystemMint("K93mdxqMgivPNTFEXnoUmN8WH5tNzrSJfaguevQpump") {
		t.Fatal("expected pump mint not to be system mint")
	}
}

func TestMintPairWasSwapped(t *testing.T) {
	// Cannot t.Parallel() — reads global state via mints configured in another test
	if !ingestion_solana.MintPairWasSwapped(ingestion_solana.WrappedSOLMint, "MemeMint1111111111111111111111111111111") {
		t.Error("expected swap when WSOL is base and quote is project mint")
	}
	if ingestion_solana.MintPairWasSwapped("MemeMint1111111111111111111111111111111", ingestion_solana.WrappedSOLMint) {
		t.Error("expected no swap when project mint is base and WSOL is quote")
	}
}

func TestResolveTradableMint_BaseUSDC_QuotePump(t *testing.T) {
	// Cannot t.Parallel() — modifies global state via ConfigureStableMints()
	const usdc = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	const pump = "K93mdxqMgivPNTFEXnoUmN8WH5tNzrSJfaguevQpump"
	ingestion_solana.ConfigureStableMints([]string{ingestion_solana.WrappedSOLMint, usdc})

	token, base, ok := ingestion_solana.ResolveTradableMint(usdc, pump)
	if !ok {
		t.Fatal("expected ok=true for USDC+pump pair")
	}
	if token != pump || base != usdc {
		t.Fatalf("got token=%q base=%q, want token=%q base=%q", token, base, pump, usdc)
	}
}
