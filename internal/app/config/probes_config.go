package config

// ProbesConfig configures the optional market-data enrichment stage
// (residual-risk #4). All probes default OFF — the framework ships
// dormant. Enable per-probe by setting `enabled: true` AND providing
// the deployment-specific config (e.g. simulation_contract address).
type ProbesConfig struct {
	// Enabled toggles the entire probes worker. When false, the worker
	// is not registered and the data_quality stage continues to consume
	// the raw market_data_event directly.
	Enabled bool `yaml:"enabled"`

	// HoneypotSim configures the honeypot_sim probe. See
	// internal/modules/probes/honeypot_sim.go.
	HoneypotSim HoneypotSimYAML `yaml:"honeypot_sim"`

	// SolanaAuthorities configures the SPL mint/freeze authority probe.
	SolanaAuthorities SolanaProbeYAML `yaml:"solana_authorities"`

	// SolanaPumpfunLp configures the pump.fun bonding-curve reserve probe.
	SolanaPumpfunLp SolanaProbeYAML `yaml:"solana_pumpfun_lp"`

	// SolanaHolderDist configures the SPL holder-concentration probe.
	SolanaHolderDist SolanaHolderDistYAML `yaml:"solana_holder_dist"`

	// SolanaMetadata configures the off-chain metadata fetch probe.
	// Fetches the MetadataURI (IPFS/Arweave/HTTPS) and sets
	// SocialLinksKnown + HasSocialLinks. No RPC client required.
	SolanaMetadata SolanaMetadataYAML `yaml:"solana_metadata"`

	// EVMPairReserves configures the Uniswap-V2 getReserves probe.
	EVMPairReserves EVMPairReservesYAML `yaml:"evm_pair_reserves"`
}

// HoneypotSimYAML mirrors probes.HoneypotSimConfig but lives in the
// config package to avoid importing probes into config (and creating an
// import cycle when validate_ranges ranges over probe-specific bounds).
type HoneypotSimYAML struct {
	Enabled            bool   `yaml:"enabled"`
	SimulationContract string `yaml:"simulation_contract"`
	TimeoutMs          int    `yaml:"timeout_ms"`
}

// SolanaProbeYAML is the common shape for Solana enrichment probes that
// only need a timeout + commitment (authorities, pumpfun_lp).
type SolanaProbeYAML struct {
	Enabled    bool   `yaml:"enabled"`
	TimeoutMs  int    `yaml:"timeout_ms"`
	Commitment string `yaml:"commitment"`
}

// SolanaHolderDistYAML adds a top-K knob on top of the common shape.
type SolanaHolderDistYAML struct {
	Enabled    bool   `yaml:"enabled"`
	TimeoutMs  int    `yaml:"timeout_ms"`
	Commitment string `yaml:"commitment"`
	TopK       int    `yaml:"top_k"`
}

// EVMPairReservesYAML configures the Uniswap-V2 pair getReserves probe.
type EVMPairReservesYAML struct {
	Enabled   bool `yaml:"enabled"`
	TimeoutMs int  `yaml:"timeout_ms"`
}

// SolanaMetadataYAML configures the off-chain metadata fetch probe.
// The probe resolves IPFS/Arweave URIs via the configured gateway and
// parses the JSON for social link fields.
type SolanaMetadataYAML struct {
	Enabled      bool   `yaml:"enabled"`
	TimeoutMs    int    `yaml:"timeout_ms"`
	IPFSGateway  string `yaml:"ipfs_gateway"`
	MaxBodyBytes int64  `yaml:"max_body_bytes"`
}
