package resource_control

import (
	"context"

	"crypto-sniping-bot/database"
)

// exitEventTypes are event types that MUST never be dropped under any backpressure condition.
// These are the exit-path events that guarantee position management and settlement.
var exitEventTypes = map[string]bool{
	"position_event_exit":   true,
	"execution_replacement": true,
	"execution_result_event": true,
}

// BackpressurePolicy determines whether an event should be dropped under resource pressure.
// Exit-path events are NEVER dropped — this is an invariant enforced here.
type BackpressurePolicy interface {
	// ShouldDrop returns (true, reason) if the event can be safely dropped.
	// Always returns (false, "") for exit-path events.
	ShouldDrop(ctx context.Context, evt *database.Event, queueDepth int64, maxDepth int64) (drop bool, reason string)
}

// DefaultBackpressurePolicy applies a simple queue-depth shed policy.
// Any event that is not on the protected exit-path list is eligible for dropping
// when queueDepth > maxDepth.
type DefaultBackpressurePolicy struct{}

// NewBackpressurePolicy returns the default backpressure policy.
func NewBackpressurePolicy() *DefaultBackpressurePolicy {
	return &DefaultBackpressurePolicy{}
}

// ShouldDrop returns true for droppable events when queue is over capacity.
func (p *DefaultBackpressurePolicy) ShouldDrop(_ context.Context, evt *database.Event, queueDepth, maxDepth int64) (bool, string) {
	if evt == nil {
		return false, ""
	}
	// Exit-path events are protected and never dropped.
	if exitEventTypes[evt.EventType] {
		return false, ""
	}
	if queueDepth > maxDepth {
		return true, "compute_queue_full"
	}
	return false, ""
}

// IsExitEvent returns true if the event type is a protected exit-path event.
func IsExitEvent(eventType string) bool {
	return exitEventTypes[eventType]
}
