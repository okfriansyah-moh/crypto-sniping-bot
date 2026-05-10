package ingestion_solana

// meteora_dlmm.go — Meteora DLMM (Dynamic Liquidity Market Maker) pool decoder.
//
// Program ID: LBUZKhRxPF3XUpBCjp4YzTKgLLjLeNox4HgSehp9ZSe
//
// Meteora DLMM is an Anchor program. Pool creation uses the InitializeLbPair
// instruction. The discriminator is the first 8 bytes of
// SHA256("global:initialize_lb_pair"):
// = [110, 106, 20, 253, 63, 145, 232, 63]
//
// Account layout for InitializeLbPair (0-based):
//
//	0 = lbPair                  ← new pool state account
//	1 = binArrayBitmapExtension
//	2 = tokenMintX              ← first token mint
//	3 = tokenMintY              ← second token mint
//	4 = reserveX
//	5 = reserveY
//	6 = oracle
//	7 = presetParameter
//	8 = funder                  ← deployer / creator
//	9 = tokenProgram
//	10 = systemProgram
//	11 = rent
//	12 = eventAuthority
//	13 = program
//
// Source: https://github.com/meteora-ag/dlmm-sdk

import (
	"bytes"
	"fmt"

	"crypto-sniping-bot/contracts"
)

// meteoraDLMMInitLbPairDiscriminator is the 8-byte Anchor selector for
// InitializeLbPair. SHA256("global:initialize_lb_pair")[:8]
var meteoraDLMMInitLbPairDiscriminator = []byte{110, 106, 20, 253, 63, 145, 232, 63}

const (
	meteoraDLMMAccountLbPair     = 0
	meteoraDLMMAccountTokenMintX = 2
	meteoraDLMMAccountTokenMintY = 3
	meteoraDLMMAccountFunder     = 8
)

// MeteoraDLMMInitLbPairEvent holds the decoded fields from an
// InitializeLbPair instruction.
type MeteoraDLMMInitLbPairEvent struct {
	LbPair     string // pool state account
	TokenMintX string // first token mint
	TokenMintY string // second token mint
	Funder     string // deployer
}

// IsMeteoraDLMMInitLbPair returns true when instr targets Meteora DLMM and its
// data begins with the InitializeLbPair discriminator.
func IsMeteoraDLMMInitLbPair(instr InstructionData, programID string) bool {
	return instr.ProgramID == programID &&
		bytes.HasPrefix(instr.Data, meteoraDLMMInitLbPairDiscriminator)
}

// DecodeMeteoraDLMMInitLbPair extracts pool and token mints from an
// InitializeLbPair instruction.
func DecodeMeteoraDLMMInitLbPair(instr InstructionData) (*MeteoraDLMMInitLbPairEvent, error) {
	if !bytes.HasPrefix(instr.Data, meteoraDLMMInitLbPairDiscriminator) {
		return nil, nil
	}
	if len(instr.Accounts) <= meteoraDLMMAccountTokenMintY {
		return nil, fmt.Errorf("meteora_dlmm: init_lb_pair: insufficient accounts: got %d", len(instr.Accounts))
	}
	funder := ""
	if len(instr.Accounts) > meteoraDLMMAccountFunder {
		funder = instr.Accounts[meteoraDLMMAccountFunder]
	}
	return &MeteoraDLMMInitLbPairEvent{
		LbPair:     instr.Accounts[meteoraDLMMAccountLbPair],
		TokenMintX: instr.Accounts[meteoraDLMMAccountTokenMintX],
		TokenMintY: instr.Accounts[meteoraDLMMAccountTokenMintY],
		Funder:     funder,
	}, nil
}

// NormalizeMeteoraDLMMInitLbPair converts an InitializeLbPair instruction into
// a MarketDataDTO. Returns nil when the instruction is not recognized.
func NormalizeMeteoraDLMMInitLbPair(
	tx *TransactionResult,
	instr InstructionData,
	versionID string,
) (*contracts.MarketDataDTO, error) {
	event, err := DecodeMeteoraDLMMInitLbPair(instr)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, nil
	}
	if event.TokenMintX == "" {
		return nil, fmt.Errorf("meteora_dlmm: init_lb_pair: empty tokenMintX")
	}

	return &contracts.MarketDataDTO{
		EventID:           solanaEventID(tx.Signature, instr.Index),
		TraceID:           solanaEventID(tx.Signature, instr.Index),
		CorrelationID:     solanaEventID(tx.Signature, instr.Index),
		CausationID:       "",
		VersionID:         versionID,
		Chain:             "solana",
		Market:            "solana-meteora-dlmm",
		BlockNumber:       tx.Slot,
		BlockHash:         tx.RecentBlockhash,
		TxHash:            tx.Signature,
		LogIndex:          uint32(instr.Index),
		EventTopic:        "MeteoraDLMMInitLbPair",
		PoolAddress:       event.LbPair,
		TokenAddress:      event.TokenMintX,
		BaseAddress:       event.TokenMintY,
		Token0Address:     event.TokenMintX,
		Token1Address:     event.TokenMintY,
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
