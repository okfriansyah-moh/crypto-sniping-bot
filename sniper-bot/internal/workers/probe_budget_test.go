package workers

import (
	"testing"

	"crypto-sniping-bot/internal/app/config"
)

func TestProbeBudget_CreditCeilingDefers(t *testing.T) {
	b := newProbeBudget(config.ProbesConfig{
		MaxProbeCreditsPerHour: 10,
		ProbeCreditCosts: map[string]int{
			"solana_holder_dist": 11,
		},
	})
	if b.tryConsume("websocket", []string{"solana_holder_dist"}) {
		t.Fatal("expected first consume to fail when single probe exceeds credit budget")
	}
}

func TestProbeBudget_RespectsFreshBucket(t *testing.T) {
	b := newProbeBudget(config.ProbesConfig{
		MaxProbesPerHour: 2,
		RateLimitBuckets: config.ProbeRateLimitBuckets{
			FreshTokensPerHour: 1,
		},
	})
	if !b.tryConsume("websocket", []string{"solana_authorities"}) {
		t.Fatal("expected first fresh token to consume budget")
	}
	if b.tryConsume("websocket", []string{"solana_authorities"}) {
		t.Fatal("expected second fresh token to be deferred")
	}
}

func TestProbeBudget_RescanUsesSeparateBucket(t *testing.T) {
	b := newProbeBudget(config.ProbesConfig{
		MaxProbesPerHour: 10,
		RateLimitBuckets: config.ProbeRateLimitBuckets{
			FreshTokensPerHour:  1,
			RescanTokensPerHour: 1,
		},
	})
	if !b.tryConsume("websocket", nil) {
		t.Fatal("fresh consume failed")
	}
	if b.tryConsume("websocket", nil) {
		t.Fatal("fresh bucket should be exhausted")
	}
	if !b.tryConsume("rescan_15m", nil) {
		t.Fatal("rescan should use separate bucket")
	}
}
