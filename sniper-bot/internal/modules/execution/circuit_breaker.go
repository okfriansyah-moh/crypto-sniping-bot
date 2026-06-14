// Package execution — circuit breaker for per-RPC-endpoint failure tracking.
// The circuit breaker opens after N consecutive errors and halts routing to
// the endpoint until the cooldown expires.
package execution

import (
	"fmt"
	"sync"
	"time"
)

// CircuitState represents the circuit breaker state.
type CircuitState int

const (
	StateClosed   CircuitState = iota // normal operation
	StateOpen                         // circuit open — endpoint unhealthy
	StateHalfOpen                     // cooldown expired; next call is a probe
)

// CircuitBreaker tracks consecutive failure counts per endpoint.
// Thread-safe.
type CircuitBreaker struct {
	mu               sync.Mutex
	endpoints        map[string]*endpointState
	failureThreshold int
	cooldown         time.Duration
}

type endpointState struct {
	consecutiveErrors int
	state             CircuitState
	openedAt          time.Time
}

// NewCircuitBreaker returns a new CircuitBreaker.
// failureThreshold is the number of consecutive errors before opening.
// cooldown is the duration before the circuit transitions to HalfOpen.
func NewCircuitBreaker(failureThreshold int, cooldown time.Duration) *CircuitBreaker {
	if failureThreshold <= 0 {
		failureThreshold = 3
	}
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	return &CircuitBreaker{
		endpoints:        make(map[string]*endpointState),
		failureThreshold: failureThreshold,
		cooldown:         cooldown,
	}
}

// Allow returns true if the endpoint can receive a request.
// Transitions Open→HalfOpen when cooldown has elapsed.
func (cb *CircuitBreaker) Allow(endpoint string) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	s := cb.getOrCreate(endpoint)
	switch s.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(s.openedAt) >= cb.cooldown {
			s.state = StateHalfOpen
			return true
		}
		return false
	case StateHalfOpen:
		return true
	}
	return false
}

// RecordSuccess records a successful call. Resets failure count and closes circuit.
func (cb *CircuitBreaker) RecordSuccess(endpoint string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	s := cb.getOrCreate(endpoint)
	s.consecutiveErrors = 0
	s.state = StateClosed
}

// RecordFailure records a failed call. Opens circuit when threshold is exceeded.
func (cb *CircuitBreaker) RecordFailure(endpoint string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	s := cb.getOrCreate(endpoint)
	s.consecutiveErrors++
	if s.consecutiveErrors >= cb.failureThreshold && s.state == StateClosed {
		s.state = StateOpen
		s.openedAt = time.Now()
	}
}

// State returns the current CircuitState for an endpoint.
func (cb *CircuitBreaker) State(endpoint string) CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.getOrCreate(endpoint).state
}

// HealthyEndpoint returns the first healthy endpoint from the list.
// Returns an error if all endpoints have open circuits.
func (cb *CircuitBreaker) HealthyEndpoint(endpoints []string) (string, error) {
	for _, ep := range endpoints {
		if cb.Allow(ep) {
			return ep, nil
		}
	}
	return "", fmt.Errorf("circuit_breaker: all %d endpoints unhealthy", len(endpoints))
}

func (cb *CircuitBreaker) getOrCreate(endpoint string) *endpointState {
	s, ok := cb.endpoints[endpoint]
	if !ok {
		s = &endpointState{state: StateClosed}
		cb.endpoints[endpoint] = s
	}
	return s
}
