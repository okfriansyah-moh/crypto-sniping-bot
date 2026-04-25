package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds the complete application configuration loaded from YAML files.
// All thresholds and tunable parameters must come from YAML — no hardcoded values.
// See docs/implementation_roadmap.md § 0.5.
type Config struct {
	Pipeline  PipelineConfig  `yaml:"pipeline"`
	Database  DatabaseConfig  `yaml:"database"`
	Worker    WorkerConfig    `yaml:"worker"`
	Logging   LoggingConfig   `yaml:"logging"`

	// SchemaVersion is set from pipeline.schema_version.
	SchemaVersion string
}

// PipelineConfig holds top-level pipeline metadata.
type PipelineConfig struct {
	SchemaVersion string `yaml:"schema_version"`
}

// DatabaseConfig holds database connection parameters.
type DatabaseConfig struct {
	Engine   string     `yaml:"engine"`
	Host     string     `yaml:"host"`
	Port     int        `yaml:"port"`
	Database string     `yaml:"database"`
	User     string     `yaml:"user"`
	SSLMode  string     `yaml:"ssl_mode"` // disable | require | verify-ca | verify-full
	Pool     PoolConfig `yaml:"pool"`
}

// PoolConfig holds connection pool settings.
type PoolConfig struct {
	MaxOpenConns        int `yaml:"max_open_conns"`
	MaxIdleConns        int `yaml:"max_idle_conns"`
	ConnMaxLifetimeSecs int `yaml:"conn_max_lifetime_seconds"`
}

// WorkerConfig holds worker loop parameters.
type WorkerConfig struct {
	IdleBackoffMs  int  `yaml:"idle_backoff_ms"`
	MaxRetryCount  int  `yaml:"max_retry_count"`
	PanicRecovery  bool `yaml:"panic_recovery"`
}

// LoggingConfig holds structured logging settings.
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// Load reads configuration from one or more YAML config files.
// Files are merged in order (later files override earlier ones for shared keys).
// Environment variables can override values: SNIPER_DB_PASSWORD overrides database.password.
// Returns an error if any required key is missing or files cannot be parsed.
func Load(paths ...string) (*Config, error) {
	if len(paths) == 0 {
		// Default: load pipeline.yaml from config/ relative to cwd.
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("config: get working directory: %w", err)
		}
		paths = []string{filepath.Join(cwd, "config", "pipeline.yaml")}
	}

	cfg := &Config{}
	for _, path := range paths {
		if err := loadFile(path, cfg); err != nil {
			return nil, err
		}
	}

	// Apply environment variable overrides.
	applyEnvOverrides(cfg)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cfg.SchemaVersion = cfg.Pipeline.SchemaVersion
	return cfg, nil
}

// Validate checks that all required configuration fields are present.
func (c *Config) Validate() error {
	var missing []string

	if c.Database.Host == "" {
		missing = append(missing, "database.host")
	}
	if c.Database.Port == 0 {
		missing = append(missing, "database.port")
	}
	if c.Database.Database == "" {
		missing = append(missing, "database.database")
	}
	if c.Database.User == "" {
		missing = append(missing, "database.user")
	}

	if len(missing) > 0 {
		return fmt.Errorf("config: missing required keys: %s", strings.Join(missing, ", "))
	}
	return nil
}

// Snapshot returns canonical JSON of all tunable config parameters.
// Used to derive the StrategyVersionID.
func (c *Config) Snapshot() ([]byte, error) {
	return json.Marshal(c)
}

// DBPassword returns the database password from SNIPER_DB_PASSWORD env var.
func (c *Config) DBPassword() string {
	return os.Getenv("SNIPER_DB_PASSWORD")
}

// Port returns the HTTP server port from PORT env var (default: 8080).
func (c *Config) Port() string {
	return getEnv("PORT", "8080")
}

func loadFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("config: parse %s: %w", path, err)
	}
	return nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("SNIPER_DB_HOST"); v != "" {
		cfg.Database.Host = v
	}
	if v := os.Getenv("SNIPER_DB_NAME"); v != "" {
		cfg.Database.Database = v
	}
	if v := os.Getenv("SNIPER_DB_USER"); v != "" {
		cfg.Database.User = v
	}
	if v := os.Getenv("SNIPER_DB_SSL_MODE"); v != "" {
		cfg.Database.SSLMode = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
