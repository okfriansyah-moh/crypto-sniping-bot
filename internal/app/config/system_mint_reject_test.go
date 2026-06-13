package config_test

import (
	"testing"

	"crypto-sniping-bot/internal/app/config"
)

func TestSystemMintRejectConfig_EffectiveMintsDefaultsWSOL(t *testing.T) {
	t.Parallel()
	mints := config.SystemMintRejectConfig{}.EffectiveMints()
	if len(mints) != 1 || mints[0] != "So11111111111111111111111111111111111111112" {
		t.Fatalf("unexpected default mints: %v", mints)
	}
}

func TestSystemMintRejectConfig_ShouldRejectToken(t *testing.T) {
	t.Parallel()
	cfg := config.SystemMintRejectConfig{
		Enabled: true,
		Mints:   []string{"So11111111111111111111111111111111111111112"},
	}
	if !cfg.ShouldRejectToken("So11111111111111111111111111111111111111112") {
		t.Fatal("expected WSOL to be rejected when enabled")
	}
	if cfg.ShouldRejectToken("K93mdxqMgivPNTFEXnoUmN8WH5tNzrSJfaguevQpump") {
		t.Fatal("expected meme mint not to be rejected")
	}
	cfg.Enabled = false
	if cfg.ShouldRejectToken("So11111111111111111111111111111111111111112") {
		t.Fatal("expected WSOL to pass when reject disabled")
	}
}
