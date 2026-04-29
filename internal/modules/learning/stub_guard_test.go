// Phase 9 (Profitability Restoration § 9.7) — stub guard tests.
package learning

import (
	"math"
	"testing"

	"crypto-sniping-bot/contracts"
)

func TestAllStubs_AllHalfHalf_True(t *testing.T) {
	f := contracts.FeatureDTO{
		TxVelocityScore: 0.5, WalletEntropy: 0.5,
		VolumeMomentum: 0.5, PriceMomentum: 0.5,
	}
	if !AllStubs(f) {
		t.Fatal("expected AllStubs=true for all-0.5 vector")
	}
}

func TestAllStubs_AnyDifferent_False(t *testing.T) {
	cases := []contracts.FeatureDTO{
		{TxVelocityScore: 0.6, WalletEntropy: 0.5, VolumeMomentum: 0.5, PriceMomentum: 0.5},
		{TxVelocityScore: 0.5, WalletEntropy: 0.4, VolumeMomentum: 0.5, PriceMomentum: 0.5},
		{TxVelocityScore: 0.5, WalletEntropy: 0.5, VolumeMomentum: 0.0, PriceMomentum: 0.5},
		{TxVelocityScore: 0.5, WalletEntropy: 0.5, VolumeMomentum: 0.5, PriceMomentum: 1.0},
	}
	for i, f := range cases {
		if AllStubs(f) {
			t.Errorf("case %d: expected AllStubs=false", i)
		}
	}
}

func TestAllStubs_NaN_NotAStub(t *testing.T) {
	f := contracts.FeatureDTO{
		TxVelocityScore: math.NaN(), WalletEntropy: 0.5,
		VolumeMomentum: 0.5, PriceMomentum: 0.5,
	}
	if AllStubs(f) {
		t.Fatal("NaN must not count as stub")
	}
}

func TestAllStubs_ZeroVector_False(t *testing.T) {
	if AllStubs(contracts.FeatureDTO{}) {
		t.Fatal("zero vector is cold-start, not stub")
	}
}
