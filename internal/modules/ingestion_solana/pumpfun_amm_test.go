package ingestion_solana_test

// pumpfun_amm_test.go — tests for the PumpFun AMM CreatePool decoder (P4).
// Verifies: wrong discriminator → Is* returns false; insufficient accounts → error;
// valid instruction → correct MarketDataDTO field values.

import (
	"testing"

	"crypto-sniping-bot/internal/modules/ingestion_solana"
)

const pumpfunAMMProgramID = "pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA"

// pumpfunCreatePoolDiscriminator is SHA256("global:create_pool")[:8]
var pumpfunCreatePoolDiscriminator = []byte{233, 146, 209, 142, 207, 104, 64, 188}

func makePumpFunAMMCreatePoolInstr(accounts []string) ingestion_solana.InstructionData {
	return ingestion_solana.InstructionData{
		ProgramID: pumpfunAMMProgramID,
		Data:      append(pumpfunCreatePoolDiscriminator, 0x01, 0x02), // extra bytes OK
		Accounts:  accounts,
		Index:     0,
	}
}

func TestPumpFunAMM_IsPumpFunAMMCreatePool_WrongDiscriminator_ReturnsFalse(t *testing.T) {
	t.Parallel()
	instr := ingestion_solana.InstructionData{
		ProgramID: pumpfunAMMProgramID,
		Data:      []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
		Accounts:  []string{"pool", "cfg", "creator", "baseMint", "quoteMint"},
		Index:     0,
	}
	if ingestion_solana.IsPumpFunAMMCreatePool(instr, pumpfunAMMProgramID) {
		t.Fatal("expected false for wrong discriminator")
	}
}

func TestPumpFunAMM_IsPumpFunAMMCreatePool_WrongProgram_ReturnsFalse(t *testing.T) {
	t.Parallel()
	instr := makePumpFunAMMCreatePoolInstr([]string{"pool", "cfg", "creator", "baseMint", "quoteMint"})
	if ingestion_solana.IsPumpFunAMMCreatePool(instr, "wrongProgramID") {
		t.Fatal("expected false for wrong programID")
	}
}

func TestPumpFunAMM_DecodePumpFunAMMCreatePool_InsufficientAccounts_ReturnsError(t *testing.T) {
	t.Parallel()
	instr := makePumpFunAMMCreatePoolInstr([]string{"pool", "cfg", "creator"}) // missing index 3
	event, err := ingestion_solana.DecodePumpFunAMMCreatePool(instr)
	if err == nil {
		t.Fatal("expected error for insufficient accounts")
	}
	if event != nil {
		t.Fatal("expected nil event on error")
	}
}

func TestPumpFunAMM_NormalizePumpFunAMMCreatePool_ValidEvent_ReturnsDTO(t *testing.T) {
	t.Parallel()
	accounts := []string{"poolAddr", "globalCfg", "creatorAddr", "baseMintAddr", "quoteMintAddr"}
	instr := makePumpFunAMMCreatePoolInstr(accounts)
	tx := &ingestion_solana.TransactionResult{
		Signature:       "sig123",
		Slot:            12345,
		RecentBlockhash: "bhash",
		BlockTime:       1700000000,
	}

	dto, err := ingestion_solana.NormalizePumpFunAMMCreatePool(tx, instr, "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto == nil {
		t.Fatal("expected non-nil DTO")
	}
	if dto.Market != "solana-pumpfun-amm" {
		t.Errorf("Market = %q, want solana-pumpfun-amm", dto.Market)
	}
	if dto.EventTopic != "PumpFunAMMCreatePool" {
		t.Errorf("EventTopic = %q, want PumpFunAMMCreatePool", dto.EventTopic)
	}
	if dto.PoolAddress != "poolAddr" {
		t.Errorf("PoolAddress = %q, want poolAddr", dto.PoolAddress)
	}
	if dto.TokenAddress != "baseMintAddr" {
		t.Errorf("TokenAddress = %q, want baseMintAddr", dto.TokenAddress)
	}
	if dto.CreatorAddress != "creatorAddr" {
		t.Errorf("CreatorAddress = %q, want creatorAddr", dto.CreatorAddress)
	}
}
