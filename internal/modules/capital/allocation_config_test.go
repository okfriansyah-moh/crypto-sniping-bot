package capital

import (
	"context"
	"testing"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// TestProcess_Phase10_ConfigDrivenAllocationFields verifies Task B:
// MaxSlippageBps, WalletShard, CohortID are sourced from CapitalConfig
// (and that ShardIndex is deterministic for the same token address).
func TestProcess_Phase10_ConfigDrivenAllocationFields(t *testing.T) {
	cfg := &config.CapitalConfig{
		FixedEntrySizeUsd:     10.0,
		MaxSizeUsd:            100.0,
		TTLSeconds:            3,
		DefaultMaxSlippageBps: 175,
		WalletShardCount:      4,
		DefaultCohortID:       "new_launch",
	}
	m := New(cfg)

	in := contracts.SelectionOutputDTO{
		EventID:          "ev-1",
		TraceID:          "tr-1",
		CorrelationID:    "co-1",
		VersionID:        "v-1",
		TokenLifecycleID: "tl-1",
		TokenAddress:     "0xdeadbeef",
		Selected:         true,
	}

	out, err := m.Process(context.Background(), in, "ethereum")
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out.MaxSlippageBps != 175 {
		t.Errorf("MaxSlippageBps = %d, want 175 (from cfg)", out.MaxSlippageBps)
	}
	if out.CohortID != "new_launch" {
		t.Errorf("CohortID = %q, want %q", out.CohortID, "new_launch")
	}
	wantShard := contracts.ShardIndex("0xdeadbeef", 4)
	if out.WalletShard != wantShard {
		t.Errorf("WalletShard = %d, want %d", out.WalletShard, wantShard)
	}
	// Determinism: same token → same shard on repeat call.
	out2, _ := m.Process(context.Background(), in, "ethereum")
	if out.WalletShard != out2.WalletShard {
		t.Errorf("ShardIndex non-deterministic: %d vs %d", out.WalletShard, out2.WalletShard)
	}
}

// TestProcess_Phase10_LegacyDefaults verifies backward-compat: when the
// new CapitalConfig fields are zero, the legacy values (200 / 0 / "default")
// are emitted exactly as before.
func TestProcess_Phase10_LegacyDefaults(t *testing.T) {
	cfg := &config.CapitalConfig{
		FixedEntrySizeUsd: 10.0,
		MaxSizeUsd:        100.0,
		TTLSeconds:        3,
	}
	m := New(cfg)

	in := contracts.SelectionOutputDTO{
		EventID:      "ev-2",
		TraceID:      "tr-2",
		VersionID:    "v-2",
		TokenAddress: "0xcafebabe",
		Selected:     true,
	}

	out, err := m.Process(context.Background(), in, "ethereum")
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out.MaxSlippageBps != 200 {
		t.Errorf("MaxSlippageBps = %d, want 200 (legacy default)", out.MaxSlippageBps)
	}
	if out.WalletShard != 0 {
		t.Errorf("WalletShard = %d, want 0 (sharding disabled)", out.WalletShard)
	}
	if out.CohortID != "default" {
		t.Errorf("CohortID = %q, want %q", out.CohortID, "default")
	}
}
