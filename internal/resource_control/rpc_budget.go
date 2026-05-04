package resource_control

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrBudgetExhausted is returned when an RPC endpoint has no remaining token-bucket capacity.
var ErrBudgetExhausted = errors.New("resource_control: rpc budget exhausted")

// RPCBudget enforces a token-bucket rate limit per RPC endpoint.
// Acquire blocks until a token is available or the context is cancelled.
// Release must be called after each RPC call completes.
type RPCBudget interface {
	Acquire(ctx context.Context, endpoint string) error
	Release(endpoint string)
}

// tokenBucket is a simple token bucket for one endpoint.
type tokenBucket struct {
	mu       sync.Mutex
	tokens   int
	capacity int
	rate     int // tokens added per second
	lastFill time.Time
	waitMs   int
}

func newTokenBucket(rate, burst, waitMs int) *tokenBucket {
	return &tokenBucket{
		tokens:   burst,
		capacity: burst,
		rate:     rate,
		lastFill: time.Now(),
		waitMs:   waitMs,
	}
}

// fill refills tokens based on elapsed time. Must be called under lock.
func (b *tokenBucket) fill(now time.Time) {
	elapsed := now.Sub(b.lastFill).Seconds()
	added := int(elapsed * float64(b.rate))
	if added > 0 {
		b.tokens += added
		if b.tokens > b.capacity {
			b.tokens = b.capacity
		}
		b.lastFill = now
	}
}

// tryAcquire attempts to consume one token. Returns true if successful.
func (b *tokenBucket) tryAcquire() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fill(time.Now())
	if b.tokens <= 0 {
		return false
	}
	b.tokens--
	return true
}

// RPCBudgetImpl is the concrete token-bucket implementation.
type RPCBudgetImpl struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	rate    int // per-endpoint rate
	burst   int
	waitMs  int
}

// NewRPCBudget creates a new token-bucket RPCBudget.
// rate is tokens/second; burst is the bucket capacity; waitMs is how long
// to wait before returning ErrBudgetExhausted.
func NewRPCBudget(rate, burst, waitMs int) *RPCBudgetImpl {
	return &RPCBudgetImpl{
		buckets: make(map[string]*tokenBucket),
		rate:    rate,
		burst:   burst,
		waitMs:  waitMs,
	}
}

func (r *RPCBudgetImpl) bucket(endpoint string) *tokenBucket {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, ok := r.buckets[endpoint]; ok {
		return b
	}
	b := newTokenBucket(r.rate, r.burst, r.waitMs)
	r.buckets[endpoint] = b
	return b
}

// Acquire blocks for up to waitMs milliseconds then returns ErrBudgetExhausted.
func (r *RPCBudgetImpl) Acquire(ctx context.Context, endpoint string) error {
	b := r.bucket(endpoint)
	deadline := time.Now().Add(time.Duration(r.waitMs) * time.Millisecond)
	for {
		if b.tryAcquire() {
			return nil
		}
		if time.Now().After(deadline) {
			return ErrBudgetExhausted
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// Release is a no-op for the token bucket model — tokens refill by time.
func (r *RPCBudgetImpl) Release(_ string) {}
