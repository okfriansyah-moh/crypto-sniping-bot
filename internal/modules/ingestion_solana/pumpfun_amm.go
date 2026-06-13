package ingestion_solana

// pumpfun_amm.go — PumpFun AMM (graduation) pool-creation decoder.
//
// Program ID: pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA
//
// PumpFun AMM is an Anchor program. When a bonding-curve token "graduates"
// (reaches full progress), PumpFun migrates liquidity into its own AMM and
// emits a CreatePool instruction. This is the pool-creation event the ingestion
// layer captures — it signals that the token has left the bonding curve and
// entered a real AMM with discoverable liquidity.
//
// Anchor instruction discriminator: first 8 bytes of SHA256("global:create_pool")
// = [233, 146, 209, 142, 207, 104, 64, 188] — derived from the upstream Rust IDL.
//
// Account layout for CreatePool (0-based):
//
//	 0 = pool                    (new pool state account)
//	 1 = globalConfig
//	 2 = creator
//	 3 = baseMint               ← the graduated token
//	 4 = quoteMint              ← SOL or USDC
//	 5 = lpMint
//	 6 = userBaseTokenAccount
//	 7 = userQuoteTokenAccount
//	 8 = userPoolTokenAccount
//	 9 = baseTokenVault
//	10 = quoteTokenVault
//	11 = ...system / token programs
//
// Source: https://github.com/pump-fun-amm/docs (public IDL)

import (
	"bytes"
	"fmt"

	"crypto-sniping-bot/contracts"
)

// pumpfunAMMCreatePoolDiscriminator is the 8-byte Anchor instruction selector
// for the CreatePool instruction of the PumpFun AMM program.
// SHA256("global:create_pool")[:8] = [233, 146, 209, 142, 207, 104, 64, 188]
var pumpfunAMMCreatePoolDiscriminator = []byte{233, 146, 209, 142, 207, 104, 64, 188}

// pumpfunAMMAccountBaseMint is the 0-based account index for the graduated
// token mint in the CreatePool instruction.
const (
	pumpfunAMMAccountPool      = 0 // pool state (new)
	pumpfunAMMAccountCreator   = 2
	pumpfunAMMAccountBaseMint  = 3
	pumpfunAMMAccountQuoteMint = 4
)

// IsPumpFunAMMCreatePool returns true when instr belongs to the PumpFun AMM
// program and its data begins with the CreatePool discriminator.
// Returns false for all other instructions (swaps, migrations, etc.).
// Deterministic: same input → same output, no allocation.
func IsPumpFunAMMCreatePool(instr InstructionData, programID string) bool {
	if instr.ProgramID != programID {
		return false
	}
	return bytes.HasPrefix(instr.Data, pumpfunAMMCreatePoolDiscriminator)
}

// DecodePumpFunAMMCreatePool extracts the pool and token mints from a
// CreatePool instruction. Returns an error only for structural data issues
// (account indices out of range); unknown/unrelated instructions return nil.
func DecodePumpFunAMMCreatePool(instr InstructionData) (*PumpFunAMMCreatePoolEvent, error) {
	if !bytes.HasPrefix(instr.Data, pumpfunAMMCreatePoolDiscriminator) {
		return nil, nil // not a CreatePool
	}
	if len(instr.Accounts) <= pumpfunAMMAccountBaseMint {
		return nil, fmt.Errorf("pumpfun_amm: create_pool: insufficient accounts: got %d", len(instr.Accounts))
	}

	creator := ""
	if len(instr.Accounts) > pumpfunAMMAccountCreator {
		creator = instr.Accounts[pumpfunAMMAccountCreator]
	}
	quoteMint := ""
	if len(instr.Accounts) > pumpfunAMMAccountQuoteMint {
		quoteMint = instr.Accounts[pumpfunAMMAccountQuoteMint]
	}

	return &PumpFunAMMCreatePoolEvent{
		Pool:      instr.Accounts[pumpfunAMMAccountPool],
		Creator:   creator,
		BaseMint:  instr.Accounts[pumpfunAMMAccountBaseMint],
		QuoteMint: quoteMint,
	}, nil
}

// PumpFunAMMCreatePoolEvent holds the decoded fields from a PumpFun AMM
// CreatePool instruction.
type PumpFunAMMCreatePoolEvent struct {
	Pool      string // new pool state account
	Creator   string // creator / payer
	BaseMint  string // graduated token mint
	QuoteMint string // paired token (usually SOL native mint)
}

// NormalizePumpFunAMMCreatePool converts a CreatePool instruction into a
// MarketDataDTO. Returns nil when the instruction is not a CreatePool event.
// Returns an error only on structural data failure.
func NormalizePumpFunAMMCreatePool(
	tx *TransactionResult,
	instr InstructionData,
	versionID string,
) (*contracts.MarketDataDTO, error) {
	event, err := DecodePumpFunAMMCreatePool(instr)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, nil // not a CreatePool
	}
	tokenMint, baseAddr, ok := ResolveTradableMint(event.BaseMint, event.QuoteMint)
	if !ok {
		return nil, nil // system-mint pair or empty mint — skip emit (dto_nil_skip)
	}

	return &contracts.MarketDataDTO{
		EventID:           solanaEventID(tx.Signature, instr.Index),
		TraceID:           solanaEventID(tx.Signature, instr.Index),
		CorrelationID:     solanaEventID(tx.Signature, instr.Index),
		CausationID:       "",
		VersionID:         versionID,
		Chain:             "solana",
		Market:            "solana-pumpfun-amm",
		BlockNumber:       tx.Slot,
		BlockHash:         tx.RecentBlockhash,
		TxHash:            tx.Signature,
		LogIndex:          uint32(instr.Index),
		EventTopic:        "PumpFunAMMCreatePool",
		PoolAddress:       event.Pool,
		TokenAddress:      tokenMint,
		BaseAddress:       baseAddr,
		Token0Address:     event.BaseMint,
		Token1Address:     event.QuoteMint,
		Amount0Raw:        "0",
		Amount1Raw:        "0",
		ReserveBaseRaw:    "0",
		ReserveTokenRaw:   "0",
		CreatorAddress:    event.Creator,
		BlockTimestamp:    blockTimestamp(tx.BlockTime),
		IngestedAt:        blockTimestamp(tx.BlockTime),
		Transport:         "ws",
		ConfirmationDepth: 0,
	}, nil
}
