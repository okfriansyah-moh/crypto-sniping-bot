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

	"crypto-sniping-bot/backend-dashboard/internal/api"
	"crypto-sniping-bot/backend-dashboard/internal/auth"
	"crypto-sniping-bot/shared/database/engines/postgres"
	"crypto-sniping-bot/internal/app/bootstrap"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/app/logging"
)

// serve.go — dashboard API process: DB + HTTP listener with auth/CORS middleware.

func runServe() {
	dashCfg, err := config.LoadDashboardConfig()
	if err != nil {
		slog.Error("dashboard_config_load_failed", "error", err)
		os.Exit(1)
	}

	cfgPath, err := bootstrap.FindPipelineConfigPath()
	if err != nil {
		slog.Error("pipeline_config_not_found", "error", err)
		os.Exit(1)
	}

	pipelineCfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("pipeline_config_load_failed", "error", err)
		os.Exit(1)
	}

	logger := logging.New(pipelineCfg.Logging.Level, pipelineCfg.Logging.Format)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	startTime := time.Now().UTC()

	db := postgres.New(logger)
	if err := db.Initialize(ctx, bootstrap.BuildDBConfig(pipelineCfg)); err != nil {
		logger.Error("db_connect_failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := db.Close(shutdownCtx); err != nil {
			logger.Warn("db_close_failed", "error", err)
		}
	}()

	mux := http.NewServeMux()
	api.Register(mux, api.Deps{
		DB:           db,
		PipelineCfg:  pipelineCfg,
		DashboardCfg: dashCfg,
		StartTime:    startTime,
	})
	handler := auth.WrapHandler(mux, dashCfg)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", dashCfg.ListenPort),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("dashboard_http_listen",
			"addr", srv.Addr,
			"auth_fail_closed", config.DashboardAuthFailClosed(),
			"cors_origins", dashCfg.CorsAllowedOrigins,
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("dashboard_http_failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("dashboard_http_shutdown_failed", "error", err)
	}

	logger.Info("dashboard_shutdown", "signal", ctx.Err().Error())
}
