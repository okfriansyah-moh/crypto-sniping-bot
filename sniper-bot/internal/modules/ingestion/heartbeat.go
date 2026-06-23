package ingestion

import "time"

// HeartbeatConfig holds WebSocket heartbeat parameters.
type HeartbeatConfig struct {
	IntervalMs int // how often to send a ping
	TimeoutMs  int // how long to wait for a pong before declaring timeout
}

// Heartbeat tracks WebSocket liveness via last-activity timestamps.
type Heartbeat struct {
	cfg      HeartbeatConfig
	lastSeen time.Time
}

// NewHeartbeat creates a Heartbeat initialised to now.
func NewHeartbeat(cfg HeartbeatConfig) *Heartbeat {
	return &Heartbeat{cfg: cfg, lastSeen: time.Now()}
}

// Reset records activity — call on every received message or successful pong.
func (h *Heartbeat) Reset() {
	h.lastSeen = time.Now()
}

// TimedOut returns true if no activity has been seen within the timeout window.
func (h *Heartbeat) TimedOut() bool {
	return time.Since(h.lastSeen) > time.Duration(h.cfg.TimeoutMs)*time.Millisecond
}

// Interval returns the configured ping interval.
func (h *Heartbeat) Interval() time.Duration {
	return time.Duration(h.cfg.IntervalMs) * time.Millisecond
}
