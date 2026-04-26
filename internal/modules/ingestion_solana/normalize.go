package ingestion_solana

// normalize.go — normalization functions for Solana program events.
// Each function maps a decoded instruction to a deterministic MarketDataDTO.
//
// Field mapping (all EVM-independent):
//   Chain             = "solana"
//   Market            = "solana-raydium-v4" | "solana-pumpfun"
//   BlockNumber       = slot number (uint64)
//   TxHash            = transaction signature (base58)
//   LogIndex          = instruction index within transaction
//   EventID           = ContentIDFromString("solana|" + signature + "|" + strconv.Itoa(instrIndex))
//   CausationID       = "" (Layer 0 root event)
//   BlockTimestamp    = Unix timestamp from block_time (RFC3339)
//   EventTopic        = "PoolCreated" | "PumpFunCreate"
//   Transport         = "ws"
//   ConfirmationDepth = 0 (unconfirmed at emit time; confirmed by commitment level)

import (
	"fmt"
	"strconv"
	"time"

	"crypto-sniping-bot/contracts"
)

// solanaEventID derives the content-addressable EventID for a Solana instruction.
// EventID = SHA256("solana|" + signature + "|" + instrIndex)[:16]
func solanaEventID(signature string, instrIndex int) string {
	return contracts.ContentIDFromString("solana|" + signature + "|" + strconv.Itoa(instrIndex))
}

// blockTimestamp converts a Unix timestamp (from blockTime) to RFC3339.
// Returns a zero-value string if blockTime is 0.
func blockTimestamp(unixSec int64) string {
	if unixSec == 0 {
		return ""
	}
	return time.Unix(unixSec, 0).UTC().Format(time.RFC3339)
}

// NormalizePumpFunCreate normalizes a Pump.fun token creation instruction.
// Returns nil (no error) if the instruction is not a recognized create event.
// Returns an error only on malformed data.
func NormalizePumpFunCreate(tx *TransactionResult, instr InstructionData, versionID string) (*contracts.MarketDataDTO, error) {
	event, err := DecodePumpFunCreate(instr.Data)
	if err != nil {
		return nil, nil // skip: not a create instruction
	}

	// Resolve mint and bonding curve from instruction accounts by index.
	// Pump.fun Create account layout (0-based):
	//   0 = mint
	//   1 = mintAuthority
	//   2 = bondingCurve
	//   3 = associatedBondingCurve
	//   4 = global
	//   5 = mplTokenMetadata
	//   6 = metadata
	//   7 = user (creator/payer)
	if len(instr.Accounts) < 4 {
		return nil, fmt.Errorf("pump_fun_create: insufficient accounts: %d", len(instr.Accounts))
	}
	mint := instr.Accounts[0]
	bondingCurve := instr.Accounts[2]

	// For Pump.fun, the "pool" is the bonding curve account.
	// Base token is SOL (native), represented as wrapped SOL address.
	const wrappedSOL = "So11111111111111111111111111111111111111112"

	dto := &contracts.MarketDataDTO{
		EventID:          solanaEventID(tx.Signature, instr.Index),
		TraceID:          solanaEventID(tx.Signature, instr.Index),
		CorrelationID:    solanaEventID(tx.Signature, instr.Index),
		CausationID:      "", // Layer 0 root
		VersionID:        versionID,
		Chain:            "solana",
		Market:           "solana-pumpfun",
		BlockNumber:      tx.Slot,
		BlockHash:        tx.RecentBlockhash,
		TxHash:           tx.Signature,
		LogIndex:         uint32(instr.Index),
		EventTopic:       "PumpFunCreate",
		PoolAddress:      bondingCurve,
		TokenAddress:     mint,
		BaseAddress:      wrappedSOL,
		Token0Address:    mint,
		Token1Address:    wrappedSOL,
		Amount0Raw:       "0",
		Amount1Raw:       "0",
		ReserveBaseRaw:   "0",
		ReserveTokenRaw:  "0",
		BlockTimestamp:   blockTimestamp(tx.BlockTime),
		IngestedAt:       time.Now().UTC().Format(time.RFC3339Nano),
		RpcEndpoint:      "",
		Transport:        "ws",
		ConfirmationDepth: 0,
		Reorged:          false,
		ExpiresAt:        "",
		Priority:         0,
		// Pump.fun specific metadata stored in reserved fields
		// (name and symbol are available in event but MarketDataDTO has no text fields)
	}
	_ = event // event.Name, event.Symbol available if needed for future enrichment
	return dto, nil
}

// NormalizeRaydiumPoolInit normalizes a Raydium V4 pool initialization instruction.
// Returns nil if the instruction is not a pool init event.
func NormalizeRaydiumPoolInit(tx *TransactionResult, instr InstructionData, versionID string) (*contracts.MarketDataDTO, error) {
	event, err := DecodeRaydiumPoolInit(instr.Data)
	if err != nil {
		return nil, nil // skip: not a pool init instruction
	}

	// Raydium V4 Initialize2 account layout (0-based):
	//   0 = tokenProgram
	//   1 = splAssociatedTokenAccount
	//   2 = systemProgram
	//   3 = rent
	//   4 = amm (pool)
	//   5 = ammAuthority
	//   6 = ammOpenOrders
	//   7 = lpMintAddress
	//   8 = coinMintAddress  (token0)
	//   9 = pcMintAddress    (token1)
	//   ...
	if len(instr.Accounts) < 10 {
		return nil, fmt.Errorf("raydium_pool_init: insufficient accounts: %d", len(instr.Accounts))
	}
	ammPool := instr.Accounts[4]
	coinMint := instr.Accounts[8]  // token being listed
	pcMint := instr.Accounts[9]    // quote token (SOL/USDC)

	// Determine base vs token ordering: base is the known stablecoin/SOL side.
	tokenAddr, baseAddr := coinMint, pcMint

	dto := &contracts.MarketDataDTO{
		EventID:          solanaEventID(tx.Signature, instr.Index),
		TraceID:          solanaEventID(tx.Signature, instr.Index),
		CorrelationID:    solanaEventID(tx.Signature, instr.Index),
		CausationID:      "",
		VersionID:        versionID,
		Chain:            "solana",
		Market:           "solana-raydium-v4",
		BlockNumber:      tx.Slot,
		BlockHash:        tx.RecentBlockhash,
		TxHash:           tx.Signature,
		LogIndex:         uint32(instr.Index),
		EventTopic:       "PoolCreated",
		PoolAddress:      ammPool,
		TokenAddress:     tokenAddr,
		BaseAddress:      baseAddr,
		Token0Address:    coinMint,
		Token1Address:    pcMint,
		Amount0Raw:       strconv.FormatUint(event.InitPcAmount, 10),
		Amount1Raw:       strconv.FormatUint(event.InitCoinAmount, 10),
		ReserveBaseRaw:   strconv.FormatUint(event.InitPcAmount, 10),
		ReserveTokenRaw:  strconv.FormatUint(event.InitCoinAmount, 10),
		BlockTimestamp:   blockTimestamp(tx.BlockTime),
		IngestedAt:       time.Now().UTC().Format(time.RFC3339Nano),
		RpcEndpoint:      "",
		Transport:        "ws",
		ConfirmationDepth: 0,
		Reorged:          false,
		ExpiresAt:        "",
		Priority:         0,
	}
	return dto, nil
}

// NormalizeRaydiumSwap normalizes a Raydium V4 swap instruction.
// Returns nil if not a swap instruction.
func NormalizeRaydiumSwap(tx *TransactionResult, instr InstructionData, versionID string) (*contracts.MarketDataDTO, error) {
	event, err := DecodeRaydiumSwap(instr.Data)
	if err != nil {
		return nil, nil // not a swap instruction
	}

	// Raydium swap account layout simplified:
	//   4 = amm pool
	//   8 = coinVault
	//   9 = pcVault
	if len(instr.Accounts) < 10 {
		return nil, nil
	}
	ammPool := instr.Accounts[4]

	dto := &contracts.MarketDataDTO{
		EventID:          solanaEventID(tx.Signature, instr.Index),
		TraceID:          solanaEventID(tx.Signature, instr.Index),
		CorrelationID:    solanaEventID(tx.Signature, instr.Index),
		CausationID:      "",
		VersionID:        versionID,
		Chain:            "solana",
		Market:           "solana-raydium-v4",
		BlockNumber:      tx.Slot,
		BlockHash:        tx.RecentBlockhash,
		TxHash:           tx.Signature,
		LogIndex:         uint32(instr.Index),
		EventTopic:       "Swap",
		PoolAddress:      ammPool,
		TokenAddress:     "",
		BaseAddress:      "",
		Token0Address:    "",
		Token1Address:    "",
		Amount0Raw:       strconv.FormatUint(event.AmountIn, 10),
		Amount1Raw:       strconv.FormatUint(event.MinimumAmountOut, 10),
		ReserveBaseRaw:   "0",
		ReserveTokenRaw:  "0",
		BlockTimestamp:   blockTimestamp(tx.BlockTime),
		IngestedAt:       time.Now().UTC().Format(time.RFC3339Nano),
		RpcEndpoint:      "",
		Transport:        "ws",
		ConfirmationDepth: 0,
	}
	return dto, nil
}
