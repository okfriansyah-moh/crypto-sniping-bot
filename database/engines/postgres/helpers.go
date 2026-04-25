package postgres

import (
	"crypto/sha256"
)

// hashBytes returns the SHA256 of data.
func hashBytes(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}
