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

	ExecutionID    string  `json:"execution_id"`     // SHA256(correlation_id)[:16] — idempotency key
	SizeUsd        float64 `json:"size_usd"`
	SizeBaseRaw    string  `json:"size_base_raw"`    // decimal string
	MaxSlippageBps int32   `json:"max_slippage_bps"`
	WalletAddress  string  `json:"wallet_address"`  // EIP-55
	WalletShard    int32   `json:"wallet_shard"`
	AllocatedAt    string  `json:"allocated_at"` // ISO 8601
}
