package ingestion_solana_test

// orca_whirlpool_test.go — tests for the Orca Whirlpool InitPool decoder (P4).

import (
	"testing"

	"crypto-sniping-bot/sniper-bot/internal/modules/ingestion_solana"
)

const orcaWhirlpoolProgramID = "whirLbMiicVdio4qvUfM5KAg6Ct8VwpYzGff3uctyCc"

// orcaInitPoolDiscriminator is SHA256("global:initialize_pool")[:8]
var orcaInitPoolDiscriminator = []byte{95, 180, 10, 172, 84, 174, 232, 40}

func makeOrcaInitPoolInstr(accounts []string) ingestion_solana.InstructionData {
	return ingestion_solana.InstructionData{
		ProgramID: orcaWhirlpoolProgramID,
		Data:      append(orcaInitPoolDiscriminator, 0x01),
		Accounts:  accounts,
		Index:     1,
	}
}

func TestOrcaWhirlpool_IsOrcaWhirlpoolInitPool_WrongDiscriminator_ReturnsFalse(t *testing.T) {
	t.Parallel()
	instr := ingestion_solana.InstructionData{
		ProgramID: orcaWhirlpoolProgramID,
		Data:      []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88},
		Accounts:  []string{"cfg", "mintA", "mintB", "funder", "pool"},
		Index:     0,
	}
	if ingestion_solana.IsOrcaWhirlpoolInitPool(instr, orcaWhirlpoolProgramID) {
		t.Fatal("expected false for wrong discriminator")
	}
}

func TestOrcaWhirlpool_IsOrcaWhirlpoolInitPool_WrongProgram_ReturnsFalse(t *testing.T) {
	t.Parallel()
	instr := makeOrcaInitPoolInstr([]string{"cfg", "mintA", "mintB", "funder", "pool"})
	if ingestion_solana.IsOrcaWhirlpoolInitPool(instr, "differentProgramID") {
		t.Fatal("expected false for wrong programID")
	}
}

func TestOrcaWhirlpool_DecodeOrcaWhirlpoolInitPool_InsufficientAccounts_ReturnsError(t *testing.T) {
	t.Parallel()
	instr := makeOrcaInitPoolInstr([]string{"cfg", "mintA"}) // needs at least 5 accounts
	event, err := ingestion_solana.DecodeOrcaWhirlpoolInitPool(instr)
	if err == nil {
		t.Fatal("expected error for insufficient accounts")
	}
	if event != nil {
		t.Fatal("expected nil event on error")
	}
}

func TestOrcaWhirlpool_NormalizeOrcaWhirlpoolInitPool_ValidEvent_ReturnsDTO(t *testing.T) {
	t.Parallel()
	// Account layout: 0=cfg, 1=tokenMintA, 2=tokenMintB, 3=funder, 4=pool
	accounts := []string{"whirlpoolsCfg", "mintA", "mintB", "funderAddr", "poolAddr"}
	instr := makeOrcaInitPoolInstr(accounts)
	tx := &ingestion_solana.TransactionResult{
		Signature:       "orca_sig",
		Slot:            42000,
		RecentBlockhash: "orchash",
		BlockTime:       1700000001,
	}

	dto, err := ingestion_solana.NormalizeOrcaWhirlpoolInitPool(tx, instr, "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto == nil {
		t.Fatal("expected non-nil DTO")
	}
	if dto.Market != "solana-orca-whirlpool" {
		t.Errorf("Market = %q, want solana-orca-whirlpool", dto.Market)
	}
	if dto.EventTopic != "OrcaWhirlpoolInitPool" {
		t.Errorf("EventTopic = %q, want OrcaWhirlpoolInitPool", dto.EventTopic)
	}
	if dto.PoolAddress != "poolAddr" {
		t.Errorf("PoolAddress = %q, want poolAddr", dto.PoolAddress)
	}
	if dto.TokenAddress != "mintA" {
		t.Errorf("TokenAddress = %q, want mintA", dto.TokenAddress)
	}
	if dto.CreatorAddress != "funderAddr" {
		t.Errorf("CreatorAddress = %q, want funderAddr", dto.CreatorAddress)
	}
}
