package ingestion_solana_test

// raydium_clmm_test.go — tests for the Raydium CLMM CreatePool decoder (P4).
//
// Raydium CLMM shares the create_pool discriminator with PumpFun AMM.
// Disambiguation is by program ID — tests verify this explicitly.

import (
	"testing"

	"crypto-sniping-bot/internal/modules/ingestion_solana"
)

const raydiumCLMMProgramID = "CAMMCzo5YL8w4VFF8KVHrK22GGUsp5VTaW7grrKgrWqK"

// sharedCreatePoolDiscriminator is SHA256("global:create_pool")[:8],
// shared by both PumpFun AMM and Raydium CLMM.
var sharedCreatePoolDiscriminator = []byte{233, 146, 209, 142, 207, 104, 64, 188}

func makeRaydiumCLMMCreatePoolInstr(accounts []string) ingestion_solana.InstructionData {
	return ingestion_solana.InstructionData{
		ProgramID: raydiumCLMMProgramID,
		Data:      append(sharedCreatePoolDiscriminator, 0x00),
		Accounts:  accounts,
		Index:     3,
	}
}

func TestRaydiumCLMM_IsRaydiumCLMMCreatePool_WrongDiscriminator_ReturnsFalse(t *testing.T) {
	t.Parallel()
	instr := ingestion_solana.InstructionData{
		ProgramID: raydiumCLMMProgramID,
		Data:      []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
		Accounts:  make([]string, 10),
		Index:     0,
	}
	if ingestion_solana.IsRaydiumCLMMCreatePool(instr, raydiumCLMMProgramID) {
		t.Fatal("expected false for wrong discriminator")
	}
}

func TestRaydiumCLMM_IsRaydiumCLMMCreatePool_WrongProgram_ReturnsFalse(t *testing.T) {
	t.Parallel()
	instr := makeRaydiumCLMMCreatePoolInstr(make([]string, 10))
	// PumpFun AMM has the same discriminator but different program ID — must return false.
	if ingestion_solana.IsRaydiumCLMMCreatePool(instr, pumpfunAMMProgramID) {
		t.Fatal("expected false: same discriminator but wrong programID")
	}
}

func TestRaydiumCLMM_IsRaydiumCLMMCreatePool_CorrectProgram_ReturnsTrue(t *testing.T) {
	t.Parallel()
	instr := makeRaydiumCLMMCreatePoolInstr(make([]string, 10))
	if !ingestion_solana.IsRaydiumCLMMCreatePool(instr, raydiumCLMMProgramID) {
		t.Fatal("expected true for correct discriminator + programID")
	}
}

func TestRaydiumCLMM_DecodeRaydiumCLMMCreatePool_InsufficientAccounts_ReturnsError(t *testing.T) {
	t.Parallel()
	// TokenMint1 is at index 4 — need at least 5 accounts
	instr := makeRaydiumCLMMCreatePoolInstr(make([]string, 3))
	event, err := ingestion_solana.DecodeRaydiumCLMMCreatePool(instr)
	if err == nil {
		t.Fatal("expected error for insufficient accounts")
	}
	if event != nil {
		t.Fatal("expected nil event on error")
	}
}

func TestRaydiumCLMM_NormalizeRaydiumCLMMCreatePool_ValidEvent_ReturnsDTO(t *testing.T) {
	t.Parallel()
	// 0=poolCreator, 1=ammCfg, 2=poolState, 3=tokenMint0, 4=tokenMint1
	accounts := []string{"creatorAddr", "ammCfg", "poolStateAddr", "mint0Addr", "mint1Addr"}
	instr := makeRaydiumCLMMCreatePoolInstr(accounts)
	tx := &ingestion_solana.TransactionResult{
		Signature:       "clmm_sig",
		Slot:            77777,
		RecentBlockhash: "clmmhash",
		BlockTime:       1700000003,
	}

	dto, err := ingestion_solana.NormalizeRaydiumCLMMCreatePool(tx, instr, "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto == nil {
		t.Fatal("expected non-nil DTO")
	}
	if dto.Market != "solana-raydium-clmm" {
		t.Errorf("Market = %q, want solana-raydium-clmm", dto.Market)
	}
	if dto.EventTopic != "RaydiumCLMMCreatePool" {
		t.Errorf("EventTopic = %q, want RaydiumCLMMCreatePool", dto.EventTopic)
	}
	if dto.PoolAddress != "poolStateAddr" {
		t.Errorf("PoolAddress = %q, want poolStateAddr", dto.PoolAddress)
	}
	if dto.TokenAddress != "mint0Addr" {
		t.Errorf("TokenAddress = %q, want mint0Addr", dto.TokenAddress)
	}
	if dto.BaseAddress != "mint1Addr" {
		t.Errorf("BaseAddress = %q, want mint1Addr", dto.BaseAddress)
	}
	if dto.CreatorAddress != "creatorAddr" {
		t.Errorf("CreatorAddress = %q, want creatorAddr", dto.CreatorAddress)
	}
}
