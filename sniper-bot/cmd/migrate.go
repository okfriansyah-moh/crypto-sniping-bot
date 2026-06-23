package main

import (
	"context"
	"log/slog"
	"os"

	"crypto-sniping-bot/shared/database/engines/postgres"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/app/logging"
)

// migrate.go — Database migration command.
// Usage: sniper migrate

func runMigrate() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfgPath, err := findConfigPath()
	if err != nil {
		logger.Error("config_not_found", "error", err)
		os.Exit(1)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		logger.Error("config_load_failed", "error", err)
		os.Exit(1)
	}

	log := logging.New(cfg.Logging.Level, cfg.Logging.Format)
	slog.SetDefault(log)

	ctx := context.Background()

	db := postgres.New(log)
	dbCfg := buildDBConfig(cfg)

	if err := db.Initialize(ctx, dbCfg); err != nil {
		log.Error("db_connect_failed", "error", err)
		os.Exit(1)
	}
	defer db.Close(ctx) //nolint:errcheck

	if err := db.RunMigrations(ctx); err != nil {
		log.Error("migrations_failed", "error", err)
		os.Exit(1)
	}

	log.Info("migrations_applied_successfully")
}
