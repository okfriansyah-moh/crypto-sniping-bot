package main

import (
	"fmt"
	"os"
	"path/filepath"

	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
)

// findConfigPath returns the path to pipeline.yaml, searching from cwd up.
func findConfigPath() (string, error) {
	// 1. Check CONFIG_PATH env var.
	if v := os.Getenv("CONFIG_PATH"); v != "" {
		return v, nil
	}

	// 2. Look for config/pipeline.yaml relative to cwd.
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get cwd: %w", err)
	}

	candidate := filepath.Join(cwd, "config", "pipeline.yaml")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}

	return "", fmt.Errorf("pipeline.yaml not found; set CONFIG_PATH or run from project root")
}

// buildDBConfig converts the app config into a database.Config.
func buildDBConfig(cfg *config.Config) database.Config {
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
