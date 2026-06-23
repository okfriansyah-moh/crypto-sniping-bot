package bootstrap

import (
	"fmt"
	"os"

	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
)

// FindPipelineConfigPath returns the path to pipeline.yaml (CONFIG_PATH or shared/config/pipeline.yaml).
func FindPipelineConfigPath() (string, error) {
	if v := os.Getenv("CONFIG_PATH"); v != "" {
		return v, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get cwd: %w", err)
	}

	candidate := config.JoinConfigFile(cwd, "pipeline.yaml")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}

	return "", fmt.Errorf("pipeline.yaml not found; set CONFIG_PATH or run from project root")
}

// BuildDBConfig converts application YAML database settings into database.Config.
func BuildDBConfig(cfg *config.Config) database.Config {
	return database.Config{
		Engine:              cfg.Database.Engine,
		Host:                cfg.Database.Host,
		Port:                cfg.Database.Port,
		Database:            cfg.Database.Database,
		User:                cfg.Database.User,
		Password:            cfg.DBPassword(),
		SSLMode:             cfg.Database.SSLMode,
		MaxOpenConns:        cfg.Database.Pool.MaxOpenConns,
		MaxIdleConns:        cfg.Database.Pool.MaxIdleConns,
		ConnMaxLifetimeSecs: cfg.Database.Pool.ConnMaxLifetimeSecs,
	}
}
