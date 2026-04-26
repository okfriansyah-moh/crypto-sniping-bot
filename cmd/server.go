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
	"crypto-sniping-bot/internal/workers"
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

	// Register Phase 2 pipeline stage workers.
	orch.RegisterStage("dq_worker", workers.NewDataQualityWorker(db, cfg, logger), "market_data_event")
	orch.RegisterStage("features_worker", workers.NewFeaturesWorker(db, cfg, logger), "data_quality_event")
	orch.RegisterStage("edge_worker", workers.NewEdgeWorker(db, cfg, logger), "feature_event")
	orch.RegisterStage("probability_worker", workers.NewProbabilityWorker(db, cfg, logger), "feature_event")
	orch.RegisterStage("slippage_worker", workers.NewSlippageWorker(db, cfg, logger), "feature_event")
	orch.RegisterStage("validation_worker", workers.NewValidationWorker(db, cfg, logger), "edge_event")
	orch.RegisterStage("selection_worker", workers.NewSelectionWorker(db, cfg, logger), "validated_edge_event")
	orch.RegisterStage("capital_worker", workers.NewCapitalWorker(db, cfg, logger), "selection_event")
	orch.RegisterStage("execution_worker",
		workers.NewExecutionWorker(db, cfg, nil, cfg.Capital.WalletPrivateKey, 1, "", logger),
		"allocation_event",
	)
	orch.RegisterStage("position_open_worker", workers.NewPositionOpenWorker(db, cfg, logger), "execution_result_event")

	// Position poll runs as a separate goroutine (timer-driven, not event-driven).
	go func() {
		if err := workers.RunPositionPoll(ctx, db, cfg, nil, logger); err != nil && err != ctx.Err() {
			logger.Error("position_poll_failed", "error", err)
		}
	}()

	// Latency profile emitter — periodic per-chain profile generator (Phase 4).
	latencyWorker := workers.NewLatencyWorker(db, cfg, orch.VersionID(), logger)
	go func() {
		if err := latencyWorker.Run(ctx); err != nil && err != ctx.Err() {
			logger.Error("latency_worker_failed", "error", err)
		}
	}()

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

	// Graceful HTTP shutdown: give in-flight requests 10 s to drain.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http_server_shutdown_failed", "error", err)
	}

	logger.Info("server_shutdown")
}
