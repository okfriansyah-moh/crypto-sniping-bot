package execution_solana

// recent_blockhash.go — cached blockhash with TTL.
// Avoids redundant GetLatestBlockhash calls within the confirm timeout window.

import (
	"context"
	"sync"
	"time"
)

// BlockhashCache caches the most recent blockhash for a configurable TTL.
type BlockhashCache struct {
	mu            sync.Mutex
	client        SolanaClient
	commitment    string
	ttl           time.Duration
	hash          string
	lastValidSlot uint64
	fetchedAt     time.Time
}

// NewBlockhashCache creates a cache with the given TTL.
func NewBlockhashCache(client SolanaClient, commitment string, ttlMs int) *BlockhashCache {
	ttl := time.Duration(ttlMs) * time.Millisecond
	if ttl <= 0 {
		ttl = 2 * time.Second
	}
	return &BlockhashCache{
		client:     client,
		commitment: commitment,
		ttl:        ttl,
	}
}

// Get returns a fresh blockhash, refreshing from the network if the cache is stale.
func (c *BlockhashCache) Get(ctx context.Context) (string, uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.hash != "" && time.Since(c.fetchedAt) < c.ttl {
		return c.hash, c.lastValidSlot, nil
	}

	hash, lastValid, err := c.client.GetLatestBlockhash(ctx, c.commitment)
	if err != nil {
		return "", 0, err
	}
	c.hash = hash
	c.lastValidSlot = lastValid
	c.fetchedAt = time.Now()
	return hash, lastValid, nil
}

// Invalidate forces the next Get to fetch a fresh blockhash.
func (c *BlockhashCache) Invalidate() {
	c.mu.Lock()
	c.hash = ""
	c.mu.Unlock()
}
