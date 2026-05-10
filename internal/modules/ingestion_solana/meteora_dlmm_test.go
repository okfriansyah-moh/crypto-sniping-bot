package ingestion_solana_test

// meteora_dlmm_test.go — tests for the Meteora DLMM InitLbPair decoder (P4).

import (
	"testing"

	"crypto-sniping-bot/internal/modules/ingestion_solana"
)

const meteoraDLMMProgramID = "LBUZKhRxPF3XUpBCjp4YzTKgLLjLeNox4HgSehp9ZSe"

// meteoraInitLbPairDiscriminator is SHA256("global:initialize_lb_pair")[:8]
var meteoraInitLbPairDiscriminator = []byte{110, 106, 20, 253, 63, 145, 232, 63}

func makeMeteoraDLMMInitLbPairInstr(accounts []string) ingestion_solana.InstructionData {
	return ingestion_solana.InstructionData{
		ProgramID: meteoraDLMMProgramID,
		Data:      append(meteoraInitLbPairDiscriminator, 0x00),
		Accounts:  accounts,
		Index:     2,
	}
}

func TestMeteoraDLMM_IsMeteoraDLMMInitLbPair_WrongDiscriminator_ReturnsFalse(t *testing.T) {
	t.Parallel()
	instr := ingestion_solana.InstructionData{
		ProgramID: meteoraDLMMProgramID,
		Data:      []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00, 0x11},
		Accounts:  make([]string, 14),
		Index:     0,
	}
	if ingestion_solana.IsMeteoraDLMMInitLbPair(instr, meteoraDLMMProgramID) {
		t.Fatal("expected false for wrong discriminator")
	}
}

func TestMeteoraDLMM_IsMeteoraDLMMInitLbPair_WrongProgram_ReturnsFalse(t *testing.T) {
	t.Parallel()
	accts := make([]string, 14)
	instr := makeMeteoraDLMMInitLbPairInstr(accts)
	if ingestion_solana.IsMeteoraDLMMInitLbPair(instr, "wrongProgram") {
		t.Fatal("expected false for wrong programID")
	}
}

func TestMeteoraDLMM_DecodeMeteoraDLMMInitLbPair_InsufficientAccounts_ReturnsError(t *testing.T) {
	t.Parallel()
	// TokenMintY is at index 3 — Decode returns error when len(accounts) <= 3.
	instr := makeMeteoraDLMMInitLbPairInstr(make([]string, 2))
	event, err := ingestion_solana.DecodeMeteoraDLMMInitLbPair(instr)
	if err == nil {
		t.Fatal("expected error for insufficient accounts")
	}
	if event != nil {
		t.Fatal("expected nil event on error")
	}
}

func TestMeteoraDLMM_NormalizeMeteoraDLMMInitLbPair_ValidEvent_ReturnsDTO(t *testing.T) {
	t.Parallel()
	// 14 accounts: lbPair(0), bitmap(1), mintX(2), mintY(3), resX(4), resY(5),
	// oracle(6), preset(7), funder(8), tokenProg(9), sysProg(10), rent(11), evtAuth(12), prog(13)
	accounts := []string{
		"lbPairAddr", "bitmapExt", "mintXAddr", "mintYAddr",
		"reserveX", "reserveY", "oracle", "preset",
		"funderAddr", "tokenProg", "sysProg", "rent", "evtAuth", "progAddr",
	}
	instr := makeMeteoraDLMMInitLbPairInstr(accounts)
	tx := &ingestion_solana.TransactionResult{
		Signature:       "meteora_sig",
		Slot:            55000,
		RecentBlockhash: "mehash",
		BlockTime:       1700000002,
	}

	dto, err := ingestion_solana.NormalizeMeteoraDLMMInitLbPair(tx, instr, "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto == nil {
		t.Fatal("expected non-nil DTO")
	}
	if dto.Market != "solana-meteora-dlmm" {
		t.Errorf("Market = %q, want solana-meteora-dlmm", dto.Market)
	}
	if dto.EventTopic != "MeteoraDLMMInitLbPair" {
		t.Errorf("EventTopic = %q, want MeteoraDLMMInitLbPair", dto.EventTopic)
	}
	if dto.PoolAddress != "lbPairAddr" {
		t.Errorf("PoolAddress = %q, want lbPairAddr", dto.PoolAddress)
	}
	if dto.TokenAddress != "mintXAddr" {
		t.Errorf("TokenAddress = %q, want mintXAddr", dto.TokenAddress)
	}
	if dto.BaseAddress != "mintYAddr" {
		t.Errorf("BaseAddress = %q, want mintYAddr", dto.BaseAddress)
	}
	if dto.CreatorAddress != "funderAddr" {
		t.Errorf("CreatorAddress = %q, want funderAddr", dto.CreatorAddress)
	}
}
