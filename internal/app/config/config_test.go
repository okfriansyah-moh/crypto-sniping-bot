package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"crypto-sniping-bot/internal/app/config"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func minimalValidConfig() *config.Config {
	return &config.Config{
		Database: config.DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			Database: "sniper",
			User:     "sniper",
		},
	}
}

func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "pipeline.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp yaml: %v", err)
	}
	return path
}

// ── Config.Validate ───────────────────────────────────────────────────────────

func TestValidate_AllRequiredFields_Passes(t *testing.T) {
	// Arrange
	cfg := minimalValidConfig()

	// Act
	err := cfg.Validate()

	// Assert
	if err != nil {
		t.Errorf("expected nil error for valid config, got: %v", err)
	}
}

func TestValidate_MissingHost_Fails(t *testing.T) {
	// Arrange
	cfg := minimalValidConfig()
	cfg.Database.Host = ""

	// Act / Assert
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for missing database.host")
	}
}

func TestValidate_MissingPort_Fails(t *testing.T) {
	// Arrange
	cfg := minimalValidConfig()
	cfg.Database.Port = 0

	// Act / Assert
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for missing database.port")
	}
}

func TestValidate_MissingDatabase_Fails(t *testing.T) {
	// Arrange
	cfg := minimalValidConfig()
	cfg.Database.Database = ""

	// Act / Assert
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for missing database.database")
	}
}

func TestValidate_MissingUser_Fails(t *testing.T) {
	// Arrange
	cfg := minimalValidConfig()
	cfg.Database.User = ""

	// Act / Assert
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for missing database.user")
	}
}

// ── Config.Snapshot ───────────────────────────────────────────────────────────

func TestSnapshot_Deterministic(t *testing.T) {
	// Arrange
	cfg := minimalValidConfig()

	// Act
	snap1, err1 := cfg.Snapshot()
	snap2, err2 := cfg.Snapshot()

	// Assert
	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected errors: %v, %v", err1, err2)
	}
	if string(snap1) != string(snap2) {
		t.Error("Snapshot is non-deterministic")
	}
}

func TestSnapshot_NonEmpty(t *testing.T) {
	// Arrange
	cfg := minimalValidConfig()

	// Act
	snap, err := cfg.Snapshot()

	// Assert
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap) == 0 {
		t.Error("Snapshot returned empty bytes")
	}
}

// ── Config.Port ───────────────────────────────────────────────────────────────

func TestPort_Default(t *testing.T) {
	// Arrange: ensure PORT is unset
	t.Setenv("PORT", "")
	cfg := minimalValidConfig()

	// Act
	port := cfg.Port()

	// Assert
	if port != "8080" {
		t.Errorf("expected default port 8080, got %q", port)
	}
}

func TestPort_OverriddenByEnv(t *testing.T) {
	// Arrange
	t.Setenv("PORT", "9090")
	cfg := minimalValidConfig()

	// Act
	port := cfg.Port()

	// Assert
	if port != "9090" {
		t.Errorf("expected 9090 from env, got %q", port)
	}
}

// ── Config.DBPassword ─────────────────────────────────────────────────────────

func TestDBPassword_Empty_WhenEnvUnset(t *testing.T) {
	// Arrange
	t.Setenv("SNIPER_DB_PASSWORD", "")
	cfg := minimalValidConfig()

	// Act / Assert
	if pw := cfg.DBPassword(); pw != "" {
		t.Errorf("expected empty password, got %q", pw)
	}
}

func TestDBPassword_ReturnsEnvValue(t *testing.T) {
	// Arrange
	t.Setenv("SNIPER_DB_PASSWORD", "s3cret")
	cfg := minimalValidConfig()

	// Act / Assert
	if pw := cfg.DBPassword(); pw != "s3cret" {
		t.Errorf("expected s3cret, got %q", pw)
	}
}

// ── config.Load ───────────────────────────────────────────────────────────────

func TestLoad_ValidYAML_Passes(t *testing.T) {
	// Arrange
	yaml := `
pipeline:
  schema_version: "0.1.0"
database:
  engine: postgres
  host: localhost
  port: 5432
  database: sniper
  user: sniper
  ssl_mode: disable
worker:
  idle_backoff_ms: 100
logging:
  level: info
  format: json
`
	path := writeTempYAML(t, yaml)

	// Act
	cfg, err := config.Load(path)

	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.Host != "localhost" {
		t.Errorf("expected host localhost, got %q", cfg.Database.Host)
	}
	if cfg.SchemaVersion != "0.1.0" {
		t.Errorf("expected schema_version 0.1.0, got %q", cfg.SchemaVersion)
	}
}

func TestLoad_MissingFile_ReturnsError(t *testing.T) {
	// Act
	_, err := config.Load("/nonexistent/path/pipeline.yaml")

	// Assert
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoad_InvalidYAML_ReturnsError(t *testing.T) {
	// Arrange
	path := writeTempYAML(t, `{invalid yaml:::`)

	// Act
	_, err := config.Load(path)

	// Assert
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestLoad_MissingRequiredKey_ReturnsError(t *testing.T) {
	// Arrange: valid YAML but missing required database fields
	yaml := `
pipeline:
  schema_version: "0.1.0"
logging:
  level: info
  format: json
`
	path := writeTempYAML(t, yaml)

	// Act
	_, err := config.Load(path)

	// Assert
	if err == nil {
		t.Error("expected validation error for missing database fields")
	}
}

func TestLoad_EnvOverride_AppliedAfterFile(t *testing.T) {
	// Arrange
	yaml := `
database:
  host: original-host
  port: 5432
  database: sniper
  user: sniper
`
	path := writeTempYAML(t, yaml)
	t.Setenv("SNIPER_DB_HOST", "override-host")

	// Act
	cfg, err := config.Load(path)

	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.Host != "override-host" {
		t.Errorf("expected override-host from env, got %q", cfg.Database.Host)
	}
}
