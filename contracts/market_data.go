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

	Chain       string `json:"chain"`  // eth | bsc
	Market      string `json:"market"` // e.g., "eth-uniswap-v2"
	BlockNumber uint64 `json:"block_number"`
	BlockHash   string `json:"block_hash"` // 0x-prefixed
	TxHash      string `json:"tx_hash"`
	LogIndex    uint32 `json:"log_index"`

	EventTopic   string `json:"event_topic"`   // PairCreated | Mint | Swap | Burn
	PoolAddress  string `json:"pool_address"`  // EIP-55 checksummed
	TokenAddress string `json:"token_address"` // target token side (non-base)
	BaseAddress  string `json:"base_address"`  // WETH/USDT/USDC/BNB

	Token0Address   string `json:"token0_address"`
	Token1Address   string `json:"token1_address"`
	Amount0Raw      string `json:"amount0_raw"` // decimal string, no scientific notation
	Amount1Raw      string `json:"amount1_raw"`
	ReserveBaseRaw  string `json:"reserve_base_raw"`
	ReserveTokenRaw string `json:"reserve_token_raw"`

	BlockTimestamp string `json:"block_timestamp"` // ISO 8601 UTC
	IngestedAt     string `json:"ingested_at"`     // ISO 8601 UTC

	RpcEndpoint       string `json:"rpc_endpoint"`
	Transport         string `json:"transport"` // websocket | polling | gap_recovery
	ConfirmationDepth uint32 `json:"confirmation_depth"`
	Reorged           bool   `json:"reorged"`

	ExpiresAt string `json:"expires_at"` // ISO 8601 UTC; "" = no expiry
	Priority  int32  `json:"priority"`   // higher = processed first; default 0

	// Symbol and Name are populated for Solana tokens where on-chain metadata
	// is available at ingest time (e.g. Pump.fun create instruction).
	// Empty string for EVM and Raydium pool-init events.
	Symbol string `json:"symbol,omitempty"`
	Name   string `json:"name,omitempty"`

	// Phase 10 (Reference-Repo Improvements / Task F) — Solana bonding
	// curve progress in bps (0..10000). Populated by ingestion_solana for
	// pump.fun / bonk.fun curve markets; 0 (the default) means "not
	// applicable". Used by Layer 1 to reject already-graduated curves.
	BondingCurveProgressBps int32 `json:"bonding_curve_progress_bps,omitempty"`

	// ─────────────────────────────────────────────────────────────────────
	// Layer-1 Data Quality detector inputs (additive — Layer 1 fix).
	//
	// Each block carries a *Known flag because the zero value of bool/int/
	// float is indistinguishable from "not measured". When *Known is false,
	// the corresponding detector emits a `dq_unknown_<name>` flag and the
	// active operational-mode profile decides how to degrade (STRICT counts
	// it as risk; BALANCED/EXPLORATION ignore it).
	//
	// The data_quality module MUST NOT make RPC calls to populate these —
	// they are filled by upstream workers (ingestion / probe workers) so
	// the module remains a pure function. Until upstream populates them,
	// detectors degrade per profile rather than silently passing.
	// ─────────────────────────────────────────────────────────────────────

	// Honeypot simulation result (callStatic buy → sell on the router).
	HoneypotSimKnown bool `json:"honeypot_sim_known,omitempty"`
	BuySimSuccess    bool `json:"buy_sim_success,omitempty"`
	SellSimSuccess   bool `json:"sell_sim_success,omitempty"`

	// Tax (basis points, 1bp = 0.01%). Includes detection of dynamic / per-
	// address tax overrides and presence of a blacklist function.
	TaxKnown                 bool  `json:"tax_known,omitempty"`
	BuyTaxBps                int32 `json:"buy_tax_bps,omitempty"`
	SellTaxBps               int32 `json:"sell_tax_bps,omitempty"`
	InitialBuyTaxBps         int32 `json:"initial_buy_tax_bps,omitempty"`
	InitialSellTaxBps        int32 `json:"initial_sell_tax_bps,omitempty"`
	TaxIsDynamic             bool  `json:"tax_is_dynamic,omitempty"`
	BlacklistFunctionPresent bool  `json:"blacklist_function_present,omitempty"`

	// LP lock — boolean lock plus a [0,1] strength score (0 = unlocked,
	// 1 = burned/permanent).
	LpLockKnown    bool    `json:"lp_lock_known,omitempty"`
	LpLocked       bool    `json:"lp_locked,omitempty"`
	LpLockStrength float64 `json:"lp_lock_strength,omitempty"`
	LpLockDays     int32   `json:"lp_lock_days,omitempty"`

	// Owner-privilege selectors discovered on the contract (EVM) or
	// authority state (Solana). Canonical entries: "mint", "pause",
	// "blacklist", "set_max_tx", "upgrade", "set_tax".
	OwnerPrivilegesKnown     bool     `json:"owner_privileges_known,omitempty"`
	OwnerPrivileges          []string `json:"owner_privileges,omitempty"`
	MintAuthorityRenounced   bool     `json:"mint_authority_renounced,omitempty"`
	FreezeAuthorityRenounced bool     `json:"freeze_authority_renounced,omitempty"`
	SolanaAuthoritiesKnown   bool     `json:"solana_authorities_known,omitempty"`
	ContractVerified         bool     `json:"contract_verified,omitempty"`
	ContractVerifiedKnown    bool     `json:"contract_verified_known,omitempty"`

	// Holder distribution — fraction (0..1) of supply held by top 5 wallets.
	HolderDistKnown bool    `json:"holder_dist_known,omitempty"`
	Top5HolderPct   float64 `json:"top5_holder_pct,omitempty"`
	HolderCount     int32   `json:"holder_count,omitempty"`

	// Wash-trading window stats over the most recent N swaps.
	WashStatsKnown  bool    `json:"wash_stats_known,omitempty"`
	TxCount1m       int32   `json:"tx_count_1m,omitempty"`
	UniqueWallets1m int32   `json:"unique_wallets_1m,omitempty"`
	WalletEntropy   float64 `json:"wallet_entropy,omitempty"`
	RepeatRatio1m   float64 `json:"repeat_ratio_1m,omitempty"`

	// Liquidity quality — USD-denominated pool depth and LP-churn signal.
	LpStatsKnown        bool    `json:"lp_stats_known,omitempty"`
	LiquidityUsd        float64 `json:"liquidity_usd,omitempty"`
	SingleLpProviderPct float64 `json:"single_lp_provider_pct,omitempty"`
	LpChurnDetected     bool    `json:"lp_churn_detected,omitempty"`
	LpChurnBlocks       int32   `json:"lp_churn_blocks,omitempty"`
	PoolAgeSeconds      int32   `json:"pool_age_seconds,omitempty"`

	// Token supply — total/maximum token supply at creation.
	// Populated by ingestion for Pump.fun tokens (from create instruction)
	// and EVM tokens (from totalSupply() view call).
	// TotalSupplyKnown=false means the value was not available at ingest time;
	// the DQ check is skipped rather than incorrectly rejecting.
	TotalSupplyKnown bool    `json:"total_supply_known,omitempty"`
	TotalSupply      float64 `json:"total_supply,omitempty"`

	// ─────────────────────────────────────────────────────────────────────
	// Dev Reputation — serial launcher and social-link signals.
	//
	// CreatorAddress is the wallet that deployed the token (pump.fun `user`
	// field from the CreateEvent log, or EVM deployer). Populated by
	// ingestion at Layer 0 so downstream workers can look up creator history
	// without making RPC calls inside the DQ module.
	//
	// CreatorPrevTokenCount is the number of OTHER tokens this creator
	// wallet has previously launched (excluding the current token). Populated
	// by the solana_creator_reputation probe via a single DB COUNT query
	// against the market_data table. When CreatorPrevTokenCountKnown=false
	// (probe not yet run), the DQ detector degrades per profile — STRICT
	// counts it as unknown risk; BALANCED/EXPLORATION ignore it.
	//
	// MetadataURI is the on-chain URI pointing to the token's off-chain
	// JSON metadata (IPFS, Arweave, or HTTPS). For pump.fun tokens this is
	// the `uri` field emitted in the CreateEvent log. Populated by ingestion
	// at Layer 0 so the solana_metadata probe can fetch it without any extra
	// RPC call. Empty for tokens whose program does not emit a URI.
	//
	// HasSocialLinks is true when the token metadata URI resolves to at least
	// one non-empty social link (twitter / telegram / website). Populated by
	// the solana_metadata probe. When SocialLinksKnown=false the detector
	// degrades per profile.
	// ─────────────────────────────────────────────────────────────────────
	MetadataURI                string `json:"metadata_uri,omitempty"`
	CreatorAddress             string `json:"creator_address,omitempty"`
	CreatorPrevTokenCountKnown bool   `json:"creator_prev_token_count_known,omitempty"`
	CreatorPrevTokenCount      int32  `json:"creator_prev_token_count,omitempty"`
	SocialLinksKnown           bool   `json:"social_links_known,omitempty"`
	HasSocialLinks             bool   `json:"has_social_links,omitempty"`
}
