package config

// PriceOracleConfig controls the live price feed used by Layer 9 position
// monitoring and Layer 8 shadow execution fills.
//
// mode "on_chain" uses Solana pool reserve reads (bonding curve / AMM vaults)
// as the primary oracle for Solana tokens, with DEXScreener as fallback for
// non-Solana chains and when on-chain reads fail.
//
// mode "dexscreener" (or any other value) uses DEXScreener for all chains.
type PriceOracleConfig struct {
	Mode              string `yaml:"mode"`                // "on_chain" | "dexscreener"
	CacheTTLSeconds   int    `yaml:"cache_ttl_seconds"`   // bounded RPC cache TTL
	StaleMaxMultiplier int   `yaml:"stale_max_multiplier"` // fail-open stale window = TTL × multiplier
}

// DefaultPriceOracleConfig returns safe defaults when YAML omits the block.
func DefaultPriceOracleConfig() PriceOracleConfig {
	return PriceOracleConfig{
		Mode:               "dexscreener",
		CacheTTLSeconds:    5,
		StaleMaxMultiplier: 3,
	}
}

func applyPriceOracleDefaults(c *PriceOracleConfig) {
	def := DefaultPriceOracleConfig()
	if c.Mode == "" {
		c.Mode = def.Mode
	}
	if c.CacheTTLSeconds <= 0 {
		c.CacheTTLSeconds = def.CacheTTLSeconds
	}
	if c.StaleMaxMultiplier <= 0 {
		c.StaleMaxMultiplier = def.StaleMaxMultiplier
	}
}
