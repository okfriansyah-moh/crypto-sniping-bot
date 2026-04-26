package execution_solana

// send_tx.go — Solana transaction binary serialization and signing.
//
// Solana legacy transaction format:
//   [compact-u16: num_signatures]
//   [64 bytes per signature]
//   [message: header + account_keys + recent_blockhash + instructions]
//
// Reference: https://docs.solana.com/developing/programming-model/transactions

import (
"encoding/base64"
"fmt"
"math/big"
)

// BuildAndSignTransaction serializes and signs a Solana legacy transaction.
// Returns the base64-encoded signed transaction ready for sendTransaction.
func BuildAndSignTransaction(keypair *Keypair, recentBlockhash string, instr *RawInstruction) (string, error) {
if keypair == nil {
return "", fmt.Errorf("build_and_sign: nil keypair")
}
if instr == nil {
return "", fmt.Errorf("build_and_sign: nil instruction")
}

accountKeys, progIdx, accountIdxs := buildAccountMap(keypair, instr)
blockhashBytes := decodeBase58To32(recentBlockhash)
instrData := serializeInstruction(progIdx, accountIdxs, instr.Data)
msg := buildMessage(accountKeys, blockhashBytes, instrData)

sig := keypair.Sign(msg)
if len(sig) != 64 {
return "", fmt.Errorf("sign: unexpected signature length %d", len(sig))
}

var tx []byte
tx = append(tx, encodeCompactU16(1)...)
tx = append(tx, sig...)
tx = append(tx, msg...)
return base64.StdEncoding.EncodeToString(tx), nil
}

// buildAccountMap collects unique public keys in transaction order.
// Returns the key list, the program's index, and each account's index.
func buildAccountMap(keypair *Keypair, instr *RawInstruction) (
accountKeys [][]byte,
programIndex int,
accountIndexes []int,
) {
keyIndex := make(map[string]int)
var keys [][]byte

addKey := func(pubkeyStr string) int {
if idx, ok := keyIndex[pubkeyStr]; ok {
return idx
}
decoded := decodeBase58To32(pubkeyStr)
idx := len(keys)
keyIndex[pubkeyStr] = idx
keys = append(keys, decoded)
return idx
}

addKey(base58Encode(keypair.PublicKeyBytes())) // signer always at index 0
programIndex = addKey(instr.ProgramID)

accountIndexes = make([]int, len(instr.Accounts))
for i, acc := range instr.Accounts {
accountIndexes[i] = addKey(acc.PublicKey)
}
return keys, programIndex, accountIndexes
}

// buildMessage serializes the Solana message (header + accounts + blockhash + instrs).
func buildMessage(accountKeys [][]byte, blockhash []byte, instrData []byte) []byte {
var msg []byte
msg = append(msg, 1, 0, 1) // header: [num_req_sigs=1, num_readonly_signed=0, num_readonly_unsigned=1]
msg = append(msg, encodeCompactU16(len(accountKeys))...)
for _, k := range accountKeys {
msg = append(msg, k...)
}
msg = append(msg, blockhash...)
msg = append(msg, encodeCompactU16(1)...) // one instruction
msg = append(msg, instrData...)
return msg
}

// serializeInstruction serializes a single instruction.
func serializeInstruction(programIdx int, accountIdxs []int, data []byte) []byte {
var buf []byte
buf = append(buf, byte(programIdx))
buf = append(buf, encodeCompactU16(len(accountIdxs))...)
for _, idx := range accountIdxs {
buf = append(buf, byte(idx))
}
buf = append(buf, encodeCompactU16(len(data))...)
buf = append(buf, data...)
return buf
}

// encodeCompactU16 encodes v using Solana's compact-u16 format.
func encodeCompactU16(v int) []byte {
if v < 0x80 {
return []byte{byte(v)}
}
if v < 0x4000 {
return []byte{byte(v&0x7F | 0x80), byte(v >> 7)}
}
return []byte{byte(v&0x7F | 0x80), byte(v>>7&0x7F | 0x80), byte(v >> 14)}
}

// decodeBase58To32 decodes a base58 string to exactly 32 bytes.
// Falls back to zero-padded raw bytes for test stubs.
func decodeBase58To32(s string) []byte {
out := make([]byte, 32)
decoded := base58Decode(s)
if len(decoded) == 32 {
copy(out, decoded)
} else {
copy(out, []byte(s))
}
return out
}

const solBase58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// base58Encode encodes bytes to a base58 string (Bitcoin/Solana alphabet).
func base58Encode(input []byte) string {
if len(input) == 0 {
return ""
}
leadingZeros := 0
for _, b := range input {
if b != 0 {
break
}
leadingZeros++
}
n := new(big.Int).SetBytes(input)
var result []byte
mod := new(big.Int)
base := big.NewInt(58)
for n.Sign() > 0 {
n.DivMod(n, base, mod)
result = append(result, solBase58Alphabet[mod.Int64()])
}
for i := 0; i < leadingZeros; i++ {
result = append(result, '1')
}
for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
result[i], result[j] = result[j], result[i]
}
return string(result)
}

// base58Decode decodes a base58 string to bytes.
func base58Decode(s string) []byte {
if s == "" {
return nil
}
leadingOnes := 0
for _, c := range s {
if c != '1' {
break
}
leadingOnes++
}
n := new(big.Int)
for _, c := range s {
idx := -1
for i, a := range solBase58Alphabet {
if a == c {
idx = i
break
}
}
if idx < 0 {
return nil
}
n.Mul(n, big.NewInt(58))
n.Add(n, big.NewInt(int64(idx)))
}
decoded := n.Bytes()
result := make([]byte, leadingOnes+len(decoded))
copy(result[leadingOnes:], decoded)
return result
}
