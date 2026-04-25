package contracts

// ExecutionResultDTO carries the trade outcome with full realism metadata
// from Layer 8 execution engine.
//
// Source file: contracts/execution.go
// Producer:    internal/modules/execution
// Consumer:    internal/modules/position (on success)
type ExecutionResultDTO struct {
	EventID       string `json:"event_id"`
	TraceID       string `json:"trace_id"`
	CorrelationID string `json:"correlation_id"`
	CausationID   string `json:"causation_id"`
	VersionID     string `json:"version_id"`

	TokenLifecycleID string `json:"token_lifecycle_id"`
	ExecutionID      string `json:"execution_id"`
	AllocationID     string `json:"allocation_id"`

	Status              string `json:"status"`               // confirmed | reverted | dropped | replaced | failed
	Success             bool   `json:"success"`
	TxHash              string `json:"tx_hash"`              // empty if never submitted
	BlockNumber         uint64 `json:"block_number"`
	Attempts            int32  `json:"attempts"`
	Replaced            bool   `json:"replaced"`
	ReplacementCount    int32  `json:"replacement_count"`
	MempoolRoute        string `json:"mempool_route"`        // public | private_flashbots | private_beaverbuild
	NonceUsed           uint64 `json:"nonce_used"`
	WalletAddress       string `json:"wallet_address"`
	WalletShard         int32  `json:"wallet_shard"`
	FinalGasUsed        uint64 `json:"final_gas_used"`
	FinalMaxFeeWei      string `json:"final_max_fee_wei"`    // decimal string
	FinalPriorityFeeWei string `json:"final_priority_fee_wei"`
	RealizedEntryPrice  string `json:"realized_entry_price"` // decimal string
	SlippageRealizedBps int32  `json:"slippage_realized_bps"`
	LatencyMs           int32  `json:"latency_ms"`
	ErrorCode           string `json:"error_code"` // enum; empty if success
	CompletedAt         string `json:"completed_at"` // ISO 8601
}
