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

	// TODO: tax, lp_lock, owner_privileges, holder_dist, wash_stats —
	// add their YAML structs alongside HoneypotSim once each probe
	// implementation lands. Leave the parent ProbesConfig stable.
}

// HoneypotSimYAML mirrors probes.HoneypotSimConfig but lives in the
// config package to avoid importing probes into config (and creating an
// import cycle when validate_ranges ranges over probe-specific bounds).
type HoneypotSimYAML struct {
	Enabled            bool   `yaml:"enabled"`
	SimulationContract string `yaml:"simulation_contract"`
	TimeoutMs          int    `yaml:"timeout_ms"`
}
