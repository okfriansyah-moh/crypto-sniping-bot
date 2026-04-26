package resource_control

import (
	"errors"
	"sync/atomic"
)

// ErrQueueFull is returned when the compute queue is at maximum depth.
var ErrQueueFull = errors.New("resource_control: compute queue full")

// ComputeBudget tracks the queue depth and enforces a maximum.
// Exit-path events are never dropped regardless of queue depth.
type ComputeBudget interface {
	// Enqueue increments the queue counter. Returns ErrQueueFull if max is reached.
	// isExit must be true for exit-path events — they bypass the cap.
	Enqueue(isExit bool) error
	// Dequeue decrements the queue counter.
	Dequeue()
	// Depth returns the current queue depth.
	Depth() int64
}

// ComputeBudgetImpl is the atomic counter implementation.
type ComputeBudgetImpl struct {
	depth    atomic.Int64
	maxDepth int64
}

// NewComputeBudget creates a ComputeBudget with the given maximum queue depth.
func NewComputeBudget(maxDepth int) *ComputeBudgetImpl {
	return &ComputeBudgetImpl{maxDepth: int64(maxDepth)}
}

// Enqueue attempts to claim a compute slot.
// Exit events always succeed; entry events fail when queue >= max.
func (c *ComputeBudgetImpl) Enqueue(isExit bool) error {
	cur := c.depth.Add(1)
	if !isExit && cur > c.maxDepth {
		c.depth.Add(-1)
		return ErrQueueFull
	}
	return nil
}

// Dequeue releases a compute slot.
func (c *ComputeBudgetImpl) Dequeue() {
	c.depth.Add(-1)
}

// Depth returns the current queue depth.
func (c *ComputeBudgetImpl) Depth() int64 {
	return c.depth.Load()
}
