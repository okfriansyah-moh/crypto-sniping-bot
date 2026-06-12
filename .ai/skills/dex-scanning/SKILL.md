# DEX Scanning Skill

## Purpose

Enforce correct ingestion patterns for on-chain DEX events. This is the system's
entry point — `MarketDataDTO` quality here determines DataQuality, Edge, and ultimately
profit downstream. Garbage in → garbage PnL.

**Core invariant:** `Profit = Edge × ... × DataQuality × ...`
Layer 0 sets `DataQuality` ceiling. A missed pool or malformed event cannot be recovered.

---

## Rules

### Event Source Priority

| Source                          | Latency    | Reliability | Use For                              |
| ------------------------------- | ---------- | ----------- | ------------------------------------ |
| WebSocket RPC (`eth_subscribe`) | ~50–200ms  | High        | Primary: new blocks, pending tx      |
| DEXScreener API (polling)       | 500ms–2s   | Medium      | Supplementary: discovery, enrichment |
| HTTP JSON-RPC fallback          | ~200–500ms | Medium      | Backup when WS fails                 |

**Rule:** WebSocket subscription is primary. DEXScreener is supplementary — never the
sole source. DEXScreener data is enrichment, not authoritative state.

### MarketDataDTO Production

Every pool event MUST produce a `MarketDataDTO` with all fields populated:

```go
// Required fields — adapter rejects writes with zero values
MarketDataDTO{
    EventID:         SHA256(chain + tx_hash + log_index)[:16],  // content-addressable
    Chain:           "eth-uniswap-v2",                          // market ID
    TxHash:          "0x...",                                    // lowercase hex
    BlockNumber:     uint64,                                     // never 0
    LogIndex:        uint32,
    TokenAddress:    EIP55Checksum(address),                    // checksummed
    PairAddress:     EIP55Checksum(address),
    LiquidityUSD:    float64,                                   // ≥ 0.0
    PriceUSD:        float64,                                   // > 0.0
    Timestamp:       ISO8601UTC,                                // "2026-04-24T10:00:00Z"
    TraceID:         generateTraceID(),                         // fresh 16 hex chars (Layer 0 only)
    CorrelationID:   generateCorrelationID(),                   // fresh 16 hex chars (Layer 0 only)
    CausationID:     "",                                        // MUST be empty string in Layer 0
    VersionID:       activeStrategyVersion.VersionID,
}
```

**ID Rule:** `EventID = SHA256(chain || tx_hash || log_index)[:16]`
Replay of the same block produces the same EventID — no duplicates.

### New Pool Detection (Primary Edge)

```
Pool is "new" when:
  block_number_first_seen ≤ current_block - config.new_pool_window_blocks
  AND liquidity_added_at > current_time - config.new_pool_max_age_seconds
```

Config-driven thresholds (never hardcode):

```yaml
# config/pipeline.yaml
ingestion:
  new_pool_window_blocks: 50 # ~10 min on ETH
  new_pool_max_age_seconds: 600 # 10 minutes
  min_initial_liquidity_usd: 1000.0
  dexscreener_enrich: true
```

### DEXScreener Integration Pattern

DEXScreener is enrichment — call it AFTER detecting a new pool, not before.

```go
// Correct: detect on-chain first, enrich with DEXScreener
func enrichWithDEXScreener(ctx context.Context, pair PairAddress, cfg Config) (*DEXScreenerData, error) {
    if !cfg.DEXScreenerEnabled {
        return nil, nil  // graceful skip
    }
    url := fmt.Sprintf("https://api.dexscreener.com/latest/dex/pairs/%s/%s", chain, pair)
    // Use parameterized URLs only — no user input in URL
    resp, err := httpClient.GetWithTimeout(ctx, url, cfg.DEXScreenerTimeoutMs)
    if err != nil {
        // Non-fatal: log and continue without enrichment
        logger.Warn("dexscreener enrichment failed", "pair", pair, "error", err)
        return nil, nil
    }
    return parseDEXScreenerResponse(resp)
}
```

**Rate limit awareness:** DEXScreener free tier = 300 req/min. Use token bucket.
**Never block ingestion** on DEXScreener failure — it's optional enrichment.

### RPC Subscription Pattern

```go
// Correct: subscribe to new blocks + filter for pair creation events
func subscribeNewPools(ctx context.Context, client *ethclient.Client) (<-chan *types.Log, error) {
    query := ethereum.FilterQuery{
        Topics: [][]common.Hash{
            {crypto.Keccak256Hash([]byte("PairCreated(address,address,address,uint256)"))},
        },
        Addresses: []common.Address{factoryAddress},
    }
    logs := make(chan types.Log, 100)  // buffered — never block RPC goroutine
    sub, err := client.SubscribeFilterLogs(ctx, query, logs)
    if err != nil {
        return nil, fmt.Errorf("subscribe filter logs: %w", err)
    }
    // ... handle sub.Err() in goroutine
    return logs, nil
}
```

### Anti-Patterns

```go
// ❌ Using DEXScreener as primary source
marketData := fetchFromDEXScreener(pair)  // Wrong — DEXScreener is supplementary

// ❌ Hardcoded contract addresses
factoryAddress := "0x5C69bEe701ef814a2B6a3EDD4B1652CB9cc5aA6f"  // Wrong — config-driven

// ❌ Non-content-addressable ID
eventID := uuid.New().String()  // Wrong — must be SHA256(content)[:16]

// ❌ CausationID set in Layer 0
dto.CausationID = parentEvent.EventID  // Wrong — must be "" in Layer 0

// ❌ Blocking ingestion on enrichment
dexData, err := dexscreener.Fetch(pair)
if err != nil { return err }  // Wrong — DEXScreener failure must NOT block

// ✅ Correct
eventID := sha256content(chain + txHash + strconv.Itoa(int(logIndex)))[:16]
dto := contracts.MarketDataDTO{
    EventID:       eventID,
    CausationID:   "",  // Layer 0 root event
    // ...
}
```

---

## DEX Factory Addresses (Reference)

| DEX            | Chain    | Factory Address                              |
| -------------- | -------- | -------------------------------------------- |
| Uniswap V2     | Ethereum | `0x5C69bEe701ef814a2B6a3EDD4B1652CB9cc5aA6f` |
| Uniswap V3     | Ethereum | `0x1F98431c8aD98523631AE4a59f267346ea31F984` |
| PancakeSwap V2 | BSC      | `0xcA143Ce32Fe78f1f7019d7d551a6402fC5350c73` |
| PancakeSwap V3 | BSC      | `0x0BFbCF9fa4f9C56B0F40a671Ad40E0805A091865` |

**Store these in config, not code.** Each market has its own isolated pipeline instance.

---

## Checklist

```
[ ] EventID is SHA256(chain+tx_hash+log_index)[:16]
[ ] CausationID is empty string "" in Layer 0
[ ] TraceID and CorrelationID are freshly generated in Layer 0
[ ] VersionID references active StrategyVersion
[ ] DEXScreener failure does NOT block ingestion
[ ] Factory/router addresses are config-driven, never hardcoded
[ ] New pool detection uses config.new_pool_max_age_seconds
[ ] RPC subscription has buffered channel (never blocks RPC goroutine)
[ ] Token address is EIP-55 checksummed
[ ] MarketDataDTO INSERT uses ON CONFLICT DO NOTHING
[ ] Rate limiting applied to DEXScreener API calls
```

---

## References

- Architecture: `docs/architecture.md` § 3.0 (Detection & Ingestion)
- DTO spec: `docs/dto_contracts.md` § 3.1 (MarketDataDTO)
- Roadmap: `docs/implementation_roadmap.md` Phase 1
- Config: `config/pipeline.yaml` → `ingestion` section