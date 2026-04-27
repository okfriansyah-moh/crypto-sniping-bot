package ingestion_solana

// raydium.go — Raydium AMM V4 instruction decoders.
//
// Program ID: 675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8
//
// Initialize2 discriminator: [1, 0, 0, 0, 0, 0, 0, 0] (opcode = 1)
// SwapBaseIn discriminator:   [9, 0, 0, 0, 0, 0, 0, 0] (opcode = 9)
// SwapBaseOut discriminator:  [11, 0, 0, 0, 0, 0, 0, 0] (opcode = 11)
//
// Raydium V4 uses a simple 1-byte opcode (not Anchor discriminator),
// stored as a little-endian u64 in the first 8 bytes of instruction data.

import "fmt"

// RaydiumPoolInitDiscriminator corresponds to Initialize2 (opcode 1).
var RaydiumPoolInitDiscriminator = [8]byte{1, 0, 0, 0, 0, 0, 0, 0}

// RaydiumSwapBaseInDiscriminator corresponds to SwapBaseIn (opcode 9).
var RaydiumSwapBaseInDiscriminator = [8]byte{9, 0, 0, 0, 0, 0, 0, 0}

// RaydiumSwapBaseOutDiscriminator corresponds to SwapBaseOut (opcode 11).
var RaydiumSwapBaseOutDiscriminator = [8]byte{11, 0, 0, 0, 0, 0, 0, 0}

// RaydiumPoolInitEvent holds decoded fields from a Raydium V4 Initialize2 instruction.
// Initialize2 layout (after 8-byte opcode):
//   nonce          : u8
//   openTime       : u64
//   initPcAmount   : u64
//   initCoinAmount : u64
type RaydiumPoolInitEvent struct {
	Nonce          uint8
	OpenTime       uint64
	InitPcAmount   uint64
	InitCoinAmount uint64
}

// DecodeRaydiumPoolInit decodes a Raydium V4 pool initialization instruction.
func DecodeRaydiumPoolInit(data []byte) (*RaydiumPoolInitEvent, error) {
	if !MatchDiscriminator(data, RaydiumPoolInitDiscriminator) {
		return nil, fmt.Errorf("not a raydium initialize2 instruction")
	}
	r := NewReader(data[8:])

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
// SwapBaseIn layout (after 8-byte opcode):
//   amountIn       : u64
//   minimumAmountOut : u64
type RaydiumSwapEvent struct {
	AmountIn         uint64
	MinimumAmountOut uint64
}

// DecodeRaydiumSwap decodes a Raydium V4 swap instruction (SwapBaseIn or SwapBaseOut).
func DecodeRaydiumSwap(data []byte) (*RaydiumSwapEvent, error) {
	if !MatchDiscriminator(data, RaydiumSwapBaseInDiscriminator) &&
		!MatchDiscriminator(data, RaydiumSwapBaseOutDiscriminator) {
		return nil, fmt.Errorf("not a raydium swap instruction")
	}
	r := NewReader(data[8:])

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
