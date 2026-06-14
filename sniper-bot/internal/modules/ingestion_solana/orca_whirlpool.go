package ingestion_solana

// orca_whirlpool.go — Orca Whirlpool concentrated-liquidity pool-creation decoder.
//
// Program ID: whirLbMiicVdio4qvUfM5KAg6Ct8VwpYzGff3uctyCc
//
// Orca Whirlpool is an Anchor program. Pool creation is triggered by the
// InitializePool instruction. The discriminator is the first 8 bytes of
// SHA256("global:initialize_pool"):
// = [95, 180, 10, 172, 84, 174, 232, 40]
//
// Account layout for InitializePool (0-based):
//
//	 0 = whirlpoolsConfig
//	 1 = tokenMintA                 ← first token in the pair
//	 2 = tokenMintB                 ← second token in the pair
//	 3 = funder
//	 4 = whirlpool                  ← new pool state account
//	 5 = tokenVaultA
//	 6 = tokenVaultB
//	 7 = feeTier
//	 8 = tokenProgram
//	 9 = systemProgram
//	10 = rent
//
// The InstructionData body (after discriminator) holds:
//   - tickSpacing  uint16 (le)  — fee-tier tick spacing
//   - sqrtPriceX64 uint128 (le) — initial sqrt price in Q64.64 fixed-point
//
// Source: https://github.com/orca-so/whirlpools

import (
	"bytes"
	"fmt"

	"crypto-sniping-bot/shared/contracts"
)

// orcaWhirlpoolInitPoolDiscriminator is the 8-byte Anchor selector for
// InitializePool. SHA256("global:initialize_pool")[:8]
var orcaWhirlpoolInitPoolDiscriminator = []byte{95, 180, 10, 172, 84, 174, 232, 40}

const (
	orcaWhirlpoolAccountTokenMintA = 1
	orcaWhirlpoolAccountTokenMintB = 2
	orcaWhirlpoolAccountFunder     = 3
	orcaWhirlpoolAccountPool       = 4
)

// OrcaWhirlpoolInitPoolEvent holds the decoded fields from an
// InitializePool instruction.
type OrcaWhirlpoolInitPoolEvent struct {
	Pool       string // whirlpool state account
	TokenMintA string // first token mint
	TokenMintB string // second token mint
	Funder     string // deployer / funder
}

// IsOrcaWhirlpoolInitPool returns true when instr targets the Orca Whirlpool
// program and its data begins with the InitializePool discriminator.
func IsOrcaWhirlpoolInitPool(instr InstructionData, programID string) bool {
	return instr.ProgramID == programID &&
		bytes.HasPrefix(instr.Data, orcaWhirlpoolInitPoolDiscriminator)
}

// DecodeOrcaWhirlpoolInitPool extracts pool and token mints from an
// InitializePool instruction.
func DecodeOrcaWhirlpoolInitPool(instr InstructionData) (*OrcaWhirlpoolInitPoolEvent, error) {
	if !bytes.HasPrefix(instr.Data, orcaWhirlpoolInitPoolDiscriminator) {
		return nil, nil
	}
	if len(instr.Accounts) <= orcaWhirlpoolAccountPool {
		return nil, fmt.Errorf("orca_whirlpool: init_pool: insufficient accounts: got %d", len(instr.Accounts))
	}
	funder := ""
	if len(instr.Accounts) > orcaWhirlpoolAccountFunder {
		funder = instr.Accounts[orcaWhirlpoolAccountFunder]
	}
	return &OrcaWhirlpoolInitPoolEvent{
		Pool:       instr.Accounts[orcaWhirlpoolAccountPool],
		TokenMintA: instr.Accounts[orcaWhirlpoolAccountTokenMintA],
		TokenMintB: instr.Accounts[orcaWhirlpoolAccountTokenMintB],
		Funder:     funder,
	}, nil
}

// NormalizeOrcaWhirlpoolInitPool converts an InitializePool instruction into a
// MarketDataDTO. Returns nil when the instruction is not recognized.
func NormalizeOrcaWhirlpoolInitPool(
	tx *TransactionResult,
	instr InstructionData,
	versionID string,
) (*contracts.MarketDataDTO, error) {
	event, err := DecodeOrcaWhirlpoolInitPool(instr)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, nil
	}
	if event.TokenMintA == "" {
		return nil, fmt.Errorf("orca_whirlpool: init_pool: empty tokenMintA")
	}

	return &contracts.MarketDataDTO{
		EventID:           solanaEventID(tx.Signature, instr.Index),
		TraceID:           solanaEventID(tx.Signature, instr.Index),
		CorrelationID:     solanaEventID(tx.Signature, instr.Index),
		CausationID:       "",
		VersionID:         versionID,
		Chain:             "solana",
		Market:            "solana-orca-whirlpool",
		BlockNumber:       tx.Slot,
		BlockHash:         tx.RecentBlockhash,
		TxHash:            tx.Signature,
		LogIndex:          uint32(instr.Index),
		EventTopic:        "OrcaWhirlpoolInitPool",
		PoolAddress:       event.Pool,
		TokenAddress:      event.TokenMintA,
		BaseAddress:       event.TokenMintB,
		Token0Address:     event.TokenMintA,
		Token1Address:     event.TokenMintB,
		Amount0Raw:        "0",
		Amount1Raw:        "0",
		ReserveBaseRaw:    "0",
		ReserveTokenRaw:   "0",
		CreatorAddress:    event.Funder,
		BlockTimestamp:    blockTimestamp(tx.BlockTime),
		IngestedAt:        blockTimestamp(tx.BlockTime),
		Transport:         "ws",
		ConfirmationDepth: 0,
	}, nil
}
