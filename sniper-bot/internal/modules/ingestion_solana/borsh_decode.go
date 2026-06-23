package ingestion_solana

// borsh_decode.go — minimal Borsh binary format helpers.
// Borsh uses little-endian encoding for all integers.
// Reference: https://borsh.io/

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Reader wraps a byte slice for sequential Borsh decoding.
type Reader struct {
	data []byte
	pos  int
}

// NewReader creates a Borsh reader over data.
func NewReader(data []byte) *Reader {
	return &Reader{data: data}
}

// Remaining returns the number of unread bytes.
func (r *Reader) Remaining() int {
	return len(r.data) - r.pos
}

// ReadU8 reads a single byte.
func (r *Reader) ReadU8() (byte, error) {
	if r.pos >= len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	b := r.data[r.pos]
	r.pos++
	return b, nil
}

// ReadU32 reads a uint32 in little-endian order.
func (r *Reader) ReadU32() (uint32, error) {
	if r.pos+4 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.LittleEndian.Uint32(r.data[r.pos:])
	r.pos += 4
	return v, nil
}

// ReadU64 reads a uint64 in little-endian order.
func (r *Reader) ReadU64() (uint64, error) {
	if r.pos+8 > len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	v := binary.LittleEndian.Uint64(r.data[r.pos:])
	r.pos += 8
	return v, nil
}

// ReadI64 reads an int64 in little-endian order.
func (r *Reader) ReadI64() (int64, error) {
	v, err := r.ReadU64()
	return int64(v), err
}

// ReadBytes reads exactly n bytes.
func (r *Reader) ReadBytes(n int) ([]byte, error) {
	if r.pos+n > len(r.data) {
		return nil, io.ErrUnexpectedEOF
	}
	b := make([]byte, n)
	copy(b, r.data[r.pos:r.pos+n])
	r.pos += n
	return b, nil
}

// ReadPublicKey reads a 32-byte Solana public key as a base58 string.
func (r *Reader) ReadPublicKey() (string, error) {
	raw, err := r.ReadBytes(32)
	if err != nil {
		return "", err
	}
	return base58Encode(raw), nil
}

// ReadString reads a Borsh-encoded string: u32 length + UTF-8 bytes.
func (r *Reader) ReadString() (string, error) {
	length, err := r.ReadU32()
	if err != nil {
		return "", err
	}
	if length > 65535 {
		return "", fmt.Errorf("borsh string too long: %d", length)
	}
	raw, err := r.ReadBytes(int(length))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// MatchDiscriminator checks that the first 8 bytes of data match disc.
// Anchor programs use 8-byte discriminators derived from the instruction name.
func MatchDiscriminator(data []byte, disc [8]byte) bool {
	if len(data) < 8 {
		return false
	}
	for i := 0; i < 8; i++ {
		if data[i] != disc[i] {
			return false
		}
	}
	return true
}

// base58Alphabet is the Bitcoin/Solana base58 alphabet.
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// base58Encode encodes a byte slice to a base58 string.
func base58Encode(input []byte) string {
	if len(input) == 0 {
		return ""
	}

	// Count leading zeros.
	leadingZeros := 0
	for _, b := range input {
		if b != 0 {
			break
		}
		leadingZeros++
	}

	// Copy input to avoid mutation.
	buf := make([]byte, len(input))
	copy(buf, input)

	// Convert to base58.
	var result []byte
	for len(buf) > 0 {
		remainder := 0
		var next []byte
		for _, b := range buf {
			cur := remainder*256 + int(b)
			digit := cur / 58
			remainder = cur % 58
			if len(next) > 0 || digit > 0 {
				next = append(next, byte(digit))
			}
		}
		result = append(result, base58Alphabet[remainder])
		buf = next
	}

	// Add leading '1' characters for leading zeros.
	for i := 0; i < leadingZeros; i++ {
		result = append(result, '1')
	}

	// Reverse the result.
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return string(result)
}
