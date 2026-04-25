package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"crypto-sniping-bot/database/engines/postgres"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/app/logging"
	"crypto-sniping-bot/internal/app/web"
	"crypto-sniping-bot/internal/orchestrator"
)

// server.go — Main daemon entry point.

func runServer() {
	cfgPath, err := findConfigPath()
	if err != nil {
		slog.Error("config_not_found", "error", err)
		os.Exit(1)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("config_load_failed", "error", err)
		os.Exit(1)
	}

	logger := logging.New(cfg.Logging.Level, cfg.Logging.Format)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db := postgres.New(logger)
	dbCfg := buildDBConfig(cfg)

	if err := db.Initialize(ctx, dbCfg); err != nil {
		logger.Error("db_connect_failed", "error", err)
		os.Exit(1)
	}
	defer db.Close(context.Background()) //nolint:errcheck

	orch, err := orchestrator.Boot(ctx, db, cfg, logger)
	if err != nil {
		logger.Error("orchestrator_boot_failed", "error", err)
		os.Exit(1)
	}

	logger.Info("orchestrator_ready", "version_id", orch.VersionID())

	// Start HTTP health server with read/write/idle timeouts to prevent
	// slowloris and slow-read denial-of-service attacks.
	addr := fmt.Sprintf(":%s", cfg.Port())
	srv := web.NewServer(cfg, logger)
	httpSrv := &http.Server{
		Addr:         addr,
		Handler:      srv.Router(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	go func() {
		logger.Info("http_server_starting", "addr", addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http_server_failed", "error", err)
		}
	}()

	// Run orchestrator (blocks until ctx cancelled).
	if err := orch.Run(ctx); err != nil && err != ctx.Err() {
		logger.Error("orchestrator_run_failed", "error", err)
		os.Exit(1)
	}

	logger.Info("server_shutdown")
}
