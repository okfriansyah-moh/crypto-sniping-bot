package execution_test

import (
	"math/big"
	"testing"

	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/execution"
)

func TestAdaptivePriorityFee_StaticMode(t *testing.T) {
	cfg := &config.ExecutionConfig{
		PriorityFee: config.PriorityFeeConfig{Mode: "static"},
	}
	in := big.NewInt(2_000_000_000) // 2 gwei
	out := execution.AdaptivePriorityFeeWei(in, 0.5, cfg)
	if out.Cmp(in) != 0 {
		t.Errorf("static mode: got %s, want %s", out, in)
	}
}

func TestAdaptivePriorityFee_AdaptiveScales(t *testing.T) {
	cfg := &config.ExecutionConfig{
		PriorityFee: config.PriorityFeeConfig{
			Mode:          "adaptive",
			MinMultiplier: 1.0,
			MaxMultiplier: 3.0,
		},
	}
	in := big.NewInt(2_000_000_000) // 2 gwei
	// +50% latency error → 1.5x → 3 gwei
	out := execution.AdaptivePriorityFeeWei(in, 0.5, cfg)
	want := big.NewInt(3_000_000_000)
	if out.Cmp(want) != 0 {
		t.Errorf("adaptive +50%%: got %s, want %s", out, want)
	}
}

func TestAdaptivePriorityFee_BoundedByMax(t *testing.T) {
	cfg := &config.ExecutionConfig{
		PriorityFee: config.PriorityFeeConfig{
			Mode:          "adaptive",
			MinMultiplier: 1.0,
			MaxMultiplier: 2.0,
		},
	}
	in := big.NewInt(1_000_000_000)
	// +500% latency error would normally produce 6x — clamped to 2x.
	out := execution.AdaptivePriorityFeeWei(in, 5.0, cfg)
	want := big.NewInt(2_000_000_000)
	if out.Cmp(want) != 0 {
		t.Errorf("max-bounded: got %s, want %s", out, want)
	}
}

func TestAdaptivePriorityFee_BoundedByMin(t *testing.T) {
	cfg := &config.ExecutionConfig{
		PriorityFee: config.PriorityFeeConfig{
			Mode:          "adaptive",
			MinMultiplier: 1.0, // never reduce below original
			MaxMultiplier: 3.0,
		},
	}
	in := big.NewInt(2_000_000_000)
	out := execution.AdaptivePriorityFeeWei(in, -0.5, cfg) // -50% latency under budget
	if out.Cmp(in) != 0 {
		t.Errorf("min-bounded: got %s, want >= %s", out, in)
	}
}

func TestAdaptivePriorityFee_NilCfg(t *testing.T) {
	in := big.NewInt(1_000_000_000)
	out := execution.AdaptivePriorityFeeWei(in, 1.0, nil)
	if out.Cmp(in) != 0 {
		t.Errorf("nil cfg: got %s, want %s", out, in)
	}
}
