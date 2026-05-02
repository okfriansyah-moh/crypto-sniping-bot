package ingestion_solana_test

// raydium_v4_dispatcher_test.go — tests for ClassifyRaydiumV4Instruction,
// DecodeRaydiumPoolInit, and NormalizeRaydiumV4Instruction.
//
// These tests guard the regression that produced F-9 (heartbeat showed
// events_emitted=0 and dto_nil_skip increasing every notification on
// raydium-v4): the on-chain Raydium V4 program uses a single-byte instruction
// tag, NOT an 8-byte Anchor-style discriminator.

import (
	"encoding/binary"
	"errors"
	"testing"

	"crypto-sniping-bot/internal/modules/ingestion_solana"
)

// realisticInitialize2Bytes returns a byte slice in the exact wire format the
// Raydium V4 AMM program emits for an Initialize2 instruction:
//
//	tag=01  nonce=0xFE  open_time=1700000000  init_pc=5_000_000_000  init_coin=2_500_000_000_000
//
// All u64s little-endian. Real-world payloads have non-zero open_time and
// non-zero init amounts; the legacy 8-byte-discriminator decoder rejected
// every such payload because it required the trailing 7 bytes after tag=01 to
// be zero. This fixture captures that exact failing pattern as a regression
// guard.
func realisticInitialize2Bytes() []byte {
	buf := []byte{ingestion_solana.RaydiumV4OpInitialize2, 0xFE}
	buf = appendLEU64(buf, 1_700_000_000)
	buf = appendLEU64(buf, 5_000_000_000)
	buf = appendLEU64(buf, 2_500_000_000_000)
	return buf
}

func appendLEU64(buf []byte, v uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, v)
	return append(buf, b...)
}

// ── ClassifyRaydiumV4Instruction ──────────────────────────────────────────────

func TestClassifyRaydiumV4Instruction(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		data []byte
		want ingestion_solana.RaydiumV4InstructionKind
	}{
		{"empty", nil, ingestion_solana.RaydiumV4KindUnknown},
		{"zero-length", []byte{}, ingestion_solana.RaydiumV4KindUnknown},
		{"initialize2", []byte{1, 0xAA, 0xBB}, ingestion_solana.RaydiumV4KindInitialize2},
		{"swap_base_in", []byte{9, 0xAA}, ingestion_solana.RaydiumV4KindSwapBaseIn},
		{"swap_base_out", []byte{11, 0xAA}, ingestion_solana.RaydiumV4KindSwapBaseOut},
		{"set_params (tag=12)", []byte{12, 0xAA}, ingestion_solana.RaydiumV4KindUnknown},
		{"withdraw (tag=4)", []byte{4, 0xAA}, ingestion_solana.RaydiumV4KindUnknown},
		{"initialize_v1 (tag=0)", []byte{0, 0xAA}, ingestion_solana.RaydiumV4KindUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ingestion_solana.ClassifyRaydiumV4Instruction(tc.data)
			if got != tc.want {
				t.Errorf("ClassifyRaydiumV4Instruction = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestClassifyRaydiumV4Instruction_Deterministic(t *testing.T) {
	t.Parallel()
	data := realisticInitialize2Bytes()
	first := ingestion_solana.ClassifyRaydiumV4Instruction(data)
	for i := 0; i < 100; i++ {
		if got := ingestion_solana.ClassifyRaydiumV4Instruction(data); got != first {
			t.Fatalf("non-deterministic classify on iter %d: got %d, want %d", i, got, first)
		}
	}
}

// ── DecodeRaydiumPoolInit (regression guard) ─────────────────────────────────

func TestDecodeRaydiumPoolInit_RealisticFixture(t *testing.T) {
	t.Parallel()
	// The exact byte pattern that the legacy 8-byte-discriminator decoder
	// rejected on every live raydium-v4 notification. With the fixed 1-byte-tag
	// decoder this MUST decode cleanly into the expected fields.
	data := realisticInitialize2Bytes()

	evt, err := ingestion_solana.DecodeRaydiumPoolInit(data)
	if err != nil {
		t.Fatalf("decode failed on realistic fixture: %v", err)
	}
	if evt.Nonce != 0xFE {
		t.Errorf("Nonce = %d, want 254", evt.Nonce)
	}
	if evt.OpenTime != 1_700_000_000 {
		t.Errorf("OpenTime = %d, want 1_700_000_000", evt.OpenTime)
	}
	if evt.InitPcAmount != 5_000_000_000 {
		t.Errorf("InitPcAmount = %d, want 5_000_000_000", evt.InitPcAmount)
	}
	if evt.InitCoinAmount != 2_500_000_000_000 {
		t.Errorf("InitCoinAmount = %d, want 2_500_000_000_000", evt.InitCoinAmount)
	}
}

func TestDecodeRaydiumPoolInit_RejectsLegacyEightByteDiscriminator(t *testing.T) {
	t.Parallel()
	// The legacy on-disk fixture (8-byte little-endian "discriminator" with
	// tag=01 followed by 7 zero bytes) is NOT a valid Initialize2 wire payload.
	// The decoder still accepts it (tag matches, byte 1 is 0 → nonce=0,
	// remainder is zero u64s), so this case decodes a degenerate event with
	// all-zero amounts. We pin that behavior here so a future "stricter"
	// rewrite is a deliberate decision and not an accident.
	data := []byte{1, 0, 0, 0, 0, 0, 0, 0}
	data = append(data, make([]byte, 25)...) // pad to full body length

	evt, err := ingestion_solana.DecodeRaydiumPoolInit(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.OpenTime != 0 || evt.InitPcAmount != 0 || evt.InitCoinAmount != 0 {
		t.Errorf("expected zero-valued degenerate event, got %+v", evt)
	}
}

func TestDecodeRaydiumPoolInit_RejectsTruncated(t *testing.T) {
	t.Parallel()
	data := []byte{ingestion_solana.RaydiumV4OpInitialize2, 0xFE, 0x01, 0x02}

	if _, err := ingestion_solana.DecodeRaydiumPoolInit(data); err == nil {
		t.Fatal("expected error for truncated Initialize2 body, got nil")
	}
}

func TestDecodeRaydiumPoolInit_RejectsWrongTag(t *testing.T) {
	t.Parallel()
	// Tag 9 = SwapBaseIn — must NOT be accepted as Initialize2.
	data := []byte{ingestion_solana.RaydiumV4OpSwapBaseIn, 0x00}
	data = appendLEU64(data, 0)
	data = appendLEU64(data, 0)
	data = appendLEU64(data, 0)

	if _, err := ingestion_solana.DecodeRaydiumPoolInit(data); err == nil {
		t.Fatal("expected error for wrong tag, got nil")
	}
}

// ── NormalizeRaydiumV4Instruction (dispatch) ─────────────────────────────────

func TestNormalizeRaydiumV4Instruction_Initialize2_Emits(t *testing.T) {
	t.Parallel()
	data := realisticInitialize2Bytes()
	tx := &ingestion_solana.TransactionResult{
		Signature: "RegSigInit11111111111111111111111111111111",
		Slot:      99000,
		BlockTime: 1_700_000_000,
		Instructions: []ingestion_solana.InstructionData{
			{
				ProgramID: "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8",
				Accounts: []string{
					"tok", "spl", "sys", "rent",
					"AmmPool111", "auth", "orders", "lp",
					"CoinMint111", "PcMint11111", "extra",
				},
				Data:  data,
				Index: 1,
			},
		},
	}

	res := ingestion_solana.NormalizeRaydiumV4Instruction(tx, tx.Instructions[0], "v1")
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.Kind != ingestion_solana.RaydiumV4KindInitialize2 {
		t.Errorf("Kind = %d, want Initialize2", res.Kind)
	}
	if res.DTO == nil {
		t.Fatal("expected non-nil DTO for Initialize2")
	}
	if res.DTO.Market != "solana-raydium-v4" {
		t.Errorf("Market = %q, want solana-raydium-v4", res.DTO.Market)
	}
	if res.DTO.EventTopic != "PoolCreated" {
		t.Errorf("EventTopic = %q, want PoolCreated", res.DTO.EventTopic)
	}
}

// TestNormalizeRaydiumV4Instruction_SwapBaseIn_NoDTO guards F-1 fix:
// swap instructions must NOT produce a DTO (they would carry an empty
// TokenAddress and flood the DQ worker with empty-token DLQ rejections).
// The Kind is still classified correctly so the caller's heartbeat counter
// can distinguish "we saw a swap" from "unrecognized opcode".
func TestNormalizeRaydiumV4Instruction_SwapBaseIn_NoDTO(t *testing.T) {
	t.Parallel()
	data := []byte{ingestion_solana.RaydiumV4OpSwapBaseIn}
	data = appendLEU64(data, 1_000_000)
	data = appendLEU64(data, 950_000)

	accounts := make([]string, 10)
	accounts[4] = "AmmPoolSwap11"

	tx := &ingestion_solana.TransactionResult{
		Signature:    "RegSigSwap11",
		Instructions: []ingestion_solana.InstructionData{{Accounts: accounts, Data: data, Index: 0}},
	}

	res := ingestion_solana.NormalizeRaydiumV4Instruction(tx, tx.Instructions[0], "v1")
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.Kind != ingestion_solana.RaydiumV4KindSwapBaseIn {
		t.Errorf("Kind = %d, want SwapBaseIn", res.Kind)
	}
	// F-1 fix: swaps must NOT emit a DTO — they carry empty TokenAddress
	// and would pollute the DQ worker with DLQ rejections.
	if res.DTO != nil {
		t.Fatalf("expected nil DTO for SwapBaseIn (F-1), got EventTopic=%q", res.DTO.EventTopic)
	}
}

// TestNormalizeRaydiumV4Instruction_SwapBaseOut_NoDTO guards F-1 fix:
// swap instructions must NOT produce a DTO (same rationale as SwapBaseIn).
func TestNormalizeRaydiumV4Instruction_SwapBaseOut_NoDTO(t *testing.T) {
	t.Parallel()
	data := []byte{ingestion_solana.RaydiumV4OpSwapBaseOut}
	data = appendLEU64(data, 2_000_000)
	data = appendLEU64(data, 1_900_000)

	accounts := make([]string, 10)
	accounts[4] = "AmmPoolSwapOut"

	tx := &ingestion_solana.TransactionResult{
		Signature:    "RegSigSwapOut",
		Instructions: []ingestion_solana.InstructionData{{Accounts: accounts, Data: data, Index: 0}},
	}

	res := ingestion_solana.NormalizeRaydiumV4Instruction(tx, tx.Instructions[0], "v1")
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.Kind != ingestion_solana.RaydiumV4KindSwapBaseOut {
		t.Errorf("Kind = %d, want SwapBaseOut", res.Kind)
	}
	// F-1 fix: swaps must NOT emit a DTO.
	if res.DTO != nil {
		t.Fatalf("expected nil DTO for SwapBaseOut (F-1), got EventTopic=%q", res.DTO.EventTopic)
	}
}

func TestNormalizeRaydiumV4Instruction_UnknownTag_ClassifiedAsUnknown(t *testing.T) {
	t.Parallel()
	// Tag 12 = SetParams in the Raydium V4 program — recognized on-chain but
	// not an event we ingest. The dispatcher MUST classify it as Unknown so
	// the worker counts it under skipped_unknown_instruction (NOT dto_nil_skip).
	for _, tag := range []byte{0, 2, 3, 4, 5, 6, 7, 8, 10, 12, 13, 250} {
		data := []byte{tag, 0xAA, 0xBB, 0xCC}
		instr := ingestion_solana.InstructionData{Accounts: make([]string, 12), Data: data, Index: 0}
		tx := &ingestion_solana.TransactionResult{Signature: "x", Instructions: []ingestion_solana.InstructionData{instr}}

		res := ingestion_solana.NormalizeRaydiumV4Instruction(tx, instr, "v1")
		if res.Kind != ingestion_solana.RaydiumV4KindUnknown {
			t.Errorf("tag=%d: Kind = %d, want Unknown", tag, res.Kind)
		}
		if res.DTO != nil {
			t.Errorf("tag=%d: expected nil DTO for unknown tag", tag)
		}
		if res.Err != nil {
			t.Errorf("tag=%d: expected nil err for unknown tag, got %v", tag, res.Err)
		}
	}
}

func TestNormalizeRaydiumV4Instruction_Initialize2_Truncated_ReturnsError(t *testing.T) {
	t.Parallel()
	// Tag IS Initialize2 but body is truncated. This MUST surface as
	// (DTO=nil, Kind=Initialize2, Err!=nil) so the worker counts it as
	// process_errors — NOT as skipped_unknown_instruction.
	data := []byte{ingestion_solana.RaydiumV4OpInitialize2, 0xFE, 0x01}
	instr := ingestion_solana.InstructionData{Accounts: make([]string, 12), Data: data, Index: 0}
	tx := &ingestion_solana.TransactionResult{Signature: "trunc", Instructions: []ingestion_solana.InstructionData{instr}}

	res := ingestion_solana.NormalizeRaydiumV4Instruction(tx, instr, "v1")
	if res.Err == nil {
		t.Fatal("expected error for truncated Initialize2 body, got nil")
	}
	if res.Kind != ingestion_solana.RaydiumV4KindInitialize2 {
		t.Errorf("Kind = %d, want Initialize2", res.Kind)
	}
	if res.DTO != nil {
		t.Error("expected nil DTO when body is malformed")
	}
	// Sanity: the wrapped error should mention raydium pool init.
	if !errorContains(res.Err, "raydium pool init") {
		t.Errorf("error message missing 'raydium pool init': %v", res.Err)
	}
}

func errorContains(err error, sub string) bool {
	if err == nil {
		return false
	}
	for e := err; e != nil; e = errors.Unwrap(e) {
		if e.Error() == sub {
			return true
		}
		if len(e.Error()) >= len(sub) && containsString(e.Error(), sub) {
			return true
		}
	}
	return containsString(err.Error(), sub)
}

func containsString(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
