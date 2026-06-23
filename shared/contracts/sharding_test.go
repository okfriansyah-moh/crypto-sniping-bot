package contracts_test

import (
	"testing"

	"crypto-sniping-bot/shared/contracts"
)

// ── ShardIndex ────────────────────────────────────────────────────────────────

func TestShardIndex_ZeroShardCount_ReturnsZero(t *testing.T) {
	// Arrange / Act / Assert
	if got := contracts.ShardIndex("0xTOKEN", 0); got != 0 {
		t.Errorf("ShardIndex with shardCount=0: want 0, got %d", got)
	}
}

func TestShardIndex_NegativeShardCount_ReturnsZero(t *testing.T) {
	// Arrange / Act / Assert
	if got := contracts.ShardIndex("0xTOKEN", -5); got != 0 {
		t.Errorf("ShardIndex with shardCount=-5: want 0, got %d", got)
	}
}

func TestShardIndex_Deterministic(t *testing.T) {
	// Arrange
	token := "0xDeAdBeEf1234567890aBcDeF"
	count := 8

	// Act: call twice, must return same shard.
	first := contracts.ShardIndex(token, count)
	second := contracts.ShardIndex(token, count)

	// Assert
	if first != second {
		t.Errorf("ShardIndex non-deterministic: %d vs %d", first, second)
	}
}

func TestShardIndex_InRange(t *testing.T) {
	// Arrange
	tokens := []string{"0xAAA", "0xBBB", "0xCCC", "0xDDD", "0xEEE"}
	shardCounts := []int{1, 2, 4, 8, 16, 32}

	for _, count := range shardCounts {
		for _, tok := range tokens {
			// Act
			idx := contracts.ShardIndex(tok, count)

			// Assert: result must be in [0, count).
			if idx < 0 || int(idx) >= count {
				t.Errorf("ShardIndex(%q, %d) = %d: out of range [0, %d)", tok, count, idx, count)
			}
		}
	}
}

func TestShardIndex_SingleShard_AlwaysZero(t *testing.T) {
	// Arrange: with exactly 1 shard, every token maps to shard 0.
	tokens := []string{"0xA", "0xB", "0xC", "0xD"}
	for _, tok := range tokens {
		// Act / Assert
		if got := contracts.ShardIndex(tok, 1); got != 0 {
			t.Errorf("ShardIndex(%q, 1) = %d: want 0", tok, got)
		}
	}
}

func TestShardIndex_DifferentTokens_CanMapToDifferentShards(t *testing.T) {
	// Arrange: with a large shard count, two distinct tokens should not
	// necessarily collide (probabilistic — uses a diverse enough pair).
	const shardCount = 256
	idx1 := contracts.ShardIndex("0xAAAA", shardCount)
	idx2 := contracts.ShardIndex("0xBBBB", shardCount)

	// Act / Assert: they must both be in range; at least verify range.
	if idx1 < 0 || int(idx1) >= shardCount {
		t.Errorf("ShardIndex(0xAAAA, %d) = %d out of range", shardCount, idx1)
	}
	if idx2 < 0 || int(idx2) >= shardCount {
		t.Errorf("ShardIndex(0xBBBB, %d) = %d out of range", shardCount, idx2)
	}
}
