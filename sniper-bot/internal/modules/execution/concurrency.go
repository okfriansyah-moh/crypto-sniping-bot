package execution

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// ErrSemaphoreTimeout is returned when a goroutine cannot acquire the execution semaphore.
var ErrSemaphoreTimeout = errors.New("execution: concurrency semaphore timeout")

// ExecutionSemaphore is a bounded concurrency gate for in-flight transactions.
// The limit is adaptive: AdjustLimit can raise or lower it within [minLimit, maxLimit].
type ExecutionSemaphore interface {
	// Acquire blocks until a slot is available or the context is cancelled.
	Acquire(ctx context.Context) error
	// Release frees a slot.
	Release()
	// AdjustLimit sets a new concurrency limit, clamped to [minLimit, maxLimit].
	AdjustLimit(newLimit int)
	// CurrentLimit returns the active concurrency limit.
	CurrentLimit() int
}

// semaphoreImpl is the channel-based ExecutionSemaphore implementation.
type semaphoreImpl struct {
	mu       sync.Mutex
	ch       chan struct{}
	limit    atomic.Int64
	minLimit int
	maxLimit int
}

// NewExecutionSemaphore creates an ExecutionSemaphore with an initial capacity.
// initial is clamped to [minLimit, maxLimit].
func NewExecutionSemaphore(initial, minLimit, maxLimit int) ExecutionSemaphore {
	if initial < minLimit {
		initial = minLimit
	}
	if initial > maxLimit {
		initial = maxLimit
	}
	ch := make(chan struct{}, maxLimit) // channel capacity = maxLimit to allow dynamic growth
	// Pre-fill with initial slots.
	for i := 0; i < initial; i++ {
		ch <- struct{}{}
	}
	s := &semaphoreImpl{
		ch:       ch,
		minLimit: minLimit,
		maxLimit: maxLimit,
	}
	s.limit.Store(int64(initial))
	return s
}

// Acquire blocks until a semaphore slot is available or ctx is done.
func (s *semaphoreImpl) Acquire(ctx context.Context) error {
	select {
	case <-s.ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release returns a slot to the semaphore.
func (s *semaphoreImpl) Release() {
	select {
	case s.ch <- struct{}{}:
	default:
		// Channel already full — this can happen if AdjustLimit reduced the limit.
	}
}

// AdjustLimit changes the concurrency limit. Clamped to [minLimit, maxLimit].
// If increasing, adds tokens. If decreasing, the excess tokens drain naturally
// as in-flight requests complete.
func (s *semaphoreImpl) AdjustLimit(newLimit int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if newLimit < s.minLimit {
		newLimit = s.minLimit
	}
	if newLimit > s.maxLimit {
		newLimit = s.maxLimit
	}

	old := int(s.limit.Swap(int64(newLimit)))
	delta := newLimit - old
	if delta > 0 {
		// Increasing: add tokens.
		for i := 0; i < delta; i++ {
			select {
			case s.ch <- struct{}{}:
			default:
			}
		}
	} else if delta < 0 {
		// Decreasing: proactively drain idle tokens so the new limit takes
		// effect immediately rather than waiting for in-flight goroutines to
		// call Release.  We drain as many idle tokens as are both available
		// and in excess of the new limit.
		excess := -delta
		for i := 0; i < excess; i++ {
			select {
			case <-s.ch:
			default:
				// No more idle tokens to drain; remainder will drain on Release.
				i = excess // break
			}
		}
	}
}

// CurrentLimit returns the active concurrency limit.
func (s *semaphoreImpl) CurrentLimit() int {
	return int(s.limit.Load())
}
