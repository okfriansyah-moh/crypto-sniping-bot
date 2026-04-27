package execution

import (
	"testing"
)

// TestPickWallet_EmptyShards_ReturnsError verifies an error is returned when no shards are configured.
func TestPickWallet_EmptyShards_ReturnsError(t *testing.T) {
	_, err := PickWallet("0xTOKEN", nil)
	if err == nil {
		t.Fatal("expected error for empty shard list")
	}
}

// TestPickWallet_SingleShard_AlwaysPicked verifies deterministic routing with one shard.
func TestPickWallet_SingleShard_AlwaysPicked(t *testing.T) {
	shards := []WalletConfig{
		{Address: "0xWALLET1", ShardIndex: 0},
	}
	w, err := PickWallet("0xANYTOKEN", shards)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Address != "0xWALLET1" {
		t.Errorf("expected 0xWALLET1, got %q", w.Address)
	}
}

// TestPickWallet_Deterministic verifies same token always maps to same shard.
func TestPickWallet_Deterministic(t *testing.T) {
	shards := []WalletConfig{
		{Address: "0xWALLET_A", ShardIndex: 0},
		{Address: "0xWALLET_B", ShardIndex: 1},
		{Address: "0xWALLET_C", ShardIndex: 2},
	}
	token := "0xDEADBEEFCAFEBABE1234567890ABCDEF"

	first, err := PickWallet(token, shards)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Repeat multiple times to confirm determinism.
	for i := 0; i < 5; i++ {
		w, _ := PickWallet(token, shards)
		if w.Address != first.Address {
			t.Errorf("non-deterministic: first=%q, got=%q on iteration %d", first.Address, w.Address, i)
		}
	}
}

// TestPickWallet_DifferentTokens_CanMapToDifferentShards verifies tokens spread across shards.
func TestPickWallet_DifferentTokens_CanMapToDifferentShards(t *testing.T) {
	shards := []WalletConfig{
		{Address: "0xW0", ShardIndex: 0},
		{Address: "0xW1", ShardIndex: 1},
	}
	// Use two tokens known to produce different shard indices with SHA256 % 2.
	tokens := []string{
		"0x1111111111111111111111111111111111111111",
		"0x2222222222222222222222222222222222222222",
		"0x3333333333333333333333333333333333333333",
		"0x4444444444444444444444444444444444444444",
	}
	seen := map[string]bool{}
	for _, tok := range tokens {
		w, _ := PickWallet(tok, shards)
		seen[w.Address] = true
	}
	// With 4 tokens and 2 shards at least both shards should be hit.
	if len(seen) < 2 {
		t.Error("expected tokens to map to multiple shards")
	}
}

// TestShardIndex_ZeroCount_ReturnsZero verifies shardCount<=0 is safe.
func TestShardIndex_ZeroCount_ReturnsZero(t *testing.T) {
	if idx := ShardIndex("0xTOKEN", 0); idx != 0 {
		t.Errorf("expected 0 for zero shard count, got %d", idx)
	}
}

// TestShardIndex_InRange verifies result is always in [0, shardCount).
func TestShardIndex_InRange(t *testing.T) {
	tokens := []string{
		"0xAAAA",
		"0xBBBB",
		"0xCCCC",
		"0xDDDD",
		"0x0000000000000000000000000000000000000000",
	}
	for _, tok := range tokens {
		for n := 1; n <= 8; n++ {
			idx := ShardIndex(tok, n)
			if idx < 0 || idx >= n {
				t.Errorf("ShardIndex(%q, %d) = %d: out of range [0, %d)", tok, n, idx, n)
			}
		}
	}
}

// TestShardIndex_Deterministic verifies same token+count always returns same index.
func TestShardIndex_Deterministic(t *testing.T) {
	const token = "0xFEEDFACEDEADBEEF"
	const count = 7
	first := ShardIndex(token, count)
	for i := 0; i < 10; i++ {
		if got := ShardIndex(token, count); got != first {
			t.Errorf("non-deterministic: first=%d, got=%d", first, got)
		}
	}
}
