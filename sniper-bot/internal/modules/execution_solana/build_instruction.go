package execution_solana

// build_instruction.go — swap instruction builders for Raydium V4 and Pump.fun.
//
// Each builder returns a RawInstruction ready to be placed in a Solana transaction.
// Instruction data is encoded using Borsh-compatible little-endian binary layout.

import (
	"encoding/binary"
	"fmt"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// RawInstruction is a single Solana instruction ready for transaction inclusion.
type RawInstruction struct {
	ProgramID string
	Accounts  []AccountMeta
	Data      []byte
}

// AccountMeta is a Solana account reference within an instruction.
type AccountMeta struct {
	PublicKey  string
	IsSigner   bool
	IsWritable bool
}

// BuildSwapInstruction constructs the appropriate swap instruction based on the
// market argument ("solana-raydium-v4" | "solana-pumpfun").
// poolAddress is the AMM pool or bonding curve address; may be empty for Pump.fun
// (the token mint is sufficient for instruction construction in Phase 7).
func BuildSwapInstruction(alloc contracts.AllocationDTO, market, poolAddress string, cfg *config.SolanaExecutionConfig) (*RawInstruction, error) {
	switch market {
	case "solana-raydium-v4":
		return buildRaydiumSwapInstruction(alloc, poolAddress, cfg)
	case "solana-pumpfun":
		return buildPumpFunBuyInstruction(alloc, poolAddress, cfg)
	default:
		return nil, fmt.Errorf("build_instruction: unsupported market: %s", market)
	}
}

// buildRaydiumSwapInstruction builds a Raydium V4 SwapBaseIn instruction.
// Opcode 9 (SwapBaseIn):
//
//	[u64 opcode=9][u64 amountIn][u64 minimumAmountOut]
func buildRaydiumSwapInstruction(alloc contracts.AllocationDTO, poolAddress string, cfg *config.SolanaExecutionConfig) (*RawInstruction, error) {
	// Apply slippage cap from config.
	slippageBps := alloc.MaxSlippageBps
	if slippageBps <= 0 || slippageBps > cfg.SlippageCapBps {
		slippageBps = cfg.SlippageCapBps
	}

	amountIn := uint64(alloc.SizeUsd * 1e9) // Convert USD to lamports approximation
	// minimumAmountOut = amountIn * (1 - slippage/10000)
	minOut := amountIn * uint64(10000-slippageBps) / 10000

	var data []byte
	data = appendU64LE(data, 9) // SwapBaseIn opcode
	data = appendU64LE(data, amountIn)
	data = appendU64LE(data, minOut)

	// Minimal account list — real deployment would use pool state accounts.
	accounts := []AccountMeta{
		{PublicKey: poolAddress, IsSigner: false, IsWritable: true},
		{PublicKey: alloc.TokenAddress, IsSigner: false, IsWritable: true},
	}

	return &RawInstruction{
		ProgramID: "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8",
		Accounts:  accounts,
		Data:      data,
	}, nil
}

// buildPumpFunBuyInstruction builds a Pump.fun buy instruction.
// Discriminator [102,6,61,18,1,218,235,234] (sha256("global:buy")[:8])
// Layout: [8-byte disc][u64 amount][u64 maxSolCost]
func buildPumpFunBuyInstruction(alloc contracts.AllocationDTO, poolAddress string, cfg *config.SolanaExecutionConfig) (*RawInstruction, error) {
	disc := [8]byte{102, 6, 61, 18, 1, 218, 235, 234}

	slippageBps := alloc.MaxSlippageBps
	if slippageBps <= 0 || slippageBps > cfg.SlippageCapBps {
		slippageBps = cfg.SlippageCapBps
	}

	amount := uint64(alloc.SizeUsd * 1e6)
	maxSolCost := amount * uint64(10000+slippageBps) / 10000

	var data []byte
	data = append(data, disc[:]...)
	data = appendU64LE(data, amount)
	data = appendU64LE(data, maxSolCost)

	accounts := []AccountMeta{
		{PublicKey: alloc.TokenAddress, IsSigner: false, IsWritable: true},
		{PublicKey: poolAddress, IsSigner: false, IsWritable: true},
	}

	return &RawInstruction{
		ProgramID: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P",
		Accounts:  accounts,
		Data:      data,
	}, nil
}

// appendU64LE appends a uint64 in little-endian byte order to buf.
func appendU64LE(buf []byte, v uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, v)
	return append(buf, b...)
}
