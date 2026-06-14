package execution_solana

// compute_unit.go — compute unit and priority fee helpers.
//
// Solana transactions use compute units (CUs) instead of EVM gas.
// A SetComputeUnitLimit instruction caps the CUs consumed.
// A SetComputeUnitPrice instruction sets the priority fee in micro-lamports per CU.

// ComputeBudgetProgramID is the Solana Compute Budget program.
const ComputeBudgetProgramID = "ComputeBudget111111111111111111111111111111"

// SetComputeUnitLimitInstruction returns an instruction to set the CU limit.
// Instruction layout: [discriminator=0x02][u32 units]
func SetComputeUnitLimitInstruction(units uint32) *RawInstruction {
	var data []byte
	data = append(data, 0x02) // SetComputeUnitLimit discriminator
	data = appendU32LE(data, units)
	return &RawInstruction{
		ProgramID: ComputeBudgetProgramID,
		Accounts:  nil,
		Data:      data,
	}
}

// SetComputeUnitPriceInstruction returns an instruction to set the priority fee.
// Instruction layout: [discriminator=0x03][u64 microLamports]
func SetComputeUnitPriceInstruction(microLamports uint64) *RawInstruction {
	var data []byte
	data = append(data, 0x03) // SetComputeUnitPrice discriminator
	data = appendU64LE(data, microLamports)
	return &RawInstruction{
		ProgramID: ComputeBudgetProgramID,
		Accounts:  nil,
		Data:      data,
	}
}

// DefaultComputeUnits returns a safe compute unit budget for a swap transaction.
// Uses the buffer from config; falls back to 200_000 if not set.
func DefaultComputeUnits(buffer int) uint32 {
	if buffer <= 0 {
		return 200_000
	}
	return uint32(200_000 + buffer)
}

// appendU32LE appends a uint32 in little-endian byte order.
func appendU32LE(buf []byte, v uint32) []byte {
	return append(buf, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}
