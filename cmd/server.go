package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/app/web"
)

// server.go — HTTP server command.

func runServer() {
	cfg := config.Load()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	srv := web.NewServer(cfg, logger)

	addr := fmt.Sprintf(":%s", cfg.Port)
	slog.Info("starting server", "addr", addr)

	if err := http.ListenAndServe(addr, srv.Router()); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
