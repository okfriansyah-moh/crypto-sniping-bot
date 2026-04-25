package contracts

// AllocationDTO carries the capital sizing decision from Layer 7.
// ExecutionID = SHA256(correlation_id)[:16] — idempotency key.
//
// Source file: contracts/allocation.go
// Producer:    internal/modules/capital
// Consumer:    internal/modules/execution
type AllocationDTO struct {
EventID       string `json:"event_id"`
TraceID       string `json:"trace_id"`
CorrelationID string `json:"correlation_id"`
CausationID   string `json:"causation_id"`
VersionID     string `json:"version_id"`

TokenLifecycleID string `json:"token_lifecycle_id"`
TokenAddress     string `json:"token_address"`
Chain            string `json:"chain"`

ExecutionID    string  `json:"execution_id"`    // SHA256(correlation_id)[:16] — idempotency key
SizeUsd        float64 `json:"size_usd"`
SizeBaseRaw    string  `json:"size_base_raw"`   // decimal string
MaxSlippageBps int32   `json:"max_slippage_bps"`
WalletAddress  string  `json:"wallet_address"`  // EIP-55
WalletShard    int32   `json:"wallet_shard"`

// §8.3 additive: envelope rejection fields.
// Execution MUST skip processing when Rejected=true.
Rejected     bool   `json:"rejected"`      // true if envelope check failed
RejectReason string `json:"reject_reason"` // "per_token_cap"|"per_cohort_cap"|"total_exposure"|"max_concurrent"|"kill_switch"|""
CohortID     string `json:"cohort_id"`     // liquidity_bucket:age_bucket:source

ExpiresAt   string `json:"expires_at"`   // ISO 8601 UTC; "" = no expiry
Priority    int32  `json:"priority"`     // higher = processed first; default 0
AllocatedAt string `json:"allocated_at"` // ISO 8601
}
