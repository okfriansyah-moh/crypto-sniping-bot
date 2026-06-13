---
name: rpc-management
type: skill
description: >
  Multi-endpoint RPC management patterns. Use when implementing or reviewing RPC
  client pools, circuit breakers, endpoint health tracking, rate limiting, fee bump
  retry, and WebSocket/HTTP fallback. A single RPC failure must not halt trading —
  latency is a profit factor and endpoint management is how you protect it.
---

# RPC Management Skill

## Purpose

Enforce correct implementation of the multi-endpoint RPC layer that provides
blockchain access for the execution engine. Endpoint failures must trigger fallback,
not crashes. Latency is directly factored into trade profitability.

**Latency factor:** `EdgeDecayFactor = exp(-decay_rate × latency_ms)`
A stuck RPC → high latency → zero or negative EV → missed trades.

---

## Rules

### Multi-Endpoint Pool Configuration

```yaml
# config/rpc.yaml
rpc:
  endpoints:
    - url: "${RPC_ENDPOINT_1_WS}" # env var — never hardcoded URLs
      type: "websocket"
      priority: 1
      weight: 10
    - url: "${RPC_ENDPOINT_2_HTTP}"
      type: "http"
      priority: 2
      weight: 5
    - url: "${RPC_ENDPOINT_3_HTTP}"
      type: "http"
      priority: 3
      weight: 3
  circuit_breaker:
    failure_threshold: 3 # consecutive failures before degraded
    recovery_window_sec: 30 # wait before trying again
    half_open_max_calls: 1 # trial calls in half-open state
  rate_limiting:
    requests_per_second: 10 # per endpoint
    burst_size: 20
  timeout_ms: 2000
  retry_max: 2
```

### Circuit Breaker (Per Endpoint)

```go
// Circuit breaker states
type CircuitState string
const (
    CircuitClosed   CircuitState = "closed"    // healthy — accepting calls
    CircuitOpen     CircuitState = "open"      // failing — reject calls for window
    CircuitHalfOpen CircuitState = "half_open" // testing — limited trial calls
)

type EndpointCircuit struct {
    URL              string
    State            CircuitState
    ConsecutiveFails int
    LastFailure      time.Time
    mu               sync.Mutex
}

func (c *EndpointCircuit) Allow() bool {
    c.mu.Lock()
    defer c.mu.Unlock()

    switch c.State {
    case CircuitClosed:
        return true
    case CircuitOpen:
        if time.Since(c.LastFailure) > recoveryWindow {
            c.State = CircuitHalfOpen
            return true
        }
        return false
    case CircuitHalfOpen:
        return true  // allow one trial call
    default:
        return false
    }
}

func (c *EndpointCircuit) RecordSuccess() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.ConsecutiveFails = 0
    c.State = CircuitClosed
}

func (c *EndpointCircuit) RecordFailure(threshold int) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.ConsecutiveFails++
    c.LastFailure = time.Now()
    if c.ConsecutiveFails >= threshold {
        c.State = CircuitOpen
        logger.Warn("circuit opened", "url", c.URL, "fails", c.ConsecutiveFails)
    }
}
```

### Endpoint Selection (Priority + Circuit State)

```go
// Select best available endpoint — priority order, skip open circuits
func (p *RPCPool) selectEndpoint() (*Endpoint, error) {
    p.mu.RLock()
    defer p.mu.RUnlock()

    // Sort by priority (ascending — 1 = highest priority)
    for _, ep := range p.endpoints {
        if ep.Circuit.Allow() {
            return ep, nil
        }
    }
    return nil, ErrAllEndpointsDegraded
}

// Execute with automatic fallback
func (p *RPCPool) Call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
    var lastErr error
    for _, ep := range p.endpoints {  // try all in priority order
        if !ep.Circuit.Allow() { continue }

        result, err := ep.call(ctx, method, params)
        if err != nil {
            ep.Circuit.RecordFailure(p.cfg.FailureThreshold)
            lastErr = err
            continue
        }
        ep.Circuit.RecordSuccess()
        return result, nil
    }
    return nil, fmt.Errorf("all endpoints failed: %w", lastErr)
}
```

### WebSocket vs HTTP Fallback

```go
// WebSocket: preferred for new block subscriptions + mempool
// HTTP: fallback for one-off calls when WebSocket is degraded

func (p *RPCPool) subscribeNewBlocks(ctx context.Context, handler func(Block)) {
    wsEndpoint := p.findWebSocketEndpoint()
    if wsEndpoint != nil && wsEndpoint.Circuit.Allow() {
        wsEndpoint.subscribeNewBlocks(ctx, handler)
        return
    }
    // Fallback: HTTP polling
    go p.pollNewBlocks(ctx, handler)
}

func (p *RPCPool) pollNewBlocks(ctx context.Context, handler func(Block)) {
    ticker := time.NewTicker(time.Duration(p.cfg.PollIntervalMs) * time.Millisecond)
    var lastBlock int64
    for {
        select {
        case <-ctx.Done(): return
        case <-ticker.C:
            block, err := p.Call(ctx, "eth_getBlockByNumber", "latest")
            if err != nil { continue }
            if block.Number > lastBlock {
                lastBlock = block.Number
                handler(block)
            }
        }
    }
}
```

### Fee Bump Retry (Same Nonce)

```go
// Fee bump on stuck transaction — MUST use SAME nonce
// Max 2-3 retries, each with +10-20% fee increment
func feeRetry(ctx context.Context, originalTx TxParams, rpcPool *RPCPool, cfg ExecutionConfig) error {
    nonce := originalTx.Nonce  // MUST keep same nonce — replaces in-flight tx

    for attempt := 1; attempt <= cfg.MaxFeeRetries; attempt++ {
        // Bump gas price by fee increment
        newGasPrice := originalTx.GasPrice * (1 + cfg.FeeBumpPct*float64(attempt)/100)

        tx := buildTx(originalTx.To, originalTx.Data, nonce, newGasPrice)
        txHash, err := rpcPool.SendRawTransaction(ctx, tx)
        if err != nil {
            logger.Warn("fee bump failed", "attempt", attempt, "err", err)
            continue
        }

        // Wait for inclusion
        included, err := waitForInclusion(ctx, txHash, cfg.InclusionTimeoutMs, rpcPool)
        if err != nil { continue }
        if included { return nil }
    }
    return ErrMaxFeeRetriesExceeded
}
```

### Rate Limiting (Per Endpoint)

```go
// Per-endpoint token bucket rate limiter
type EndpointLimiter struct {
    limiter *rate.Limiter
}

func NewEndpointLimiter(rps int, burst int) *EndpointLimiter {
    return &EndpointLimiter{
        limiter: rate.NewLimiter(rate.Limit(rps), burst),
    }
}

// Applied before every call
func (e *Endpoint) callWithRateLimit(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
    if err := e.limiter.Wait(ctx); err != nil {
        return nil, fmt.Errorf("rate limit wait: %w", err)
    }
    return e.call(ctx, method, params)
}
```

### Latency Tracking

```go
// Track p50/p99 latency per endpoint — used by probability model
func (e *Endpoint) callTracked(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
    start := time.Now()
    result, err := e.call(ctx, method, params)
    latency := time.Since(start)

    e.metrics.RecordLatency(latency)
    if latency > e.cfg.SlowCallThreshold {
        logger.Warn("slow RPC call", "url", e.URL, "method", method, "latency_ms", latency.Milliseconds())
    }
    return result, err
}
```

### Anti-Patterns

```go
// ❌ Hardcoded RPC URL
client, err := ethclient.Dial("https://mainnet.infura.io/v3/hardcoded-key")  // FORBIDDEN

// ❌ Single endpoint, no fallback
result, err := primaryEndpoint.Call(...)
if err != nil { return err }  // No fallback — single point of failure

// ❌ Fee bump with new nonce
newTx := buildTx(to, data, wallet.GetNextNonce(), bumpedGas)  // Wrong — gets new nonce

// ❌ No circuit breaker
for _, ep := range endpoints {
    result, err := ep.Call(...)  // No circuit state check — hammers failing endpoint
}

// ✅ Correct
result, err := rpcPool.Call(ctx, method, params)  // pool handles fallback, circuit, rate limit
```

---

## Checklist

```
[ ] All RPC URLs come from environment variables — never hardcoded
[ ] Circuit breaker per endpoint: closed → open after N failures
[ ] Recovery window before circuit half-opens
[ ] Priority-ordered endpoint selection
[ ] WebSocket preferred for subscriptions, HTTP as fallback
[ ] Fee retry uses SAME nonce — replaces in-flight transaction
[ ] Max fee retries: 2-3 per config
[ ] Per-endpoint rate limiting (token bucket)
[ ] Latency tracked per endpoint for probability model input
[ ] ErrAllEndpointsDegraded handled gracefully (no panic)
[ ] RPC pool is the ONLY component calling blockchain nodes
[ ] All config values (failure_threshold, timeout_ms, etc.) from config/rpc.yaml
```

---

## References

- Architecture: `docs/reference/architecture.md` § 3.8 (Execution Engine), § 6.2 (RPC)
- Architecture context: `docs/archive/architecture-context/10_execution_engine.md`
- Roadmap: `docs/reference/implementation_roadmap.md` Phase 2.8
- Config: `config/rpc.yaml`
