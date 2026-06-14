package ingestion_solana

// raydium.go — Raydium AMM V4 instruction decoders.
//
// Program ID: 675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8
//
// Raydium V4 is NOT an Anchor program. Its on-chain instruction layout is a
// single-byte tag followed by the packed instruction body. This mirrors the
// upstream Rust implementation
// (raydium-amm/program/src/instruction.rs::AmmInstruction::unpack), which does
// `let (&tag, rest) = input.split_first()?;` and dispatches on `tag`.
//
// Recognized opcodes (subset relevant to Layer 0 ingestion):
//   tag 1  → Initialize2  (pool creation)
//   tag 9  → SwapBaseIn
//   tag 11 → SwapBaseOut
//
// Source: https://github.com/raydium-io/raydium-amm/blob/master/program/src/instruction.rs

import (
	"fmt"
	"strings"
)

// Raydium V4 instruction tag constants. See the file header for the program
// reference. Adding a new tag here MUST be paired with a normalization path
// in normalize.go and a heartbeat-counter classification in ingestion_solana.go.
const (
	RaydiumV4OpInitialize2 byte = 1
	RaydiumV4OpSwapBaseIn  byte = 9
	RaydiumV4OpSwapBaseOut byte = 11
)

// RaydiumV4InstructionKind classifies a raw Raydium V4 instruction payload by
// its leading tag byte. The worker uses this to distinguish "irrelevant
// notification" (Unknown) from "decoder produced no DTO for a recognized
// opcode" (decoder bug) — see ingestion_solana.go heartbeat counters.
type RaydiumV4InstructionKind int

const (
	// RaydiumV4KindUnknown means the leading tag is not one of the recognized
	// opcodes (or the data is empty). Notifications with this kind are counted
	// as skipped_unknown_instruction, NOT dto_nil_skip.
	RaydiumV4KindUnknown RaydiumV4InstructionKind = iota
	// RaydiumV4KindInitialize2 matches tag 1 (pool creation).
	RaydiumV4KindInitialize2
	// RaydiumV4KindSwapBaseIn matches tag 9.
	RaydiumV4KindSwapBaseIn
	// RaydiumV4KindSwapBaseOut matches tag 11.
	RaydiumV4KindSwapBaseOut
)

// RaydiumV4ProgramID is the on-chain Raydium AMM V4 program address.
const RaydiumV4ProgramID = "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8"

// LogsSuggestRaydiumPoolInit reports whether log lines indicate a Raydium V4
// Initialize2 (pool creation) transaction. Initialize2 does not emit ray_log:
// entries; swaps/deposits do. Used to trigger a getTransaction fallback when an
// embedded transactionSubscribe payload had no matching program instructions.
func LogsSuggestRaydiumPoolInit(logs []string) bool {
	if len(logs) == 0 {
		return false
	}
	for _, l := range logs {
		if strings.Contains(strings.ToLower(l), "initialize2") {
			return true
		}
	}
	hasRayLog := false
	invokesRaydium := false
	for _, l := range logs {
		if strings.Contains(l, "ray_log:") {
			hasRayLog = true
		}
		if strings.Contains(l, RaydiumV4ProgramID) {
			invokesRaydium = true
		}
	}
	return invokesRaydium && !hasRayLog
}

// ClassifyRaydiumV4Instruction returns the kind based solely on the leading
// tag byte. Deterministic: same input → same output. No allocation.
func ClassifyRaydiumV4Instruction(data []byte) RaydiumV4InstructionKind {
	if len(data) < 1 {
		return RaydiumV4KindUnknown
	}
	switch data[0] {
	case RaydiumV4OpInitialize2:
		return RaydiumV4KindInitialize2
	case RaydiumV4OpSwapBaseIn:
		return RaydiumV4KindSwapBaseIn
	case RaydiumV4OpSwapBaseOut:
		return RaydiumV4KindSwapBaseOut
	default:
		return RaydiumV4KindUnknown
	}
}

// RaydiumPoolInitEvent holds decoded fields from a Raydium V4 Initialize2 instruction.
//
// Initialize2 layout (after 1-byte tag):
//
//	nonce          : u8
//	openTime       : u64 little-endian
//	initPcAmount   : u64 little-endian
//	initCoinAmount : u64 little-endian
type RaydiumPoolInitEvent struct {
	Nonce          uint8
	OpenTime       uint64
	InitPcAmount   uint64
	InitCoinAmount uint64
}

// DecodeRaydiumPoolInit decodes a Raydium V4 Initialize2 instruction.
// Returns an error when the leading tag is not RaydiumV4OpInitialize2 or the
// body is truncated. Deterministic.
func DecodeRaydiumPoolInit(data []byte) (*RaydiumPoolInitEvent, error) {
	if len(data) < 1 || data[0] != RaydiumV4OpInitialize2 {
		return nil, fmt.Errorf("not a raydium initialize2 instruction")
	}
	r := NewReader(data[1:])

	nonce, err := r.ReadU8()
	if err != nil {
		return nil, fmt.Errorf("raydium pool init: read nonce: %w", err)
	}
	openTime, err := r.ReadU64()
	if err != nil {
		return nil, fmt.Errorf("raydium pool init: read open_time: %w", err)
	}
	initPc, err := r.ReadU64()
	if err != nil {
		return nil, fmt.Errorf("raydium pool init: read init_pc_amount: %w", err)
	}
	initCoin, err := r.ReadU64()
	if err != nil {
		return nil, fmt.Errorf("raydium pool init: read init_coin_amount: %w", err)
	}

	return &RaydiumPoolInitEvent{
		Nonce:          nonce,
		OpenTime:       openTime,
		InitPcAmount:   initPc,
		InitCoinAmount: initCoin,
	}, nil
}

// RaydiumSwapEvent holds decoded fields from a Raydium V4 swap instruction.
//
// SwapBaseIn / SwapBaseOut share the same body layout (after 1-byte tag):
//
//	amountIn         : u64 little-endian   (or amountOut for SwapBaseOut)
//	minimumAmountOut : u64 little-endian   (or maxAmountIn for SwapBaseOut)
//
// The semantics of the two u64s flip depending on the tag; this struct
// surfaces both as raw amounts and lets downstream layers interpret them.
type RaydiumSwapEvent struct {
	AmountIn         uint64
	MinimumAmountOut uint64
}

// DecodeRaydiumSwap decodes a Raydium V4 swap instruction (SwapBaseIn or
// SwapBaseOut). Returns an error when the leading tag is neither swap opcode
// or the body is truncated. Deterministic.
func DecodeRaydiumSwap(data []byte) (*RaydiumSwapEvent, error) {
	if len(data) < 1 || (data[0] != RaydiumV4OpSwapBaseIn && data[0] != RaydiumV4OpSwapBaseOut) {
		return nil, fmt.Errorf("not a raydium swap instruction")
	}
	r := NewReader(data[1:])

	amountIn, err := r.ReadU64()
	if err != nil {
		return nil, fmt.Errorf("raydium swap: read amount_in: %w", err)
	}
	minOut, err := r.ReadU64()
	if err != nil {
		return nil, fmt.Errorf("raydium swap: read minimum_amount_out: %w", err)
	}

	return &RaydiumSwapEvent{
		AmountIn:         amountIn,
		MinimumAmountOut: minOut,
	}, nil
}
