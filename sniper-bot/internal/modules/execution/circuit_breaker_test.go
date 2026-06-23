package execution_test

import (
	"math/big"
	"testing"
	"time"

	"crypto-sniping-bot/sniper-bot/internal/modules/execution"
)

func TestCircuitBreaker_AllowClosed(t *testing.T) {
	cb := execution.NewCircuitBreaker(3, 30*time.Second)
	if !cb.Allow("ep1") {
		t.Fatal("expected closed circuit to allow")
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := execution.NewCircuitBreaker(3, 30*time.Second)
	cb.RecordFailure("ep1")
	cb.RecordFailure("ep1")
	cb.RecordFailure("ep1")
	if cb.Allow("ep1") {
		t.Fatal("expected open circuit to block")
	}
	if cb.State("ep1") != execution.StateOpen {
		t.Fatalf("expected StateOpen, got %v", cb.State("ep1"))
	}
}

func TestCircuitBreaker_ResetsOnSuccess(t *testing.T) {
	cb := execution.NewCircuitBreaker(2, 30*time.Second)
	cb.RecordFailure("ep1")
	cb.RecordFailure("ep1")
	if cb.Allow("ep1") {
		t.Fatal("expected blocked after threshold")
	}
	cb.RecordSuccess("ep1")
	if !cb.Allow("ep1") {
		t.Fatal("expected circuit to close after success")
	}
}

func TestCircuitBreaker_HalfOpenAfterCooldown(t *testing.T) {
	cb := execution.NewCircuitBreaker(1, 10*time.Millisecond)
	cb.RecordFailure("ep1")
	// Immediately after — still open.
	if cb.Allow("ep1") {
		t.Fatal("expected open circuit to block immediately")
	}
	time.Sleep(20 * time.Millisecond)
	// After cooldown — transitions to HalfOpen.
	if !cb.Allow("ep1") {
		t.Fatal("expected half-open circuit to allow probe")
	}
	if cb.State("ep1") != execution.StateHalfOpen {
		t.Fatalf("expected StateHalfOpen, got %v", cb.State("ep1"))
	}
}

func TestCircuitBreaker_HealthyEndpoint(t *testing.T) {
	cb := execution.NewCircuitBreaker(1, 30*time.Second)
	cb.RecordFailure("ep1")
	ep, err := cb.HealthyEndpoint([]string{"ep1", "ep2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ep != "ep2" {
		t.Fatalf("expected ep2, got %s", ep)
	}
}

func TestCircuitBreaker_AllUnhealthy(t *testing.T) {
	cb := execution.NewCircuitBreaker(1, 30*time.Second)
	cb.RecordFailure("ep1")
	cb.RecordFailure("ep2")
	_, err := cb.HealthyEndpoint([]string{"ep1", "ep2"})
	if err == nil {
		t.Fatal("expected error when all endpoints unhealthy")
	}
}

func TestBumpGasPrice_Normal(t *testing.T) {
	orig := big.NewInt(100)
	bumped := execution.BumpGasPrice(orig, 15)
	expected := big.NewInt(115)
	if bumped.Cmp(expected) != 0 {
		t.Fatalf("expected %s, got %s", expected.String(), bumped.String())
	}
}

func TestBumpGasPrice_Zero(t *testing.T) {
	result := execution.BumpGasPrice(big.NewInt(0), 15)
	if result.Sign() != 0 {
		t.Fatalf("expected zero for zero input, got %s", result.String())
	}
}

func TestBumpGasPrice_Nil(t *testing.T) {
	result := execution.BumpGasPrice(nil, 15)
	if result.Sign() != 0 {
		t.Fatalf("expected zero for nil input, got %s", result.String())
	}
}
