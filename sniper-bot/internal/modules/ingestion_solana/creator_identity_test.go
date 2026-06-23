package ingestion_solana_test

// creator_identity_test.go — Tests for the factory-program creator identity
// guard (Task 7). Verifies that IsFactoryProgram and GuardCreatorAddress
// correctly detect and correct factory-program IDs before DTOs are emitted.

import (
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/sniper-bot/internal/modules/ingestion_solana"
)

const (
	// pumpFunBondingCurveProgram is the pump.fun bonding-curve factory program ID.
	pumpFunBondingCurveProgram = "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P"
	// pumpFunAMMProgram is the pump.fun AMM graduation factory program ID.
	pumpFunAMMProgram = "pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA"
	// realHumanWallet is a valid 44-char Solana base58 address used as a human wallet fixture.
	realHumanWallet = "HumanWallet1111111111111111111111111111111111"
)

// TestIsFactoryProgram_BothProgramIDsDetected verifies that both known
// pump.fun factory program IDs are recognised by IsFactoryProgram.
func TestIsFactoryProgram_BothProgramIDsDetected(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want bool
	}{
		{"bonding_curve_program", pumpFunBondingCurveProgram, true},
		{"amm_program", pumpFunAMMProgram, true},
		{"real_wallet", realHumanWallet, false},
		{"empty_string", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ingestion_solana.IsFactoryProgram(tt.addr); got != tt.want {
				t.Errorf("IsFactoryProgram(%q) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

// TestNormalize_FactoryCreatorIsReplacedByEventUser verifies that when
// GuardCreatorAddress is called with a factory program as the creator but a
// valid human-wallet fallback, the fallback is returned (not the factory program).
// This mirrors the code path where NormalizePumpFunCreateFromLogs returns a DTO
// whose CreatorAddress turned out to be a factory program and a human-wallet
// fallback is available (e.g., from the transaction fee payer).
func TestNormalize_FactoryCreatorIsReplacedByEventUser(t *testing.T) {
	resolved, unresolvable := ingestion_solana.GuardCreatorAddress(pumpFunBondingCurveProgram, realHumanWallet)
	if unresolvable {
		t.Fatal("expected resolvable=true when valid fallback is provided, got unresolvable=true")
	}
	if resolved != realHumanWallet {
		t.Errorf("want resolved=%q, got %q", realHumanWallet, resolved)
	}
}

// TestResolveCreatorIdentity_EmptyCreatorUsesFeePayer verifies empty creators
// are populated from the transaction fee payer when it is a human wallet.
func TestResolveCreatorIdentity_EmptyCreatorUsesFeePayer(t *testing.T) {
	md := &contracts.MarketDataDTO{}
	if !ingestion_solana.ResolveCreatorIdentity(md, realHumanWallet) {
		t.Fatal("expected resolution from fee payer fallback")
	}
	if md.CreatorAddress != realHumanWallet {
		t.Errorf("want creator %q, got %q", realHumanWallet, md.CreatorAddress)
	}
}

// TestResolveCreatorIdentity_FactoryFeePayerStaysEmpty verifies factory program
// fee payers are not used as creator identity.
func TestResolveCreatorIdentity_FactoryFeePayerStaysEmpty(t *testing.T) {
	md := &contracts.MarketDataDTO{}
	if ingestion_solana.ResolveCreatorIdentity(md, pumpFunAMMProgram) {
		t.Fatal("factory program must not resolve as creator")
	}
	if md.CreatorAddress != "" {
		t.Errorf("want empty creator, got %q", md.CreatorAddress)
	}
}

// TestNormalize_UnknownCreatorFallsToEmpty verifies that when GuardCreatorAddress
// is called with a factory program as creator and no valid fallback, the resolved
// address is "" and unresolvable=true. This ensures factory program IDs never
// reach the creator reputation probe as a human wallet identity.
func TestNormalize_UnknownCreatorFallsToEmpty(t *testing.T) {
	resolved, unresolvable := ingestion_solana.GuardCreatorAddress(pumpFunBondingCurveProgram, "")
	if !unresolvable {
		t.Fatal("expected unresolvable=true when no fallback is available, got false")
	}
	if resolved != "" {
		t.Errorf("want resolved=%q (empty), got %q", "", resolved)
	}
}

// TestGuardCreatorAddress_NonFactoryPassesThrough verifies that a real human
// wallet is returned unchanged with unresolvable=false.
func TestGuardCreatorAddress_NonFactoryPassesThrough(t *testing.T) {
	resolved, unresolvable := ingestion_solana.GuardCreatorAddress(realHumanWallet, "")
	if unresolvable {
		t.Fatal("non-factory creator must not be flagged as unresolvable")
	}
	if resolved != realHumanWallet {
		t.Errorf("want %q unchanged, got %q", realHumanWallet, resolved)
	}
}

// TestGuardCreatorAddress_AMMProgramClearedWhenNoFallback verifies the AMM
// program ID is also caught and cleared to "" when no fallback is available.
func TestGuardCreatorAddress_AMMProgramClearedWhenNoFallback(t *testing.T) {
	resolved, unresolvable := ingestion_solana.GuardCreatorAddress(pumpFunAMMProgram, "")
	if !unresolvable {
		t.Fatal("expected unresolvable=true for AMM program + empty fallback")
	}
	if resolved != "" {
		t.Errorf("want empty string, got %q", resolved)
	}
}

// TestGuardCreatorAddress_FallbackAlsoFactoryProgramClearsToEmpty verifies
// that when both the creator AND the fallback are factory program IDs, the
// result is cleared to "" rather than substituting one factory program for
// another.
func TestGuardCreatorAddress_FallbackAlsoFactoryProgramClearsToEmpty(t *testing.T) {
	resolved, unresolvable := ingestion_solana.GuardCreatorAddress(pumpFunBondingCurveProgram, pumpFunAMMProgram)
	if !unresolvable {
		t.Fatal("expected unresolvable=true when fallback is also a factory program")
	}
	if resolved != "" {
		t.Errorf("want empty string when both creator and fallback are factory programs, got %q", resolved)
	}
}

// TestGuardCreatorAddress_AMMProgramReplacedByFallback verifies that the AMM
// program ID is also corrected to a real wallet when a valid fallback is provided.
func TestGuardCreatorAddress_AMMProgramReplacedByFallback(t *testing.T) {
	resolved, unresolvable := ingestion_solana.GuardCreatorAddress(pumpFunAMMProgram, realHumanWallet)
	if unresolvable {
		t.Fatal("expected resolvable=true when valid fallback is provided for AMM program")
	}
	if resolved != realHumanWallet {
		t.Errorf("want resolved=%q, got %q", realHumanWallet, resolved)
	}
}
