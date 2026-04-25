package config

// ChainConfig holds per-chain RPC and factory configuration.
// Values come from config/chains.yaml — no hardcoded values.
type ChainConfig struct {
	Name              string          `yaml:"name"`
	ChainID           uint64          `yaml:"chain_id"`
	RPCEndpoints      []string        `yaml:"rpc_endpoints"`
	WSEndpoints       []string        `yaml:"ws_endpoints"`
	ConfirmationDepth uint32          `yaml:"confirmation_depth"`
	BaseTokens        []BaseToken     `yaml:"base_tokens"`
	Factories         []FactoryConfig `yaml:"factories"`
}

// BaseToken is a known base-side token (WETH, USDT, WBNB, etc.).
type BaseToken struct {
	Address string `yaml:"address"`
	Symbol  string `yaml:"symbol"`
}

// FactoryConfig is a DEX factory contract address and protocol label.
type FactoryConfig struct {
	Address  string `yaml:"address"`
	Protocol string `yaml:"protocol"`
	Market   string `yaml:"market"`
}

// IngestionBackoff holds exponential backoff parameters for RPC reconnects.
type IngestionBackoff struct {
	InitialMs  int     `yaml:"initial_ms"`
	MaxMs      int     `yaml:"max_ms"`
	Multiplier float64 `yaml:"multiplier"`
}
