package contracts

// MarketDataDTO is the raw normalized blockchain event emitted by the ingestion module.
// CausationID is always "" — this is a Layer 0 root event.
// EventID = SHA256(chain||tx_hash||log_index)[:16].
//
// Source file: contracts/market_data.go
// Producer:    internal/modules/ingestion
// Consumer:    internal/modules/data_quality
type MarketDataDTO struct {
	EventID       string `json:"event_id"`
	TraceID       string `json:"trace_id"`
	CorrelationID string `json:"correlation_id"`
	CausationID   string `json:"causation_id"` // "" — Layer 0 is root
	VersionID     string `json:"version_id"`

	Chain       string `json:"chain"`        // eth | bsc
	Market      string `json:"market"`       // e.g., "eth-uniswap-v2"
	BlockNumber uint64 `json:"block_number"`
	BlockHash   string `json:"block_hash"` // 0x-prefixed
	TxHash      string `json:"tx_hash"`
	LogIndex    uint32 `json:"log_index"`

	EventTopic  string `json:"event_topic"`  // PairCreated | Mint | Swap | Burn
	PoolAddress string `json:"pool_address"` // EIP-55 checksummed
	TokenAddress string `json:"token_address"` // target token side (non-base)
	BaseAddress  string `json:"base_address"`  // WETH/USDT/USDC/BNB

	Token0Address string `json:"token0_address"`
	Token1Address string `json:"token1_address"`
	Amount0Raw    string `json:"amount0_raw"` // decimal string, no scientific notation
	Amount1Raw    string `json:"amount1_raw"`
	ReserveBaseRaw  string `json:"reserve_base_raw"`
	ReserveTokenRaw string `json:"reserve_token_raw"`

	BlockTimestamp string `json:"block_timestamp"` // ISO 8601 UTC
	IngestedAt     string `json:"ingested_at"`     // ISO 8601 UTC

	RpcEndpoint       string `json:"rpc_endpoint"`
	Transport         string `json:"transport"`          // websocket | polling | gap_recovery
	ConfirmationDepth uint32 `json:"confirmation_depth"`
	Reorged           bool   `json:"reorged"`
}
