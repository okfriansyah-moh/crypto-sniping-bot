package execution

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// WalletConfig holds a single wallet shard's credentials.
type WalletConfig struct {
	// Address is the wallet's Ethereum address (checksummed hex).
	Address string
	// PrivateKey is the hex-encoded private key (no 0x prefix). Never logged.
	// json:"-" prevents accidental serialization to JSON (e.g. debug snapshots).
	PrivateKey string `json:"-"`
	// ShardIndex is the 0-based index of this shard.
	ShardIndex int
}

// PickWallet deterministically routes tokenAddress to a wallet shard.
// The routing function is: shard = SHA256(tokenAddress)[0:8] % n (big-endian uint64).
// Same tokenAddress always maps to the same shard — guaranteed deterministic.
func PickWallet(tokenAddress string, shards []WalletConfig) (WalletConfig, error) {
	if len(shards) == 0 {
		return WalletConfig{}, fmt.Errorf("execution: no wallet shards configured")
	}
	n := uint64(len(shards))
	h := sha256.Sum256([]byte(tokenAddress))
	idx := binary.BigEndian.Uint64(h[:8]) % n
	return shards[idx], nil
}

// ShardIndex returns the shard index for a given token address and shard count.
// Exported for observability / logging without returning full WalletConfig.
func ShardIndex(tokenAddress string, shardCount int) int {
	if shardCount <= 0 {
		return 0
	}
	h := sha256.Sum256([]byte(tokenAddress))
	return int(binary.BigEndian.Uint64(h[:8]) % uint64(shardCount))
}
