# DTO Contracts — Project-Specific

> **Canonical DTO registry.** All data crossing module boundaries in the deterministic event-driven sniper system uses these immutable DTOs from `contracts/`. No module may define its own DTOs. No module may pass raw maps, primitives, or untyped JSON across boundaries.

---

## 1. DTO Design Rules

### Structure

- All DTOs are **immutable** Go structs — no setters, no mutating methods, no post-construction logic
- All fields **exported** and typed; zero `interface{}` / `any`
- DTOs live exclusively in `contracts/` — one file per logical DTO group
- No methods on DTOs (pure data)

### Serialization

- JSON-serializable only via encoding/json — `string`, `int64`, `uint64`, `float64`, `bool`, nested DTOs, slices of DTOs
- **Forbidden types:** `time.Time` (use ISO 8601 `string`), `big.Int` (use decimal `string`), `[]byte`, `map[...]...` across module boundaries
- Canonical JSON (sorted keys, no whitespace) required for ID derivation

### IDs

- All content-addressable IDs = `SHA256(canonical_content_signature)[:16]` → 16 lowercase hex characters
- Deterministic: same content → same ID
- Examples: `event_id`, `token_lifecycle_id`, `execution_id`, `position_id`, `record_id`, `strategy_version_id`

### Mandatory Correlation Fields (§ 4.8 — Traceability)

Every DTO that flows through the event bus MUST include:

| Field           | Type     | Rule                                                                                                                             |
| --------------- | -------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `TraceID`       | `string` | 16 hex chars — identifies the complete journey of one token                                                                      |
| `CorrelationID` | `string` | 16 hex chars — identifies one pipeline execution attempt                                                                         |
| `CausationID`   | `string` | 16 hex chars — the `event_id` of the event that produced this DTO. MUST be empty string `""` **only** for Layer 0 (root events). |
| `VersionID`     | `string` | 16 hex chars — the active `strategy_version_id` at write time                                                                    |

The adapter rejects any event write missing these fields (except `CausationID` for Layer 0 root events).

---

## 2. Versioning Rules

- **Additive only** — new fields may be added with Go zero-value defaults
- **Never remove or rename** fields
- If a field must be replaced, add the new field and mark the old deprecated in comments; never delete
- DTO changes must merge to `main` **before** any module change that depends on them
- DTO struct changes bump `config.schema_version`, which propagates into `StrategyVersion`

---

## 3. DTO Registry

### 3.1 `MarketDataDTO` — Layer 0 (Ingestion)

Raw normalized blockchain event. Emitted by `internal/modules/ingestion/`.

```go
package contracts

type MarketDataDTO struct {
    EventID       string  `json:"event_id"`
    TraceID       string  `json:"trace_id"`
    CorrelationID string  `json:"correlation_id"`
    CausationID   string  `json:"causation_id"`    // "" — Layer 0 is root
    VersionID     string  `json:"version_id"`

    Chain         string  `json:"chain"`            // eth | bsc | solana
    Market        string  `json:"market"`           // e.g., "eth-uniswap-v2"
    BlockNumber   uint64  `json:"block_number"`
    BlockHash     string  `json:"block_hash"`       // 0x-prefixed
    TxHash        string  `json:"tx_hash"`
    LogIndex      uint32  `json:"log_index"`

    EventTopic    string  `json:"event_topic"`      // PairCreated | Mint | Swap | Burn
    PoolAddress   string  `json:"pool_address"`     // EIP-55 checksummed
    TokenAddress  string  `json:"token_address"`    // target token side (non-base)
    BaseAddress   string  `json:"base_address"`     // WETH/USDT/USDC/BNB

    Token0Address string  `json:"token0_address"`
    Token1Address string  `json:"token1_address"`
    Amount0Raw    string  `json:"amount0_raw"`      // decimal string, no scientific notation
    Amount1Raw    string  `json:"amount1_raw"`
    ReserveBaseRaw  string `json:"reserve_base_raw"`
    ReserveTokenRaw string `json:"reserve_token_raw"`

    BlockTimestamp string `json:"block_timestamp"`  // ISO 8601 UTC
    IngestedAt     string `json:"ingested_at"`      // ISO 8601 UTC

    RpcEndpoint     string `json:"rpc_endpoint"`
    Transport       string `json:"transport"`       // websocket | polling | gap_recovery
    ConfirmationDepth uint32 `json:"confirmation_depth"`
    Reorged         bool   `json:"reorged"`

    // Production hardening (architecture § 4.10.A.3) — additive, required.
    // Canonical encoding (byte-wise sort == semantic sort):
    //   EVM:    8B BE block_number || 32B tx_hash bytes || 4B BE log_index
    //   Solana: 8B BE slot          || 64B signature   || 4B BE instruction_index
    // Workers MUST process events in strict ascending OrderingKey per partition.
    OrderingKey []byte `json:"ordering_key"`
}
```

- **Source file:** `contracts/market_data.go`
- **Producer:** `internal/modules/ingestion` (EVM) and `internal/modules/ingestion_solana` (Phase 7, Solana)
- **Consumer:** `internal/modules/data_quality`
- **ID rule:** `EventID = SHA256(chain||tx_hash||log_index)[:16]` (EVM) or `SHA256("solana"||signature||instruction_index)[:16]` (Solana). Both forms are content-addressable hashes over the chain-natural ordering keys; collisions across chains are statistically negligible because `chain` is part of the hash.

> **Chain-agnostic invariant.** All DTOs are chain-agnostic. The `Chain` field is a free-form string identifier (`eth | bsc | solana`); per-chain interpretation lives in the **consuming module**, not in the DTO. Adding a new chain (e.g. Solana via Phase 7) requires **zero schema changes** to any DTO — only a new value in the `Chain` enumeration and new chain-specific producer/consumer modules. Layers 1–7, 9, 10 MUST never branch on `Chain`. See `docs/architecture.md` § 3.11.

> **Ordering invariant.** `OrderingKey` is the **single** authoritative event-ordering field across the entire pipeline. The `events` table is read with `ORDER BY logical_order_key ASC` — never `created_at`. See `docs/architecture.md` § 4.10.A for the full contract, encoding rules, and the partitioning strategy that pairs with it.

---

### 3.2 `DataQualityDTO` — Layer 1

Pass/reject decision with risk attribution. Emitted after static and heuristic checks.

```go
type DataQualityDTO struct {
    EventID       string  `json:"event_id"`
    TraceID       string  `json:"trace_id"`
    CorrelationID string  `json:"correlation_id"`
    CausationID   string  `json:"causation_id"`
    VersionID     string  `json:"version_id"`

    TokenLifecycleID string `json:"token_lifecycle_id"`
    TokenAddress     string `json:"token_address"`
    Chain            string `json:"chain"`

    Decision   string  `json:"decision"`           // PASS | REJECT | RISKY_PASS
    RiskScore  float64 `json:"risk_score"`         // [0.0, 1.0] — higher = riskier

    IsHoneypot     bool `json:"is_honeypot"`
    IsFakeLiquidity bool `json:"is_fake_liquidity"`
    IsWashTrading  bool `json:"is_wash_trading"`
    IsRugRisk      bool `json:"is_rug_risk"`
    IsTaxAnomaly   bool `json:"is_tax_anomaly"`

    BuyTaxBps     int32 `json:"buy_tax_bps"`       // 0–10000
    SellTaxBps    int32 `json:"sell_tax_bps"`
    LpLocked      bool  `json:"lp_locked"`
    LpHolderCount int32 `json:"lp_holder_count"`
    ContractVerified bool `json:"contract_verified"`

    RejectReasons []string `json:"reject_reasons"` // enum codes; empty when PASS
    EvaluatedAt   string   `json:"evaluated_at"`   // ISO 8601
}
```

- **Source file:** `contracts/data_quality.go`
- **Producer:** `internal/modules/data_quality`
- **Consumer:** `internal/modules/features` (PASS / RISKY_PASS only)

---

### 3.3 `FeatureDTO` — Layer 2

Normalized feature vector with per-feature confidence.

```go
type FeatureDTO struct {
    EventID       string  `json:"event_id"`
    TraceID       string  `json:"trace_id"`
    CorrelationID string  `json:"correlation_id"`
    CausationID   string  `json:"causation_id"`
    VersionID     string  `json:"version_id"`

    TokenLifecycleID string `json:"token_lifecycle_id"`
    TokenAddress     string `json:"token_address"`

    // Normalized [0.0, 1.0] features
    LiquidityScore       float64 `json:"liquidity_score"`
    TxVelocityScore      float64 `json:"tx_velocity_score"`
    HolderDistribution   float64 `json:"holder_distribution"`
    WalletEntropy        float64 `json:"wallet_entropy"`
    ContractSafety       float64 `json:"contract_safety"`
    TokenAge             float64 `json:"token_age"`
    VolumeMomentum       float64 `json:"volume_momentum"`
    PriceMomentum        float64 `json:"price_momentum"`

    // Raw reference values (for audit / learning)
    LiquidityUsdRaw      float64 `json:"liquidity_usd_raw"`
    TxVelocity30sRaw     float64 `json:"tx_velocity_30s_raw"`
    HolderCountRaw       int64   `json:"holder_count_raw"`
    TokenAgeSecondsRaw   int64   `json:"token_age_seconds_raw"`

    // Per-feature confidence [0.0, 1.0]
    Confidence FeatureConfidence `json:"confidence"`

    ExtractedAt string `json:"extracted_at"`
}

type FeatureConfidence struct {
    LiquidityScore     float64 `json:"liquidity_score"`
    TxVelocityScore    float64 `json:"tx_velocity_score"`
    HolderDistribution float64 `json:"holder_distribution"`
    WalletEntropy      float64 `json:"wallet_entropy"`
    ContractSafety     float64 `json:"contract_safety"`
    TokenAge           float64 `json:"token_age"`
    VolumeMomentum     float64 `json:"volume_momentum"`
    PriceMomentum      float64 `json:"price_momentum"`
}
```

- **Source file:** `contracts/feature.go`
- **Producer:** `internal/modules/features`
- **Consumer:** `internal/modules/edge`

---

### 3.4 `EdgeDTO` — Layer 3

Raw trading edge, pre-validation.

```go
type EdgeDTO struct {
    EventID       string  `json:"event_id"`
    TraceID       string  `json:"trace_id"`
    CorrelationID string  `json:"correlation_id"`
    CausationID   string  `json:"causation_id"`
    VersionID     string  `json:"version_id"`

    TokenLifecycleID string `json:"token_lifecycle_id"`
    TokenAddress     string `json:"token_address"`

    EdgeType      string  `json:"edge_type"`          // NEW_LAUNCH | MOMENTUM | WALLET_SURGE
    EdgeStrength  float64 `json:"edge_strength"`      // [0.0, 1.0]
    EdgeConfidence float64 `json:"edge_confidence"`   // [0.0, 1.0]
    MomentumScore float64 `json:"momentum_score"`     // [0.0, 1.0]
    ThresholdApplied float64 `json:"threshold_applied"`
    DetectedAt    string  `json:"detected_at"`
}
```

- **Source file:** `contracts/edge.go`
- **Producer:** `internal/modules/edge`
- **Consumer:** `internal/modules/validation`

---

### 3.5 `ProbabilityEstimateDTO`, `SlippageEstimateDTO`, `LatencyProfileDTO` — Layer 4

Model outputs for validation.

```go
type ProbabilityEstimateDTO struct {
    EventID       string  `json:"event_id"`
    TraceID       string  `json:"trace_id"`
    CorrelationID string  `json:"correlation_id"`
    CausationID   string  `json:"causation_id"`
    VersionID     string  `json:"version_id"`

    TokenLifecycleID string  `json:"token_lifecycle_id"`
    Probability      float64 `json:"probability"`       // [0.0, 1.0]
    Calibration      float64 `json:"calibration"`       // Brier-style [0.0, 1.0]
    ModelVersionID   string  `json:"model_version_id"`
    EstimatedAt      string  `json:"estimated_at"`
}

type SlippageEstimateDTO struct {
    EventID       string  `json:"event_id"`
    TraceID       string  `json:"trace_id"`
    CorrelationID string  `json:"correlation_id"`
    CausationID   string  `json:"causation_id"`
    VersionID     string  `json:"version_id"`

    TokenLifecycleID  string  `json:"token_lifecycle_id"`
    ExpectedP50Bps    int32   `json:"expected_p50_bps"`
    ExpectedP95Bps    int32   `json:"expected_p95_bps"`
    ModelVersionID    string  `json:"model_version_id"`
    EstimatedAt       string  `json:"estimated_at"`
}

type LatencyProfileDTO struct {
    EventID       string  `json:"event_id"`
    TraceID       string  `json:"trace_id"`
    CorrelationID string  `json:"correlation_id"`
    CausationID   string  `json:"causation_id"`
    VersionID     string  `json:"version_id"`

    Chain             string `json:"chain"`
    ExpectedP50Ms     int32  `json:"expected_p50_ms"`
    ExpectedP95Ms     int32  `json:"expected_p95_ms"`
    WindowSizeSeconds int32  `json:"window_size_seconds"`
    EstimatedAt       string `json:"estimated_at"`
}
```

- **Source file:** `contracts/probability.go`, `contracts/slippage.go`, `contracts/latency.go`
- **Producer:** `internal/modules/models`
- **Consumer:** `internal/modules/validation`

---

### 3.6 `ValidatedEdgeDTO` — Layer 5

Edge after EV gate.

```go
type ValidatedEdgeDTO struct {
    EventID       string  `json:"event_id"`
    TraceID       string  `json:"trace_id"`
    CorrelationID string  `json:"correlation_id"`
    CausationID   string  `json:"causation_id"`
    VersionID     string  `json:"version_id"`

    TokenLifecycleID string  `json:"token_lifecycle_id"`
    TokenAddress     string  `json:"token_address"`

    Decision              string  `json:"decision"`                // ACCEPT | REJECT
    ExpectedValueBps      int32   `json:"expected_value_bps"`
    ExpectedGainBps       int32   `json:"expected_gain_bps"`
    ExpectedLossBps       int32   `json:"expected_loss_bps"`
    FixedCostsBps         int32   `json:"fixed_costs_bps"`
    ProbabilityUsed       float64 `json:"probability_used"`
    SlippageP95BpsUsed    int32   `json:"slippage_p95_bps_used"`
    EvThresholdApplied    int32   `json:"ev_threshold_applied"`
    RejectReason          string  `json:"reject_reason"`            // empty if ACCEPT
    ValidatedAt           string  `json:"validated_at"`
}
```

- **Source file:** `contracts/validated_edge.go`
- **Producer:** `internal/modules/validation`
- **Consumer:** `internal/modules/selection` (ACCEPT only)

---

### 3.7 `SelectionOutputDTO` — Layer 6

Top-K selection result.

```go
type SelectionOutputDTO struct {
    EventID       string  `json:"event_id"`
    TraceID       string  `json:"trace_id"`
    CorrelationID string  `json:"correlation_id"`
    CausationID   string  `json:"causation_id"`
    VersionID     string  `json:"version_id"`

    TokenLifecycleID string  `json:"token_lifecycle_id"`
    TokenAddress     string  `json:"token_address"`

    Selected         bool    `json:"selected"`
    Rank             int32   `json:"rank"`             // 1-based
    CombinedScore    float64 `json:"combined_score"`   // edge × prob × confidence
    DiversityBucket  string  `json:"diversity_bucket"`
    IsExploration    bool    `json:"is_exploration"`   // explore-band pick
    RejectReason     string  `json:"reject_reason"`    // empty if Selected
    SelectedAt       string  `json:"selected_at"`
}
```

- **Source file:** `contracts/selection.go`
- **Producer:** `internal/modules/selection`
- **Consumer:** `internal/modules/capital`

---

### 3.8 `AllocationDTO` — Layer 7

Capital sizing decision.

```go
type AllocationDTO struct {
    EventID       string  `json:"event_id"`
    TraceID       string  `json:"trace_id"`
    CorrelationID string  `json:"correlation_id"`
    CausationID   string  `json:"causation_id"`
    VersionID     string  `json:"version_id"`

    TokenLifecycleID string  `json:"token_lifecycle_id"`
    TokenAddress     string  `json:"token_address"`
    Chain            string  `json:"chain"`

    ExecutionID      string  `json:"execution_id"`        // SHA256(trace_id || version_id || token_address || chain)[:16] — see architecture § 4.10.D.2
    SizeUsd          float64 `json:"size_usd"`
    SizeBaseRaw      string  `json:"size_base_raw"`        // decimal string
    MaxSlippageBps   int32   `json:"max_slippage_bps"`
    WalletAddress    string  `json:"wallet_address"`       // EIP-55
    WalletShard      int32   `json:"wallet_shard"`
    AllocatedAt      string  `json:"allocated_at"`
}
```

- **Source file:** `contracts/allocation.go`
- **Producer:** `internal/modules/capital`
- **Consumer:** `internal/modules/execution`

---

### 3.9 `ExecutionResultDTO` — Layer 8

Trade outcome with full realism metadata (§ 3.8.24).

```go
type ExecutionResultDTO struct {
    EventID       string  `json:"event_id"`
    TraceID       string  `json:"trace_id"`
    CorrelationID string  `json:"correlation_id"`
    CausationID   string  `json:"causation_id"`
    VersionID     string  `json:"version_id"`

    TokenLifecycleID string  `json:"token_lifecycle_id"`
    ExecutionID      string  `json:"execution_id"`
    AllocationID     string  `json:"allocation_id"`

    Status              string `json:"status"`              // confirmed | reverted | dropped | replaced | failed
    Success             bool   `json:"success"`
    TxHash              string `json:"tx_hash"`             // empty if never submitted
    BlockNumber         uint64 `json:"block_number"`
    Attempts            int32  `json:"attempts"`
    Replaced            bool   `json:"replaced"`
    ReplacementCount    int32  `json:"replacement_count"`
    MempoolRoute        string `json:"mempool_route"`       // public | private_flashbots | private_beaverbuild
    NonceUsed           uint64 `json:"nonce_used"`
    WalletAddress       string `json:"wallet_address"`
    WalletShard         int32  `json:"wallet_shard"`
    FinalGasUsed        uint64 `json:"final_gas_used"`
    FinalMaxFeeWei      string `json:"final_max_fee_wei"`   // decimal string
    FinalPriorityFeeWei string `json:"final_priority_fee_wei"`
    RealizedEntryPrice  string `json:"realized_entry_price"` // decimal string
    SlippageRealizedBps int32  `json:"slippage_realized_bps"`
    LatencyMs           int32  `json:"latency_ms"`
    ErrorCode           string `json:"error_code"`          // enum; empty if success
    CompletedAt         string `json:"completed_at"`
}
```

- **Source file:** `contracts/execution.go`
- **Producer:** `internal/modules/execution`
- **Consumer:** `internal/modules/position` (on success)

---

### 3.10 `PositionStateDTO` — Layer 9

Position open/exit state. Same DTO emitted multiple times across position lifetime (each emission is a state snapshot).

```go
type PositionStateDTO struct {
    EventID       string  `json:"event_id"`
    TraceID       string  `json:"trace_id"`
    CorrelationID string  `json:"correlation_id"`
    CausationID   string  `json:"causation_id"`
    VersionID     string  `json:"version_id"`

    TokenLifecycleID string  `json:"token_lifecycle_id"`
    PositionID       string  `json:"position_id"`         // SHA256(execution_id)[:16]
    ExecutionID      string  `json:"execution_id"`
    TokenAddress     string  `json:"token_address"`
    Chain            string  `json:"chain"`

    Status           string  `json:"status"`              // open | exited | failed
    EntryPrice       string  `json:"entry_price"`         // decimal string
    EntrySizeUsd     float64 `json:"entry_size_usd"`
    CurrentPrice     string  `json:"current_price"`       // decimal string; "" if not polled yet

    ExitPrice        string  `json:"exit_price"`          // empty until exited
    ExitReason       string  `json:"exit_reason"`         // TP1 | TP2 | SL | TIME | MANUAL; empty until exited
    PnlUsd           float64 `json:"pnl_usd"`             // 0 until exited
    PnlPct           float64 `json:"pnl_pct"`             // 0 until exited

    Tp1Bps           int32   `json:"tp1_bps"`
    Tp2Bps           int32   `json:"tp2_bps"`
    SlBps            int32   `json:"sl_bps"`
    MaxHoldSeconds   int32   `json:"max_hold_seconds"`

    OpenedAt         string  `json:"opened_at"`
    ExitedAt         string  `json:"exited_at"`           // empty until exited
    SnapshotAt       string  `json:"snapshot_at"`
}
```

- **Source file:** `contracts/position.go`
- **Producer:** `internal/modules/position`
- **Consumer:** `internal/modules/learning` (on exit only)

---

### 3.11 `EvaluationDTO` — Layer 10 (windowed metrics)

Per-version performance evaluation over a time window.

```go
type EvaluationDTO struct {
    EventID       string  `json:"event_id"`
    TraceID       string  `json:"trace_id"`
    CorrelationID string  `json:"correlation_id"`
    CausationID   string  `json:"causation_id"`
    VersionID     string  `json:"version_id"`            // version being evaluated

    EvaluationID         string  `json:"evaluation_id"`
    WindowStart          string  `json:"window_start"`
    WindowEnd            string  `json:"window_end"`
    SampleSize           int32   `json:"sample_size"`

    TruePositiveCount    int32   `json:"true_positive_count"`
    FalsePositiveCount   int32   `json:"false_positive_count"`
    TrueNegativeCount    int32   `json:"true_negative_count"`
    FalseNegativeCount   int32   `json:"false_negative_count"`

    Expectancy           float64 `json:"expectancy"`
    MaxDrawdownPct       float64 `json:"max_drawdown_pct"`
    BrierScore           float64 `json:"brier_score"`
    PredictionErrorMean  float64 `json:"prediction_error_mean"`
    EvaluatedAt          string  `json:"evaluated_at"`
}
```

- **Source file:** `contracts/evaluation.go`
- **Producer:** `internal/modules/learning`
- **Consumer:** `internal/modules/learning` (triggers weight updates)

---

### 3.12 `LearningRecordDTO` — Layer 10 (per-trade record)

Per-trade and per-shadow-trade record for learning.

```go
type LearningRecordDTO struct {
    EventID       string  `json:"event_id"`
    TraceID       string  `json:"trace_id"`
    CorrelationID string  `json:"correlation_id"`
    CausationID   string  `json:"causation_id"`
    VersionID     string  `json:"version_id"`

    RecordID           string  `json:"record_id"`            // SHA256(token_lifecycle_id||shadow_flag)[:16]
    TokenLifecycleID   string  `json:"token_lifecycle_id"`

    Shadow             bool    `json:"shadow"`               // TRUE if this is a rejected-opportunity observation
    Outcome            string  `json:"outcome"`              // TP | SL | TIME | RUG | MISSED_PUMP | CORRECT_REJECT
    Classification     string  `json:"classification"`       // TP | FP | TN | FN
    PnlUsd             float64 `json:"pnl_usd"`              // 0 for shadow; realized PnL for executed
    PnlPct             float64 `json:"pnl_pct"`
    PredictionError    float64 `json:"prediction_error"`
    Cohort             string  `json:"cohort"`               // "liquidity_bucket:age_bucket:source"

    FeaturesSnapshot   FeatureDTO       `json:"features_snapshot"`
    EdgeSnapshot       EdgeDTO          `json:"edge_snapshot"`
    ValidatedSnapshot  ValidatedEdgeDTO `json:"validated_snapshot"`

    RecordedAt         string  `json:"recorded_at"`
}
```

- **Source file:** `contracts/learning_record.go`
- **Producer:** `internal/modules/learning`
- **Consumer:** `internal/modules/learning` (self-consuming for parameter updates)

---

## 4. Cross-Module Dependency Matrix

| DTO                      | Producer               | Consumer(s)                               |
| ------------------------ | ---------------------- | ----------------------------------------- |
| `MarketDataDTO`          | `modules/ingestion`    | `modules/data_quality`                    |
| `DataQualityDTO`         | `modules/data_quality` | `modules/features`, `modules/learning`    |
| `FeatureDTO`             | `modules/features`     | `modules/edge`, `modules/learning`        |
| `EdgeDTO`                | `modules/edge`         | `modules/validation`, `modules/learning`  |
| `ProbabilityEstimateDTO` | `modules/models`       | `modules/validation`                      |
| `SlippageEstimateDTO`    | `modules/models`       | `modules/validation`, `modules/execution` |
| `LatencyProfileDTO`      | `modules/models`       | `modules/execution`                       |
| `ValidatedEdgeDTO`       | `modules/validation`   | `modules/selection`, `modules/learning`   |
| `SelectionOutputDTO`     | `modules/selection`    | `modules/capital`                         |
| `AllocationDTO`          | `modules/capital`      | `modules/execution`                       |
| `ExecutionResultDTO`     | `modules/execution`    | `modules/position`, `modules/learning`    |
| `PositionStateDTO`       | `modules/position`     | `modules/learning`                        |
| `EvaluationDTO`          | `modules/learning`     | `modules/learning` (internal)             |
| `LearningRecordDTO`      | `modules/learning`     | `modules/learning` (internal)             |

---

## 5. Event Bus Payload Mapping

Each event in the `events` table wraps exactly one DTO as payload:

| `event_type`            | Payload DTO                      | Emitted by                        |
| ----------------------- | -------------------------------- | --------------------------------- |
| `market_data_event`     | `MarketDataDTO`                  | ingestion                         |
| `data_quality_event`    | `DataQualityDTO`                 | data_quality                      |
| `feature_event`         | `FeatureDTO`                     | features                          |
| `edge_event`            | `EdgeDTO`                        | edge                              |
| `probability_event`     | `ProbabilityEstimateDTO`         | models                            |
| `slippage_event`        | `SlippageEstimateDTO`            | models                            |
| `latency_event`         | `LatencyProfileDTO`              | models                            |
| `validated_edge_event`  | `ValidatedEdgeDTO`               | validation                        |
| `selection_event`       | `SelectionOutputDTO`             | selection                         |
| `allocation_event`      | `AllocationDTO`                  | capital                           |
| `execution_event`       | `ExecutionResultDTO`             | execution                         |
| `position_event`        | `PositionStateDTO`               | position                          |
| `evaluation_event`      | `EvaluationDTO`                  | learning                          |
| `learning_record_event` | `LearningRecordDTO`              | learning                          |
| `telegram_event`        | `TelegramNotificationDTO` (meta) | multiple (operator notifications) |

---

## 6. Validation Rules

### Per-field Constraints (adapter-enforced)

| Field Pattern                                  | Rule                                                   |
| ---------------------------------------------- | ------------------------------------------------------ |
| `*ID` fields (16-hex)                          | `^[0-9a-f]{16}$`                                       |
| `TokenAddress`, `WalletAddress`, `PoolAddress` | EIP-55 checksummed `0x` + 40 hex chars                 |
| `TxHash`, `BlockHash`                          | `0x` + 64 hex chars                                    |
| `Chain`                                        | Must be in configured chain registry                   |
| `Probability`, `*Score`, `*Confidence`         | `[0.0, 1.0]` inclusive                                 |
| `*Bps` (basis points)                          | `[0, 10000]`                                           |
| Timestamps                                     | ISO 8601 UTC, e.g., `2026-04-21T12:34:56.789Z`         |
| Decimal strings                                | Match `^-?[0-9]+(\.[0-9]+)?$` (no scientific notation) |
| Enum fields                                    | Must be in declared enum set                           |

### Cross-DTO Constraints

- `CausationID` (when non-empty) must exist as `event_id` in a prior event
- `VersionID` must exist in `strategy_versions`
- `TraceID` must be stable for a token's entire journey
- `TokenLifecycleID` must exist in `token_lifecycle` before first consumer write
- Output DTO of stage N ↔ input of stage N+1 must type-match

### Enum Registry

| Field                              | Allowed Values                                                                                                                 |
| ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| `TokenState`                       | DETECTED, FILTERED, FEATURED, EDGE_DETECTED, VALIDATED, SELECTED, EXECUTED, POSITION_OPEN, EXITED, EVALUATED, REJECTED, FAILED |
| `DataQualityDTO.Decision`          | PASS, REJECT, RISKY_PASS                                                                                                       |
| `EdgeDTO.EdgeType`                 | NEW_LAUNCH, MOMENTUM, WALLET_SURGE                                                                                             |
| `ValidatedEdgeDTO.Decision`        | ACCEPT, REJECT                                                                                                                 |
| `ExecutionResultDTO.Status`        | confirmed, reverted, dropped, replaced, failed                                                                                 |
| `ExecutionResultDTO.MempoolRoute`  | public, private_flashbots, private_beaverbuild                                                                                 |
| `PositionStateDTO.Status`          | open, exited, failed                                                                                                           |
| `PositionStateDTO.ExitReason`      | TP1, TP2, SL, TIME, MANUAL                                                                                                     |
| `LearningRecordDTO.Outcome`        | TP, SL, TIME, RUG, MISSED_PUMP, CORRECT_REJECT                                                                                 |
| `LearningRecordDTO.Classification` | TP, FP, TN, FN                                                                                                                 |
| `MarketDataDTO.Transport`          | websocket, polling, gap_recovery                                                                                               |

---

## 7. Anti-Patterns

```go
// ❌ Raw map instead of DTO
result := map[string]interface{}{"token": "0xabc", "status": "done"}

// ❌ Mutable DTO with setters
type BadDTO struct {
    TokenAddress string
}
func (d *BadDTO) SetToken(a string) { d.TokenAddress = a }  // No mutation methods

// ❌ Logic / methods on DTO
func (d *BadDTO) IsValid() bool { ... }  // No methods — pure data only

// ❌ Cross-module type import
import "cryptobot/internal/modules/execution/internal"  // Forbidden

// ❌ Forbidden types
type BadDTO struct {
    CreatedAt time.Time       // Use string (ISO 8601)
    Amount    *big.Int        // Use decimal string
    Blob      []byte          // Forbidden
    Meta      map[string]any  // Forbidden at module boundary
}

// ❌ Missing correlation fields
type MissingTraceDTO struct {
    TokenAddress string
    // No TraceID, CorrelationID, CausationID, VersionID → rejected by adapter
}

// ✅ Correct DTO
type GoodDTO struct {
    EventID       string `json:"event_id"`
    TraceID       string `json:"trace_id"`
    CorrelationID string `json:"correlation_id"`
    CausationID   string `json:"causation_id"`
    VersionID     string `json:"version_id"`

    TokenAddress  string  `json:"token_address"`
    Score         float64 `json:"score"`         // [0.0, 1.0]
    AmountRaw     string  `json:"amount_raw"`    // decimal string
    CreatedAt     string  `json:"created_at"`    // ISO 8601
}
```

---

## 8. DTO Immutability Enforcement (Go)

Since Go has no `frozen` keyword, immutability is enforced by convention + review:

- **No pointer receivers on DTOs** — all DTO types have no methods
- **Exported fields only** — consumers read by field access, never set
- **Pass by value** — DTOs passed as values, not `*DTO`, across module boundaries
- **Review gate:** PR reviewers reject any commit that adds a method, setter, or `*DTO` return type in `contracts/`
- **Lint rule:** forbid `*contracts.X` in function signatures except in `database/` implementations

---

# 8. Production Gap Extensions (Additive — No Breaking Changes)

> All fields below are **additive**. Existing DTOs gain new fields with Go zero-value defaults. No field is removed or renamed. Applied to every DTO defined in § 3.

---

## 8.1 Common Additive Fields (Applied to ALL DTOs in § 3)

Every DTO from `MarketDataDTO` through `LearningRecordDTO` gains:

```go
// Embedded in every DTO (add to each struct at the end, before the timestamp field):
ExpiresAt string `json:"expires_at"`   // ISO 8601. Empty string = no expiry.
Priority  int32  `json:"priority"`      // Higher = processed first. Default 0.
```

### Rules

- **Producer sets `ExpiresAt`** using per-stage TTL from `config/pipeline.yaml`:
  - `market_data.ttl_seconds` (default 30)
  - `data_quality.ttl_seconds` (default 15)
  - `feature.ttl_seconds` (default 10)
  - `edge.ttl_seconds` (default 8)
  - `validated_edge.ttl_seconds` (default 5)
  - `selection.ttl_seconds` (default 4)
  - `allocation.ttl_seconds` (default 3)
  - Position/Execution/Learning/Evaluation: `""` (no expiry)
- **Producer sets `Priority`** via `resource_control.ComputePriority(eventType, dto, now)`.
- Consumers MUST skip any DTO where `ExpiresAt != "" AND parseISO(ExpiresAt) < now()` and emit an `expired_event` instead.
- `ExpiresAt` and `Priority` are included in canonical JSON for `EventID` computation — changing either produces a new EventID.

### New DTO — `ExpiredEventDTO`

Emitted by worker when TTL check fails at claim time.

```go
// contracts/expired_event.go
type ExpiredEventDTO struct {
    EventID       string `json:"event_id"`
    TraceID       string `json:"trace_id"`
    CorrelationID string `json:"correlation_id"`
    CausationID   string `json:"causation_id"`    // = original event_id that expired
    VersionID     string `json:"version_id"`

    OriginalEventType string `json:"original_event_type"`
    Stage             string `json:"stage"`        // worker group name
    ExpiredAtIso      string `json:"expired_at_iso"`
    DelayMs           int64  `json:"delay_ms"`     // (now - original.ExpiresAt) in ms

    ExpiresAt string `json:"expires_at"`           // "" — expired events never themselves expire
    Priority  int32  `json:"priority"`             // 0 (drained at idle)
    EmittedAt string `json:"emitted_at"`
}
```

- **Producer:** any worker (generic loop)
- **Consumer:** `internal/modules/learning` (as shadow false-negative candidate)
- **event_type:** `expired_event`
- **ID rule:** `EventID = SHA256(original_event_id || "expired")[:16]`

---

## 8.2 `ExecutionResultDTO` — MEV & Path Extensions

Additive fields for § 3.9:

```go
MEVProtected       bool   `json:"mev_protected"`         // true if routed via private relay
ExecutionPath      string `json:"execution_path"`        // "public" | "flashbots" | "beaverbuild" | "eden"
SlippageGuardBps   int32  `json:"slippage_guard_bps"`    // amountOutMin guard used
RejectionReason    string `json:"rejection_reason"`      // populated when Status="rejected" pre-submission
Simulated          bool   `json:"simulated"`             // true when execution_mode=shadow
```

- `ExecutionPath ∈ {"public","flashbots","beaverbuild","eden"}` — enforced at adapter write time.
- `Simulated=true` marks paper-trade records; downstream Position/Learning handle them identically except no capital is at risk.

---

## 8.3 `AllocationDTO` — Envelope Rejection

Additive fields for § 3.8:

```go
Rejected        bool   `json:"rejected"`         // true if envelope check failed
RejectReason    string `json:"reject_reason"`    // "per_token_cap" | "per_cohort_cap" | "total_exposure" | "max_concurrent" | ""
CohortID        string `json:"cohort_id"`        // liquidity_bucket:age_bucket:source
```

Consumer (Execution) MUST skip processing when `Rejected=true` — emits no execution event; learning records as shadow.

---

## 8.4 `EdgeDTO` — Opportunity Window

Additive field for § 3.4:

```go
OpportunityWindowMs int32 `json:"opportunity_window_ms"`   // Required for Phase 4 latency gate
```

Computed at emission time: `base_ms * (1 + momentum_factor)` from config.

---

## 8.5 `ValidatedEdgeDTO` — Latency Gate

Additive fields for § 3.6:

```go
ExpectedLatencyMs int32  `json:"expected_latency_ms"`     // LatencyProfile.P95Ms + Slippage.BuildSubmitP95Ms
LatencyGatePassed bool   `json:"latency_gate_passed"`
```

If `LatencyGatePassed=false`, `Decision="REJECT"` and `RejectReason="latency_exceeds_window"`.

---

## 8.6 New DTO — `SystemStateDTO`

Singleton DTO describing current system-wide risk posture. Persisted in `system_state` table (see db adapter spec § 11.1).

```go
// contracts/system_state.go
type SystemStateDTO struct {
    Mode                 string  `json:"mode"`                     // "BALANCED" | "STRICT" | "EXPLORATION" | "DEGRADED" | "HALTED"
    DrawdownPct          float64 `json:"drawdown_pct"`              // [0.0, 1.0]
    DrawdownWindowHours  int32   `json:"drawdown_window_hours"`
    OpenPositions        int32   `json:"open_positions"`
    TotalExposureUsd     float64 `json:"total_exposure_usd"`
    ActiveStrategyID     string  `json:"active_strategy_id"`        // 16 hex
    ShadowStrategyID     string  `json:"shadow_strategy_id"`        // 16 hex or ""
    LastTransitionReason string  `json:"last_transition_reason"`
    UpdatedAt            string  `json:"updated_at"`                // ISO 8601
    VersionID            string  `json:"version_id"`                // active strategy at update time
}
```

- **Producer:** `internal/modules/risk_controller` (background worker)
- **Consumer:** Execution, Selection, Capital workers (pre-check before decision)
- **No event_type** — not published to bus; read directly via `adapter.GetSystemState`. A change emits an `mode_transition_event` for audit (payload = previous + new SystemStateDTO).

---

## 8.7 `StrategyVersion` — Shadow/Rollback Status

Additive fields for § 3.13 (StrategyVersion metadata — lives in `strategy_versions` table, not event bus):

```go
Status              string  `json:"status"`                  // "draft" | "shadow" | "active" | "deactivated" | "rolled_back"
ShadowStartedAt     string  `json:"shadow_started_at"`       // ISO 8601 or ""
PromotedAt          string  `json:"promoted_at"`             // ISO 8601 or ""
RolledBackAt        string  `json:"rolled_back_at"`          // ISO 8601 or ""
ParentVersionID     string  `json:"parent_version_id"`       // previous active (for rollback target)
```

Exactly one version has `Status="active"` at any time (enforced by partial unique index — see adapter spec § 11.3).

---

## 8.8 `LearningRecordDTO` — Shadow + Simulated Flags

Additive fields for § 3.12:

```go
Simulated          bool   `json:"simulated"`                // true if from shadow execution mode
ExpiredSource      bool   `json:"expired_source"`            // true if record derived from expired_event
StrategyStatus     string `json:"strategy_status"`           // "active" | "shadow" at record time
```

Promotion/rollback logic in § Phase 5 uses `StrategyStatus` to separate shadow-version metrics from active-version metrics.

---

## 8.9 Validation Rule Additions (§ 6)

| Field                        | Rule                                                                                           |
| ---------------------------- | ---------------------------------------------------------------------------------------------- |
| `ExpiresAt`                  | Empty string OR ISO 8601 UTC with `Z` suffix                                                   |
| `Priority`                   | `int32`, `[0, 10000]` inclusive                                                                |
| `ExecutionPath`              | One of `{"public","flashbots","beaverbuild","eden"}`                                           |
| `SystemStateDTO.Mode`        | One of `{"BALANCED","STRICT","EXPLORATION","DEGRADED","HALTED"}`                               |
| `StrategyVersion.Status`     | One of `{"draft","shadow","active","deactivated","rolled_back"}`                               |
| `AllocationDTO.RejectReason` | One of `{"","per_token_cap","per_cohort_cap","total_exposure","max_concurrent","kill_switch"}` |

Adapter rejects writes with out-of-set enum values (`ErrInvalidEnum`).
