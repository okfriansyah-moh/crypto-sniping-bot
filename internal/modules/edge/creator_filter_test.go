package edge

import (
	"testing"

	"crypto-sniping-bot/contracts"
)

func TestEvaluateCreatorFilters_Pass(t *testing.T) {
	got := EvaluateCreatorFilters(
		contracts.EdgeDTO{DevBuyPctBps: 1000, CreatorRugCount: 0, DevWalletAgeSeconds: 7200},
		CreatorFilterThresholds{MaxDevBuyPctBps: 5000, MaxCreatorRugCount: 1, MinDevWalletAgeSeconds: 3600},
	)
	if got != "" {
		t.Fatalf("expected pass, got %q", got)
	}
}

func TestEvaluateCreatorFilters_DevBuy(t *testing.T) {
	got := EvaluateCreatorFilters(
		contracts.EdgeDTO{DevBuyPctBps: 6000},
		CreatorFilterThresholds{MaxDevBuyPctBps: 5000},
	)
	if got != RejectReasonDevBuyTooHigh {
		t.Fatalf("got %q want %q", got, RejectReasonDevBuyTooHigh)
	}
}

func TestEvaluateCreatorFilters_RugRepeat(t *testing.T) {
	got := EvaluateCreatorFilters(
		contracts.EdgeDTO{CreatorRugCount: 2},
		CreatorFilterThresholds{MaxCreatorRugCount: 1},
	)
	if got != RejectReasonCreatorRugRepeat {
		t.Fatalf("got %q want %q", got, RejectReasonCreatorRugRepeat)
	}
}

func TestEvaluateCreatorFilters_NewWallet(t *testing.T) {
	got := EvaluateCreatorFilters(
		contracts.EdgeDTO{DevWalletAgeSeconds: 60},
		CreatorFilterThresholds{MinDevWalletAgeSeconds: 600},
	)
	if got != RejectReasonDevWalletTooNew {
		t.Fatalf("got %q want %q", got, RejectReasonDevWalletTooNew)
	}
}

func TestEvaluateCreatorFilters_UnknownAgeIgnored(t *testing.T) {
	// DevWalletAgeSeconds=0 means "unknown"; must NOT reject.
	got := EvaluateCreatorFilters(
		contracts.EdgeDTO{DevWalletAgeSeconds: 0},
		CreatorFilterThresholds{MinDevWalletAgeSeconds: 600},
	)
	if got != "" {
		t.Fatalf("expected pass on unknown wallet age, got %q", got)
	}
}

func TestEvaluateCreatorFilters_DisabledThresholds(t *testing.T) {
	got := EvaluateCreatorFilters(
		contracts.EdgeDTO{DevBuyPctBps: 9999, CreatorRugCount: 99, DevWalletAgeSeconds: 1},
		CreatorFilterThresholds{}, // all zero = disabled
	)
	if got != "" {
		t.Fatalf("disabled thresholds must pass, got %q", got)
	}
}
