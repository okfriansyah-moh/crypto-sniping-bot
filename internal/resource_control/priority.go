// Package resource_control provides priority computation for event bus ordering.
// Workers call ComputePriority before inserting events so exit-path events
// are always processed before entry-path events.
package resource_control

import (
	"time"

	"crypto-sniping-bot/internal/app/config"
)

// PRIORITY_EXIT is the minimum priority value for exit-path events.
// Events with this priority are never dropped by TTL-based expiry.
const PRIORITY_EXIT int32 = 900

// ComputePriority returns the bus priority for an event.
// priority = base_weight + clamp((expiresAt - now) / maxTTL, 0, 1) * urgencyCoef
//
// For exit-path events (position_event exit, execution_replacement) the base
// weight is already ≥ PRIORITY_EXIT so they are never reordered below entries.
//
// eventType must be one of the keys in config.EventPriorityWeights.
// isExit is true when the event carries a terminal payload (position Status=exited
// or execution_replacement).
// expiresAt may be zero to skip urgency bonus.
func ComputePriority(
	eventType string,
	isExit bool,
	expiresAt time.Time,
	now time.Time,
	weights config.EventPriorityWeights,
) int32 {
	base := baseWeight(eventType, isExit, weights)

	if expiresAt.IsZero() || expiresAt.Before(now) {
		return base
	}

	const maxTTLSeconds = 60.0
	const urgencyCoef = 50.0

	remaining := expiresAt.Sub(now).Seconds()
	ratio := remaining / maxTTLSeconds
	if ratio > 1 {
		ratio = 1
	}
	if ratio < 0 {
		ratio = 0
	}

	urgency := int32(ratio * urgencyCoef)
	return base + urgency
}

// baseWeight returns the configured base priority for an event type.
func baseWeight(eventType string, isExit bool, w config.EventPriorityWeights) int32 {
	if eventType == "execution_replacement" {
		return w.ExecutionReplacement
	}
	if eventType == "position_event" {
		if isExit {
			return w.PositionEventExit
		}
		return w.PositionEventOpen
	}

	weights := map[string]int32{
		"allocation_event":      w.AllocationEvent,
		"validated_edge_event":  w.ValidatedEdgeEvent,
		"edge_event":            w.EdgeEvent,
		"feature_event":         w.FeatureEvent,
		"data_quality_event":    w.DataQualityEvent,
		"market_data_event":     w.MarketDataEvent,
		"adjustment_event":      w.AdjustmentEvent,
	}
	if v, ok := weights[eventType]; ok {
		return v
	}
	return 0
}

// DefaultWeights returns safe default priority weights when config is absent.
func DefaultWeights() config.EventPriorityWeights {
	return config.EventPriorityWeights{
		PositionEventExit:    1000,
		ExecutionReplacement: 900,
		PositionEventOpen:    500,
		AllocationEvent:      400,
		ValidatedEdgeEvent:   300,
		EdgeEvent:            200,
		FeatureEvent:         150,
		DataQualityEvent:     120,
		MarketDataEvent:      100,
		AdjustmentEvent:      50,
	}
}
