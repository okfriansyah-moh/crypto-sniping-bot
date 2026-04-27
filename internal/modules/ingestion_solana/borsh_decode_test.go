package ingestion_solana

// borsh_decode_test.go — internal tests for the Borsh decoder and base58 encoder.
// Uses `package ingestion_solana` (not _test suffix) to access unexported helpers.

import (
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

// ── Reader.Remaining ─────────────────────────────────────────────────────────

func TestReaderRemaining_Initial(t *testing.T) {
	t.Parallel()
	r := NewReader([]byte{1, 2, 3})
	if got := r.Remaining(); got != 3 {
		t.Errorf("Remaining() = %d, want 3", got)
	}
}

func TestReaderRemaining_AfterRead(t *testing.T) {
	t.Parallel()
	r := NewReader([]byte{1, 2, 3, 4})
	_, _ = r.ReadU8()
	if got := r.Remaining(); got != 3 {
		t.Errorf("Remaining() after ReadU8 = %d, want 3", got)
	}
}

func TestReaderRemaining_Empty(t *testing.T) {
	t.Parallel()
	r := NewReader([]byte{})
	if got := r.Remaining(); got != 0 {
		t.Errorf("Remaining() on empty = %d, want 0", got)
	}
}

// ── Reader.ReadU8 EOF ─────────────────────────────────────────────────────────

func TestReadU8_EOF(t *testing.T) {
	t.Parallel()
	r := NewReader([]byte{})
	_, err := r.ReadU8()
	if err == nil {
		t.Fatal("expected EOF error for empty reader, got nil")
	}
	if err != io.ErrUnexpectedEOF {
		t.Errorf("expected io.ErrUnexpectedEOF, got %v", err)
	}
}

// ── Reader.ReadI64 ───────────────────────────────────────────────────────────

func TestReadI64_HappyPath(t *testing.T) {
	t.Parallel()
	// Arrange: encode -1 as little-endian int64. Use all-0xFF bytes which is
	// the two's-complement representation of -1 for int64.
	data := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

	// Act
	r := NewReader(data)
	v, err := r.ReadI64()

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != -1 {
		t.Errorf("ReadI64() = %d, want -1", v)
	}
}

func TestReadI64_PositiveValue(t *testing.T) {
	t.Parallel()
	data := make([]byte, 8)
	binary.LittleEndian.PutUint64(data, 42)

	r := NewReader(data)
	v, err := r.ReadI64()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != 42 {
		t.Errorf("ReadI64() = %d, want 42", v)
	}
}

func TestReadI64_EOF(t *testing.T) {
	t.Parallel()
	r := NewReader([]byte{0x01, 0x02}) // too short for int64
	_, err := r.ReadI64()
	if err == nil {
		t.Fatal("expected EOF error, got nil")
	}
	if err != io.ErrUnexpectedEOF {
		t.Errorf("expected io.ErrUnexpectedEOF, got %v", err)
	}
}

// ── Reader.ReadPublicKey ─────────────────────────────────────────────────────

func TestReadPublicKey_HappyPath(t *testing.T) {
	t.Parallel()
	// Arrange: 32 non-zero bytes → valid base58 key
	data := make([]byte, 32)
	for i := range data {
		data[i] = byte(i + 1) // 1..32
	}

	r := NewReader(data)
	key, err := r.ReadPublicKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(key) == 0 {
		t.Error("ReadPublicKey() returned empty string")
	}
	// Verify it only contains base58 characters.
	for _, ch := range key {
		if !strings.ContainsRune(base58Alphabet, ch) {
			t.Errorf("ReadPublicKey() returned non-base58 char %q in %q", ch, key)
			break
		}
	}
}

func TestReadPublicKey_EOF(t *testing.T) {
	t.Parallel()
	r := NewReader([]byte{0x01, 0x02}) // only 2 bytes, need 32
	_, err := r.ReadPublicKey()
	if err == nil {
		t.Fatal("expected error for short data, got nil")
	}
}

// ── base58Encode ─────────────────────────────────────────────────────────────

func TestBase58Encode_Empty(t *testing.T) {
	t.Parallel()
	if got := base58Encode(nil); got != "" {
		t.Errorf("base58Encode(nil) = %q, want empty", got)
	}
	if got := base58Encode([]byte{}); got != "" {
		t.Errorf("base58Encode([]) = %q, want empty", got)
	}
}

func TestBase58Encode_AllZeros(t *testing.T) {
	t.Parallel()
	// All-zero byte slice → all '1' characters in base58.
	data := make([]byte, 4)
	got := base58Encode(data)
	for _, ch := range got {
		if ch != '1' {
			t.Errorf("base58Encode of zeros got %q, want all '1's", got)
			break
		}
	}
	if len(got) == 0 {
		t.Error("base58Encode of zeros returned empty string")
	}
}

func TestBase58Encode_SingleByte(t *testing.T) {
	t.Parallel()
	// Known value: [0x01] → "2" in base58.
	got := base58Encode([]byte{0x01})
	if got != "2" {
		t.Errorf("base58Encode([0x01]) = %q, want \"2\"", got)
	}
}

func TestBase58Encode_Deterministic(t *testing.T) {
	t.Parallel()
	data := []byte{0x11, 0x22, 0x33, 0x44, 0x55}
	first := base58Encode(data)
	second := base58Encode(data)
	if first != second {
		t.Errorf("base58Encode not deterministic: %q != %q", first, second)
	}
}

// ── Reader.ReadString ────────────────────────────────────────────────────────

func TestReadString_HappyPath(t *testing.T) {
	t.Parallel()
	const s = "hello"
	var buf []byte
	lenBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(lenBuf, uint32(len(s)))
	buf = append(buf, lenBuf...)
	buf = append(buf, []byte(s)...)

	r := NewReader(buf)
	got, err := r.ReadString()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != s {
		t.Errorf("ReadString() = %q, want %q", got, s)
	}
}

func TestReadString_TooLong(t *testing.T) {
	t.Parallel()
	// length field = 65536 (> 65535 limit)
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, 65536)

	r := NewReader(buf)
	_, err := r.ReadString()
	if err == nil {
		t.Fatal("expected error for too-long string, got nil")
	}
}

func TestReadString_EOF(t *testing.T) {
	t.Parallel()
	// Only 2 bytes — not enough for even the length prefix.
	r := NewReader([]byte{0x05, 0x00})
	_, err := r.ReadString()
	if err == nil {
		t.Fatal("expected EOF error, got nil")
	}
}

// ── MatchDiscriminator ───────────────────────────────────────────────────────

func TestMatchDiscriminator_TooShort(t *testing.T) {
	t.Parallel()
	disc := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	if MatchDiscriminator([]byte{1, 2, 3}, disc) {
		t.Error("MatchDiscriminator should return false for data shorter than 8 bytes")
	}
}

func TestMatchDiscriminator_Mismatch(t *testing.T) {
	t.Parallel()
	disc := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	data := []byte{0, 2, 3, 4, 5, 6, 7, 8, 9} // first byte differs
	if MatchDiscriminator(data, disc) {
		t.Error("MatchDiscriminator should return false when first byte differs")
	}
}

func TestMatchDiscriminator_Match(t *testing.T) {
	t.Parallel()
	disc := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	data := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10} // match + extra
	if !MatchDiscriminator(data, disc) {
		t.Error("MatchDiscriminator should return true for matching discriminator")
	}
}

// ── blockTimestamp ────────────────────────────────────────────────────────────

func TestBlockTimestamp_Zero_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	if got := blockTimestamp(0); got != "" {
		t.Errorf("blockTimestamp(0) = %q, want empty string", got)
	}
}

func TestBlockTimestamp_NonZero_ReturnsRFC3339(t *testing.T) {
	t.Parallel()
	got := blockTimestamp(1700000000)
	if len(got) == 0 {
		t.Error("blockTimestamp(non-zero) returned empty string")
	}
	// Should contain 'T' and 'Z' as per RFC3339 UTC.
	if got[len(got)-1] != 'Z' {
		t.Errorf("blockTimestamp should end with Z (UTC), got %q", got)
	}
}
