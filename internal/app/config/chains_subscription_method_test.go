package config_test

// chains_subscription_method_test.go — verifies that SolanaProgramConfig
// correctly parses subscription_method and account_filter from YAML.

import (
	"testing"

	"gopkg.in/yaml.v3"

	"crypto-sniping-bot/internal/app/config"
)

// TestProgramConfig_SubscriptionMethodDefaultsEmpty verifies that a
// SolanaProgramConfig parsed without subscription_method has an empty string,
// which the ingestion loop treats as the logsSubscribe default.
func TestProgramConfig_SubscriptionMethodDefaultsEmpty(t *testing.T) {
	const input = `
program_id: "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8"
family: "raydium-v4"
`
	var p config.SolanaProgramConfig
	if err := yaml.Unmarshal([]byte(input), &p); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if p.SubscriptionMethod != "" {
		t.Errorf("SubscriptionMethod should default to empty string, got %q", p.SubscriptionMethod)
	}
	if p.AccountFilter != "" {
		t.Errorf("AccountFilter should default to empty string, got %q", p.AccountFilter)
	}
}

// TestProgramConfig_SubscriptionMethodParsed verifies that subscription_method
// and account_filter are correctly round-tripped through YAML.
func TestProgramConfig_SubscriptionMethodParsed(t *testing.T) {
	const input = `
program_id: "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8"
family: "raydium-v4"
subscription_method: "transactionSubscribe"
account_filter: "5Q544fKrFoe6tsEbD7S8EmxGTJYAKtTVhAW5Q5pge4j1"
`
	var p config.SolanaProgramConfig
	if err := yaml.Unmarshal([]byte(input), &p); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if p.SubscriptionMethod != "transactionSubscribe" {
		t.Errorf("SubscriptionMethod: want %q, got %q", "transactionSubscribe", p.SubscriptionMethod)
	}
	const wantFilter = "5Q544fKrFoe6tsEbD7S8EmxGTJYAKtTVhAW5Q5pge4j1"
	if p.AccountFilter != wantFilter {
		t.Errorf("AccountFilter: want %q, got %q", wantFilter, p.AccountFilter)
	}
}

// TestProgramConfig_TransactionSubscribeShape verifies the full chains.yaml
// Raydium V4 entry (as written by Task 3) parses correctly in one shot.
func TestProgramConfig_TransactionSubscribeShape(t *testing.T) {
	// Mirrors the exact YAML fields added in config/chains.yaml by Task 3.
	const input = `
program_id: "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8"
family: "raydium-v4"
disabled: false
subscription_method: "transactionSubscribe"
account_filter: "5Q544fKrFoe6tsEbD7S8EmxGTJYAKtTVhAW5Q5pge4j1"
`
	var p config.SolanaProgramConfig
	if err := yaml.Unmarshal([]byte(input), &p); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if p.ProgramID != "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8" {
		t.Errorf("ProgramID mismatch: %q", p.ProgramID)
	}
	if p.Family != "raydium-v4" {
		t.Errorf("Family mismatch: %q", p.Family)
	}
	if p.Disabled {
		t.Error("Disabled should be false for raydium-v4")
	}
	if p.SubscriptionMethod != "transactionSubscribe" {
		t.Errorf("SubscriptionMethod: want %q, got %q", "transactionSubscribe", p.SubscriptionMethod)
	}
	if p.AccountFilter != "5Q544fKrFoe6tsEbD7S8EmxGTJYAKtTVhAW5Q5pge4j1" {
		t.Errorf("AccountFilter: want %q, got %q", "5Q544fKrFoe6tsEbD7S8EmxGTJYAKtTVhAW5Q5pge4j1", p.AccountFilter)
	}
}

// TestProgramConfig_PumpfunBondingCurveDisabled verifies that the pumpfun bonding-curve
// entry (as written by Task 3) parses with Disabled=true.
func TestProgramConfig_PumpfunBondingCurveDisabled(t *testing.T) {
	const input = `
program_id: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P"
family: "pumpfun"
disabled: true
`
	var p config.SolanaProgramConfig
	if err := yaml.Unmarshal([]byte(input), &p); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if !p.Disabled {
		t.Error("pumpfun bonding-curve entry must have Disabled=true")
	}
	if p.SubscriptionMethod != "" {
		t.Errorf("pumpfun bonding-curve should have empty SubscriptionMethod, got %q", p.SubscriptionMethod)
	}
}

// TestProgramConfig_PumpfunAMMRemainsActive verifies that pumpfun-amm is NOT disabled
// and uses the default logsSubscribe (no subscription_method override).
func TestProgramConfig_PumpfunAMMRemainsActive(t *testing.T) {
	const input = `
program_id: "pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA"
family: "pumpfun-amm"
`
	var p config.SolanaProgramConfig
	if err := yaml.Unmarshal([]byte(input), &p); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if p.Disabled {
		t.Error("pumpfun-amm (graduation events) must NOT be disabled")
	}
	if p.SubscriptionMethod != "" {
		t.Errorf("pumpfun-amm should have empty SubscriptionMethod (logsSubscribe default), got %q", p.SubscriptionMethod)
	}
}
