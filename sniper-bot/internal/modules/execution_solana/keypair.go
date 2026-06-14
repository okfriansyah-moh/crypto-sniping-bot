package execution_solana

// keypair.go — ed25519 keypair loading from a JSON key file.
//
// Solana keypairs are stored as JSON arrays of 64 bytes:
//   [byte0, ..., byte63]
// where bytes 0-31 are the private key seed and bytes 32-63 are the public key.
// The public key is also derivable from the seed via ed25519.
//
// This matches the format written by `solana-keygen new` and all standard tooling.

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
)

// Keypair holds an ed25519 keypair for signing Solana transactions.
type Keypair struct {
	PrivateKey ed25519.PrivateKey
	PublicKey  ed25519.PublicKey
}

// PublicKeyBytes returns the raw 32-byte public key.
func (k *Keypair) PublicKeyBytes() []byte {
	return []byte(k.PublicKey)
}

// Sign signs msg using this keypair's private key.
func (k *Keypair) Sign(msg []byte) []byte {
	return ed25519.Sign(k.PrivateKey, msg)
}

// LoadKeypair loads a Solana keypair from a JSON key file.
// The file must contain a JSON array of exactly 64 bytes.
func LoadKeypair(path string) (*Keypair, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load keypair: read file %s: %w", path, err)
	}
	return ParseKeypairJSON(data)
}

// ParseKeypairJSON parses a Solana keypair from a JSON byte array.
func ParseKeypairJSON(data []byte) (*Keypair, error) {
	var raw []byte
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse keypair: invalid JSON: %w", err)
	}
	if len(raw) != 64 {
		return nil, fmt.Errorf("parse keypair: expected 64 bytes, got %d", len(raw))
	}
	// First 32 bytes: seed; last 32 bytes: public key (ignored — derived from seed).
	seed := raw[:32]
	privKey := ed25519.NewKeyFromSeed(seed)
	pubKey := privKey.Public().(ed25519.PublicKey)
	return &Keypair{
		PrivateKey: privKey,
		PublicKey:  pubKey,
	}, nil
}

// KeypairFromSeed creates a Keypair directly from a 32-byte seed.
// Used in tests.
func KeypairFromSeed(seed []byte) (*Keypair, error) {
	if len(seed) != 32 {
		return nil, fmt.Errorf("keypair_from_seed: expected 32 bytes, got %d", len(seed))
	}
	privKey := ed25519.NewKeyFromSeed(seed)
	pubKey := privKey.Public().(ed25519.PublicKey)
	return &Keypair{PrivateKey: privKey, PublicKey: pubKey}, nil
}

// selectKeypair deterministically selects a keypair by hashing tokenAddress.
// Wallet sharding: hash(tokenAddress) % len(keypairs).
func selectKeypair(keypairs []*Keypair, tokenAddress string) *Keypair {
	if len(keypairs) == 1 {
		return keypairs[0]
	}
	h := fnv32a(tokenAddress)
	return keypairs[int(h)%len(keypairs)]
}

// fnv32a computes FNV-1a 32-bit hash of s.
func fnv32a(s string) uint32 {
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)
	h := uint32(offset32)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= prime32
	}
	return h
}
