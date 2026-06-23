package contracts

import (
	"crypto/sha256"
	"encoding/binary"
)

// ShardIndex deterministically maps a token address to a wallet shard index
// in [0, shardCount). Uses SHA256(tokenAddress)[0:8] as a big-endian uint64
// modulo shardCount. Same input always maps to the same shard (replay-safe).
//
// Returns 0 when shardCount <= 0 (callers should treat that as "no sharding").
// Lives in contracts/ because it derives a value placed on AllocationDTO and
// is the single source of truth shared by Layer 7 (capital) and Layer 8
// (execution) — both must agree on the routing.
func ShardIndex(tokenAddress string, shardCount int) int32 {
	if shardCount <= 0 {
		return 0
	}
	h := sha256.Sum256([]byte(tokenAddress))
	return int32(binary.BigEndian.Uint64(h[:8]) % uint64(shardCount))
}
