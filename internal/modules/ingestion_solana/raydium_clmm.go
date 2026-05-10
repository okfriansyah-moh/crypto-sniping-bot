package ingestion_solana

// raydium_clmm.go — Raydium CLMM (Concentrated Liquidity Market Maker) decoder.
//
// Program ID: CAMMCzo5YL8w4VFF8KVHrK22GGUsp5VTaW7grrKgrWqK
//
// Raydium CLMM is an Anchor program. Pool creation uses the CreatePool
// instruction. The discriminator is the first 8 bytes of
// SHA256("global:create_pool"):
// = [233, 146, 209, 142, 207, 104, 64, 188]
//
// NOTE: PumpFun AMM shares the same Anchor method name "create_pool" and thus
// the same discriminator. The programs are distinguished by their program IDs
// in instr.ProgramID — the dispatch switch in ingestion_solana.go uses the
// configured family to route to the correct normalizer.
//
// Account layout for CreatePool (0-based):
//
//	 0 = poolCreator             ← deployer
//	 1 = ammConfig
//	 2 = poolState               ← new pool state account
//	 3 = tokenMint0              ← first token mint
//	 4 = tokenMint1              ← second token mint
//	 5 = token0Vault
//	 6 = token1Vault
//	 7 = observationState
//	 8 = tickArrayBitmap
//	 9 = token0Program
//	10 = token1Program
//	11 = systemProgram
//	12 = rent
//
// Source: https://github.com/raydium-io/raydium-clmm

import (
	"bytes"
	"fmt"

	"crypto-sniping-bot/contracts"
)

// raydiumCLMMCreatePoolDiscriminator is the 8-byte Anchor selector for the
// CreatePool instruction of Raydium CLMM. SHA256("global:create_pool")[:8]
// (same value as PumpFun AMM — programs are distinguished by program ID, not
// the discriminator alone).
var raydiumCLMMCreatePoolDiscriminator = []byte{233, 146, 209, 142, 207, 104, 64, 188}

const (
	raydiumCLMMAccountPoolCreator = 0
	raydiumCLMMAccountPoolState   = 2
	raydiumCLMMAccountTokenMint0  = 3
	raydiumCLMMAccountTokenMint1  = 4
)

// RaydiumCLMMCreatePoolEvent holds the decoded fields from a CreatePool
// instruction on Raydium CLMM.
type RaydiumCLMMCreatePoolEvent struct {
	PoolState   string // pool state account
	TokenMint0  string // first token mint
	TokenMint1  string // second token mint
	PoolCreator string // deployer
}

// IsRaydiumCLMMCreatePool returns true when instr targets the Raydium CLMM
// program and its data begins with the CreatePool discriminator.
func IsRaydiumCLMMCreatePool(instr InstructionData, programID string) bool {
	return instr.ProgramID == programID &&
		bytes.HasPrefix(instr.Data, raydiumCLMMCreatePoolDiscriminator)
}

// DecodeRaydiumCLMMCreatePool extracts pool and token mints from a CreatePool
// instruction.
func DecodeRaydiumCLMMCreatePool(instr InstructionData) (*RaydiumCLMMCreatePoolEvent, error) {
	if !bytes.HasPrefix(instr.Data, raydiumCLMMCreatePoolDiscriminator) {
		return nil, nil
	}
	if len(instr.Accounts) <= raydiumCLMMAccountTokenMint1 {
		return nil, fmt.Errorf("raydium_clmm: create_pool: insufficient accounts: got %d", len(instr.Accounts))
	}
	return &RaydiumCLMMCreatePoolEvent{
		PoolState:   instr.Accounts[raydiumCLMMAccountPoolState],
		TokenMint0:  instr.Accounts[raydiumCLMMAccountTokenMint0],
		TokenMint1:  instr.Accounts[raydiumCLMMAccountTokenMint1],
		PoolCreator: instr.Accounts[raydiumCLMMAccountPoolCreator],
	}, nil
}

// NormalizeRaydiumCLMMCreatePool converts a CreatePool instruction into a
// MarketDataDTO. Returns nil when the instruction is not recognized.
func NormalizeRaydiumCLMMCreatePool(
	tx *TransactionResult,
	instr InstructionData,
	versionID string,
) (*contracts.MarketDataDTO, error) {
	event, err := DecodeRaydiumCLMMCreatePool(instr)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, nil
	}
	if event.TokenMint0 == "" {
		return nil, fmt.Errorf("raydium_clmm: create_pool: empty tokenMint0")
	}

	return &contracts.MarketDataDTO{
		EventID:           solanaEventID(tx.Signature, instr.Index),
		TraceID:           solanaEventID(tx.Signature, instr.Index),
		CorrelationID:     solanaEventID(tx.Signature, instr.Index),
		CausationID:       "",
		VersionID:         versionID,
		Chain:             "solana",
		Market:            "solana-raydium-clmm",
		BlockNumber:       tx.Slot,
		BlockHash:         tx.RecentBlockhash,
		TxHash:            tx.Signature,
		LogIndex:          uint32(instr.Index),
		EventTopic:        "RaydiumCLMMCreatePool",
		PoolAddress:       event.PoolState,
		TokenAddress:      event.TokenMint0,
		BaseAddress:       event.TokenMint1,
		Token0Address:     event.TokenMint0,
		Token1Address:     event.TokenMint1,
		Amount0Raw:        "0",
		Amount1Raw:        "0",
		ReserveBaseRaw:    "0",
		ReserveTokenRaw:   "0",
		CreatorAddress:    event.PoolCreator,
		BlockTimestamp:    blockTimestamp(tx.BlockTime),
		IngestedAt:        blockTimestamp(tx.BlockTime),
		Transport:         "ws",
		ConfirmationDepth: 0,
	}, nil
}
