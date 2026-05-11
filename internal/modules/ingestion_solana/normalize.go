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
	"strings"
	"time"

	"crypto-sniping-bot/contracts"
)

// sanitizeMetadataString cleans an untrusted on-chain string (name, symbol) before
// it enters the DTO. It strips ASCII control characters (0x00–0x1F and 0x7F) to
// prevent log injection — e.g. tokens named `rm -rf ...` or containing newlines
// that break structured log parsers. Truncates to maxLen runes.
// Clean strings (normal token names) pass through unchanged.
func sanitizeMetadataString(s string, maxLen int) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		// Keep printable non-DEL characters; drop control chars and DEL.
		if r >= 0x20 && r != 0x7F {
			b.WriteRune(r)
		}
	}
	result := b.String()
	runes := []rune(result)
	if len(runes) > maxLen {
		return string(runes[:maxLen])
	}
	return result
}

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
		EventID:         solanaEventID(tx.Signature, instr.Index),
		TraceID:         solanaEventID(tx.Signature, instr.Index),
		CorrelationID:   solanaEventID(tx.Signature, instr.Index),
		CausationID:     "", // Layer 0 root
		VersionID:       versionID,
		Chain:           "solana",
		Market:          "solana-pumpfun",
		BlockNumber:     tx.Slot,
		BlockHash:       tx.RecentBlockhash,
		TxHash:          tx.Signature,
		LogIndex:        uint32(instr.Index),
		EventTopic:      "PumpFunCreate",
		PoolAddress:     bondingCurve,
		TokenAddress:    mint,
		BaseAddress:     wrappedSOL,
		Token0Address:   mint,
		Token1Address:   wrappedSOL,
		Amount0Raw:      "0",
		Amount1Raw:      "0",
		ReserveBaseRaw:  "0",
		ReserveTokenRaw: "0",
		// Phase 0: pump.fun pre-graduation has no transfer tax — genuine signal.
		// Wash/Holder/Lp left as zero-value (Known=false) until Phase 4 enrichment.
		TaxKnown:          true,
		BuyTaxBps:         0,
		SellTaxBps:        0,
		BlockTimestamp:    blockTimestamp(tx.BlockTime),
		IngestedAt:        blockTimestamp(tx.BlockTime),
		RpcEndpoint:       "",
		Transport:         "ws",
		ConfirmationDepth: 0,
		Reorged:           false,
		ExpiresAt:         "",
		Priority:          0,
		Symbol:            sanitizeMetadataString(event.Symbol, 32),
		Name:              sanitizeMetadataString(event.Name, 64),
	}
	return dto, nil
}

// NormalizeRaydiumPoolInit normalizes a Raydium V4 Initialize2 instruction.
// Returns (nil, nil) when the leading tag is NOT Initialize2 (silent skip,
// dispatcher routes elsewhere). Returns (nil, err) when the tag IS Initialize2
// but the body is truncated/malformed — the worker counts this under
// process_errors so the heartbeat surfaces decoder bugs instead of swallowing
// them.
func NormalizeRaydiumPoolInit(tx *TransactionResult, instr InstructionData, versionID string) (*contracts.MarketDataDTO, error) {
	if len(instr.Data) < 1 || instr.Data[0] != RaydiumV4OpInitialize2 {
		return nil, nil // wrong tag — not an Initialize2 instruction
	}
	event, err := DecodeRaydiumPoolInit(instr.Data)
	if err != nil {
		return nil, fmt.Errorf("raydium_pool_init_decode: %w", err)
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
	coinMint := instr.Accounts[8] // token being listed
	pcMint := instr.Accounts[9]   // quote token (SOL/USDC)

	// Determine base vs token ordering: base is the known stablecoin/SOL side.
	tokenAddr, baseAddr := coinMint, pcMint

	dto := &contracts.MarketDataDTO{
		EventID:           solanaEventID(tx.Signature, instr.Index),
		TraceID:           solanaEventID(tx.Signature, instr.Index),
		CorrelationID:     solanaEventID(tx.Signature, instr.Index),
		CausationID:       "",
		VersionID:         versionID,
		Chain:             "solana",
		Market:            "solana-raydium-v4",
		BlockNumber:       tx.Slot,
		BlockHash:         tx.RecentBlockhash,
		TxHash:            tx.Signature,
		LogIndex:          uint32(instr.Index),
		EventTopic:        "PoolCreated",
		PoolAddress:       ammPool,
		TokenAddress:      tokenAddr,
		BaseAddress:       baseAddr,
		Token0Address:     coinMint,
		Token1Address:     pcMint,
		Amount0Raw:        strconv.FormatUint(event.InitPcAmount, 10),
		Amount1Raw:        strconv.FormatUint(event.InitCoinAmount, 10),
		ReserveBaseRaw:    strconv.FormatUint(event.InitPcAmount, 10),
		ReserveTokenRaw:   strconv.FormatUint(event.InitCoinAmount, 10),
		BlockTimestamp:    blockTimestamp(tx.BlockTime),
		IngestedAt:        blockTimestamp(tx.BlockTime),
		RpcEndpoint:       "",
		Transport:         "ws",
		ConfirmationDepth: 0,
		Reorged:           false,
		ExpiresAt:         "",
		Priority:          0,
	}
	return dto, nil
}

// NormalizePumpFunCreateFromLogs builds a MarketDataDTO directly from a
// Pump.fun CreateEvent decoded from logsSubscribe log lines, bypassing the
// getTransaction RPC round-trip entirely.
//
// EventID determinism: this path uses the canonical instruction-index 0 in
// the EventID schema (`solana|<sig>|0`) — the same schema the slow
// tx-fetch path produces for top-level Pump.fun create instructions. This
// guarantees that toggling pumpfun_decode_from_logs at runtime, or having
// gap-recovery reprocess the same tx via getTransaction, produces an
// identical EventID and the consumer dedups correctly. Pump.fun creates
// are virtually always the top-level (index 0) instruction in the carrier
// tx; the rare CPI-nested case still resolves to a unique-per-tx ID, so
// downstream uniqueness is preserved.
//
// BlockTimestamp is left empty because logsSubscribe does not carry blockTime;
// downstream features that need it must derive it from `slot * 400ms` or
// fetch on demand. The slot itself is sufficient for ordering.
//
// virtualSolLamports / solPriceUsd inject the pump.fun protocol-defined
// virtual SOL reserve (30 SOL at BCP=0) so that Layer-2 feature extraction
// and the CPMM slippage model receive a non-zero liquidity estimate.
// Pass virtualSolLamports=0 to disable injection (reverts to "0" reserves).
func NormalizePumpFunCreateFromLogs(
	signature string,
	slot uint64,
	event *PumpFunLogCreateEvent,
	versionID string,
	ingestedAt string,
	virtualSolLamports uint64,
	solPriceUsd float64,
) *contracts.MarketDataDTO {
	const wrappedSOL = "So11111111111111111111111111111111111111112"
	eventID := solanaEventID(signature, 0)

	// Pump.fun virtual SOL reserve at creation (protocol constant: 30 SOL).
	// Injecting this as ReserveBaseRaw + LiquidityUsd lets the CPMM slippage
	// model and feature-extraction layer work from a real liquidity estimate
	// rather than cold-starting at zero.
	//
	// Phase 0 (safety net, log-reviewer 2026-05-02): we no longer claim
	// Wash/Holder stats are "known" at create time. Those flags previously
	// coupled a Layer-2 confidence-floor concern into Layer-1 risk semantics
	// and were the root cause of the constant risk_score=0.225 stub.
	// They are flipped to false until Phase 4 enrichment populates them
	// with real signal. Tax IS truly known for pump.fun pre-graduation
	// (no transfer tax exists), so TaxKnown=true with zero rates is genuine.
	reserveBaseRaw := "0"
	liquidityUsd := 0.0
	lpStatsKnown := false
	if virtualSolLamports > 0 && solPriceUsd > 0 {
		reserveBaseRaw = strconv.FormatUint(virtualSolLamports, 10)
		liquidityUsd = float64(virtualSolLamports) / 1e9 * solPriceUsd
		lpStatsKnown = true
	}

	return &contracts.MarketDataDTO{
		EventID:         eventID,
		TraceID:         eventID,
		CorrelationID:   eventID,
		CausationID:     "", // Layer 0 root event
		VersionID:       versionID,
		Chain:           "solana",
		Market:          "solana-pumpfun",
		BlockNumber:     slot,
		BlockHash:       "", // not available without getTransaction
		TxHash:          signature,
		LogIndex:        0,
		EventTopic:      "PumpFunCreate",
		PoolAddress:     event.BondingCurve,
		TokenAddress:    event.Mint,
		BaseAddress:     wrappedSOL,
		Token0Address:   event.Mint,
		Token1Address:   wrappedSOL,
		Amount0Raw:      "0",
		Amount1Raw:      "0",
		ReserveBaseRaw:  reserveBaseRaw,
		ReserveTokenRaw: "1000000000000000", // pump.fun: 1B tokens × 1e6 decimals = 1e15 raw
		LiquidityUsd:    liquidityUsd,
		LpStatsKnown:    lpStatsKnown,
		// Phase 0: untruthful claims removed. Real values land in Phase 4.
		WashStatsKnown:  false,
		TxCount1m:       0,
		UniqueWallets1m: 0,
		HolderDistKnown: false,
		HolderCount:     0,
		Top5HolderPct:   0,
		// Tax IS genuinely known for pump.fun pre-graduation: there is no
		// transfer tax on the bonding curve. Setting TaxKnown=true with
		// zero rates removes the dq_unknown_tax flag without lying.
		TaxKnown:   true,
		BuyTaxBps:  0,
		SellTaxBps: 0,
		// PoolAgeSeconds=1 (non-zero) marks the age as "known: brand-new"
		// so deriveTokenAgeConfidence returns 0.95 instead of 0.1.
		PoolAgeSeconds:    1,
		BlockTimestamp:    "", // unavailable in log-only path; see doc above
		IngestedAt:        ingestedAt,
		RpcEndpoint:       "",
		Transport:         "ws",
		ConfirmationDepth: 0,
		Reorged:           false,
		ExpiresAt:         "",
		Priority:          0,
		Symbol:            sanitizeMetadataString(event.Symbol, 32),
		Name:              sanitizeMetadataString(event.Name, 64),
		// CreatorAddress is the pump.fun `user` field — the wallet that
		// initiated the create transaction. Populated here at Layer 0 so
		// the solana_creator_reputation probe can query DB history without
		// making any extra RPC calls inside the DQ module.
		CreatorAddress: event.User,
		// MetadataURI is the on-chain URI from the CreateEvent (IPFS/Arweave).
		// Forwarded here so the solana_metadata probe can fetch it without
		// any additional RPC call.
		MetadataURI: event.URI,
	}
}

// NormalizeRaydiumV4Result is the tri-state outcome of normalizing a single
// Raydium V4 instruction. Exactly one of:
//
//	DTO != nil                                  → emit
//	DTO == nil && Kind == RaydiumV4KindUnknown  → skipped_unknown_instruction
//	DTO == nil && Kind != RaydiumV4KindUnknown  → dto_nil_skip (decoder bug or
//	                                              recognized opcode missing
//	                                              required accounts)
//	Err != nil                                  → process error (truncated data,
//	                                              malformed body, etc.)
type NormalizeRaydiumV4Result struct {
	DTO  *contracts.MarketDataDTO
	Kind RaydiumV4InstructionKind
	Err  error
}

// NormalizeRaydiumV4Instruction is the single dispatch entry for all Raydium V4
// instructions ingested by Layer 0. Classifies the leading tag, then routes to
// the per-instruction normalizer. Deterministic.
//
// Only Initialize2 (pool-creation) instructions produce a MarketDataDTO for
// the sniping pipeline. Swap instructions (tag 9 / 11) are counted as
// skipped_unknown_instruction so heartbeat math reconciles correctly — they
// do NOT emit a DTO with an empty TokenAddress that would flood the DQ worker
// with "empty token address" rejections and DLQ entries (F-1 fix).
func NormalizeRaydiumV4Instruction(tx *TransactionResult, instr InstructionData, versionID string) NormalizeRaydiumV4Result {
	kind := ClassifyRaydiumV4Instruction(instr.Data)
	switch kind {
	case RaydiumV4KindInitialize2:
		dto, err := NormalizeRaydiumPoolInit(tx, instr, versionID)
		return NormalizeRaydiumV4Result{DTO: dto, Kind: kind, Err: err}
	case RaydiumV4KindSwapBaseIn, RaydiumV4KindSwapBaseOut:
		// Swaps are not new-pool events — Layer 0 only discovers launches.
		// Return Kind so the caller's heartbeat counter distinguishes "we saw
		// a swap" from "we saw an unrecognized opcode", but emit no DTO.
		return NormalizeRaydiumV4Result{DTO: nil, Kind: kind, Err: nil}
	default:
		// Unrecognized opcode (SetParams, Withdraw, AdminCancelOrders, etc.).
		// Reported as skipped_unknown_instruction by the worker — NOT a decoder
		// bug. No DTO produced.
		return NormalizeRaydiumV4Result{DTO: nil, Kind: RaydiumV4KindUnknown, Err: nil}
	}
}

// NormalizeRaydiumSwap normalizes a Raydium V4 swap instruction.
// Returns (nil, nil) when the data is not a swap instruction or the account
// layout cannot be resolved. Returns (nil, err) only on malformed body bytes
// for a swap-tagged instruction.
func NormalizeRaydiumSwap(tx *TransactionResult, instr InstructionData, versionID string) (*contracts.MarketDataDTO, error) {
	// Distinguish "wrong tag" (skip silently) from "tagged as swap but body is
	// truncated/malformed" (surface as error so the worker can log/count it).
	if len(instr.Data) < 1 ||
		(instr.Data[0] != RaydiumV4OpSwapBaseIn && instr.Data[0] != RaydiumV4OpSwapBaseOut) {
		return nil, nil
	}
	event, err := DecodeRaydiumSwap(instr.Data)
	if err != nil {
		return nil, fmt.Errorf("raydium_swap_decode: %w", err)
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
		EventID:           solanaEventID(tx.Signature, instr.Index),
		TraceID:           solanaEventID(tx.Signature, instr.Index),
		CorrelationID:     solanaEventID(tx.Signature, instr.Index),
		CausationID:       "",
		VersionID:         versionID,
		Chain:             "solana",
		Market:            "solana-raydium-v4",
		BlockNumber:       tx.Slot,
		BlockHash:         tx.RecentBlockhash,
		TxHash:            tx.Signature,
		LogIndex:          uint32(instr.Index),
		EventTopic:        "Swap",
		PoolAddress:       ammPool,
		TokenAddress:      "",
		BaseAddress:       "",
		Token0Address:     "",
		Token1Address:     "",
		Amount0Raw:        strconv.FormatUint(event.AmountIn, 10),
		Amount1Raw:        strconv.FormatUint(event.MinimumAmountOut, 10),
		ReserveBaseRaw:    "0",
		ReserveTokenRaw:   "0",
		BlockTimestamp:    blockTimestamp(tx.BlockTime),
		IngestedAt:        blockTimestamp(tx.BlockTime),
		RpcEndpoint:       "",
		Transport:         "ws",
		ConfirmationDepth: 0,
	}
	return dto, nil
}
