package ingestion_solana_test

// raydium_swap_test.go — tests for DecodeRaydiumSwap and NormalizeRaydiumSwap.

import (
	"encoding/binary"
	"testing"

	"crypto-sniping-bot/internal/modules/ingestion_solana"
)

// buildRaydiumSwapData builds a synthetic Raydium swap instruction payload.
func buildRaydiumSwapData(disc [8]byte, amountIn, minOut uint64) []byte {
	var buf []byte
	buf = append(buf, disc[:]...)
	b8 := make([]byte, 8)
	binary.LittleEndian.PutUint64(b8, amountIn)
	buf = append(buf, b8...)
	binary.LittleEndian.PutUint64(b8, minOut)
	buf = append(buf, b8...)
	return buf
}

// ── DecodeRaydiumSwap ─────────────────────────────────────────────────────────

func TestDecodeRaydiumSwap_SwapBaseIn_HappyPath(t *testing.T) {
	t.Parallel()
	data := buildRaydiumSwapData(ingestion_solana.RaydiumSwapBaseInDiscriminator, 1_000_000, 950_000)

	evt, err := ingestion_solana.DecodeRaydiumSwap(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt == nil {
		t.Fatal("expected non-nil event")
	}
	if evt.AmountIn != 1_000_000 {
		t.Errorf("AmountIn = %d, want 1_000_000", evt.AmountIn)
	}
	if evt.MinimumAmountOut != 950_000 {
		t.Errorf("MinimumAmountOut = %d, want 950_000", evt.MinimumAmountOut)
	}
}

func TestDecodeRaydiumSwap_SwapBaseOut_HappyPath(t *testing.T) {
	t.Parallel()
	data := buildRaydiumSwapData(ingestion_solana.RaydiumSwapBaseOutDiscriminator, 2_000_000, 1_900_000)

	evt, err := ingestion_solana.DecodeRaydiumSwap(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt == nil {
		t.Fatal("expected non-nil event")
	}
	if evt.AmountIn != 2_000_000 {
		t.Errorf("AmountIn = %d, want 2_000_000", evt.AmountIn)
	}
}

func TestDecodeRaydiumSwap_WrongDiscriminator(t *testing.T) {
	t.Parallel()
	// Use pool init discriminator — not a swap.
	data := buildRaydiumSwapData(ingestion_solana.RaydiumPoolInitDiscriminator, 1_000, 900)

	evt, err := ingestion_solana.DecodeRaydiumSwap(data)
	if err == nil {
		t.Fatal("expected error for wrong discriminator, got nil")
	}
	if evt != nil {
		t.Error("expected nil event for wrong discriminator")
	}
}

func TestDecodeRaydiumSwap_TruncatedData(t *testing.T) {
	t.Parallel()
	// Only discriminator, no body.
	data := ingestion_solana.RaydiumSwapBaseInDiscriminator[:]

	_, err := ingestion_solana.DecodeRaydiumSwap(data)
	if err == nil {
		t.Fatal("expected error for truncated data, got nil")
	}
}

// ── NormalizeRaydiumSwap ──────────────────────────────────────────────────────

func TestNormalizeRaydiumSwap_HappyPath(t *testing.T) {
	t.Parallel()
	data := buildRaydiumSwapData(ingestion_solana.RaydiumSwapBaseInDiscriminator, 500_000, 490_000)

	accounts := make([]string, 10)
	accounts[4] = "AmmPoolAddr1111111111111111111111111111111"

	tx := &ingestion_solana.TransactionResult{
		Signature: "SwapSig1111111111111111111111111111111111111",
		Slot:      77000,
		BlockTime: 1700001000,
		Instructions: []ingestion_solana.InstructionData{
			{Accounts: accounts, Data: data, Index: 0},
		},
	}

	dto, err := ingestion_solana.NormalizeRaydiumSwap(tx, tx.Instructions[0], "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto == nil {
		t.Fatal("expected non-nil DTO")
	}
	if dto.EventTopic != "Swap" {
		t.Errorf("EventTopic = %q, want Swap", dto.EventTopic)
	}
	if dto.PoolAddress != accounts[4] {
		t.Errorf("PoolAddress = %q, want %q", dto.PoolAddress, accounts[4])
	}
	if dto.Amount0Raw != "500000" {
		t.Errorf("Amount0Raw = %q, want 500000", dto.Amount0Raw)
	}
	if dto.Amount1Raw != "490000" {
		t.Errorf("Amount1Raw = %q, want 490000", dto.Amount1Raw)
	}
	if dto.Chain != "solana" {
		t.Errorf("Chain = %q, want solana", dto.Chain)
	}
	if dto.Market != "solana-raydium-v4" {
		t.Errorf("Market = %q, want solana-raydium-v4", dto.Market)
	}
}

func TestNormalizeRaydiumSwap_WrongDiscriminator_ReturnsNil(t *testing.T) {
	t.Parallel()
	// Use pool init discriminator — not a swap.
	data := buildRaydiumSwapData(ingestion_solana.RaydiumPoolInitDiscriminator, 100, 90)
	accounts := make([]string, 10)

	tx := &ingestion_solana.TransactionResult{
		Signature:    "Sig1",
		Instructions: []ingestion_solana.InstructionData{{Accounts: accounts, Data: data, Index: 0}},
	}
	dto, err := ingestion_solana.NormalizeRaydiumSwap(tx, tx.Instructions[0], "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto != nil {
		t.Error("expected nil DTO for wrong discriminator")
	}
}

func TestNormalizeRaydiumSwap_InsufficientAccounts_ReturnsNil(t *testing.T) {
	t.Parallel()
	data := buildRaydiumSwapData(ingestion_solana.RaydiumSwapBaseInDiscriminator, 100, 90)
	accounts := make([]string, 5) // fewer than 10 required

	tx := &ingestion_solana.TransactionResult{
		Signature:    "Sig2",
		Instructions: []ingestion_solana.InstructionData{{Accounts: accounts, Data: data, Index: 0}},
	}
	dto, err := ingestion_solana.NormalizeRaydiumSwap(tx, tx.Instructions[0], "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto != nil {
		t.Error("expected nil DTO for insufficient accounts")
	}
}
