package execution

import (
	"testing"

	"crypto-sniping-bot/internal/app/config"
)

func TestPrivateRPCRouterEmptyConfig(t *testing.T) {
	r := NewPrivateRPCRouter(nil)
	if r.Route(1_000_000) {
		t.Fatal("nil cfg must not route private")
	}
}

func TestPrivateRPCRouterThreshold(t *testing.T) {
	cfg := &config.ExecutionConfig{
		PrivateRouteThresholdUsd: 5_000,
		PrivateEndpoints:         []string{"https://relay-1", "https://relay-2"},
	}
	r := NewPrivateRPCRouter(cfg)
	if r.Route(1_000) {
		t.Fatal("below threshold must not route private")
	}
	if !r.Route(10_000) {
		t.Fatal("above threshold must route private")
	}
	eps := r.Endpoints()
	if len(eps) != 2 || eps[0] != "https://relay-1" {
		t.Fatalf("endpoints not preserved: %v", eps)
	}
}
