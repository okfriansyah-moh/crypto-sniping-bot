package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"crypto-sniping-bot/database/engines/postgres"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/app/logging"
	"crypto-sniping-bot/internal/app/web"
	"crypto-sniping-bot/internal/modules/execution"
	"crypto-sniping-bot/internal/modules/execution_solana"
	"crypto-sniping-bot/internal/modules/ingestion_solana"
	"crypto-sniping-bot/internal/orchestrator"
	"crypto-sniping-bot/internal/rpc"
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

	// Build Solana RPC client (gracefully optional — noops when not configured).
	// The concrete *rpc.SolanaClient satisfies both ingestion_solana.SolanaRPCClient
	// and execution_solana.SolanaClient — the same transport is reused for both roles.
	var solanaClient ingestion_solana.SolanaRPCClient // nil = noop mode
	var solanaRPCClient *rpc.SolanaClient             // concrete ref for execution module
	if len(cfg.Solana.RPCEndpoints) > 0 {
		var scErr error
		solanaRPCClient, scErr = rpc.NewSolanaClient(cfg.Solana, logger)
		if scErr != nil {
			logger.Warn("solana_client_build_failed", "error", scErr)
		} else {
			solanaClient = solanaRPCClient
		}
	}

	// Wire Telegram dispatcher (event bus → outbound) and poller (inbound commands).
	if dispatcher, poller := buildTelegramComponents(db, logger); dispatcher != nil {
		go func() {
			if err := dispatcher.Run(ctx); err != nil && err != ctx.Err() {
				logger.Error("telegram_dispatcher_failed", "error", err)
			}
		}()
		go func() {
			if err := poller.Run(ctx); err != nil && err != ctx.Err() {
				logger.Error("telegram_poller_failed", "error", err)
			}
		}()
	}

	// Register pipeline stage workers (Phase 2 baseline + Phase 3 evaluation gate).
	orch.RegisterStage("dq_worker", workers.NewDataQualityWorker(db, cfg, logger), "market_data_event")
	orch.RegisterStage("features_worker", workers.NewFeaturesWorker(db, cfg, logger), "data_quality_event")
	orch.RegisterStage("edge_worker", workers.NewEdgeWorker(db, cfg, logger), "feature_event")
	orch.RegisterStage("probability_worker", workers.NewProbabilityWorker(db, cfg, logger), "feature_event")
	orch.RegisterStage("slippage_worker", workers.NewSlippageWorker(db, cfg, logger), "feature_event")
	orch.RegisterStage("validation_worker", workers.NewValidationWorker(db, cfg, logger), "edge_event")
	orch.RegisterStage("selection_worker", workers.NewSelectionWorker(db, cfg, logger), "validated_edge_event")
	orch.RegisterStage("capital_worker", workers.NewCapitalWorker(db, cfg, logger), "selection_event")
	// Build wallet shards for execution. Reads SNIPER_WALLET_N_ADDRESS / SNIPER_WALLET_N_KEY
	// env vars (N = 0,1,2,...,wallet_shard_count-1). Falls back to the single wallet from
	// config/pipeline.yaml capital.wallet_address / capital.wallet_private_key when no
	// multi-wallet env vars are present.
	walletShards := buildWalletShards(cfg)
	execWorker := workers.NewExecutionWorker(db, cfg, nil, cfg.Capital.WalletPrivateKey, 1, "", walletShards, logger)
	// Wire Solana execution path when a concrete RPC client is available.
	// Gracefully noops when solanaRPCClient is nil (Solana not configured).
	if solanaRPCClient != nil {
		if solanaExecMod := buildSolanaExecutionModule(solanaRPCClient, cfg, logger); solanaExecMod != nil {
			execWorker.WithSolanaExecutor(solanaExecMod)
		}
	}
	orch.RegisterStage("execution_worker", execWorker, "allocation_event")
	orch.RegisterStage("position_open_worker", workers.NewPositionOpenWorker(db, cfg, logger), "execution_result_event")
	// Phase 3: evaluation gate — mandatory pre-learning stage.
	// Consumes position_state_event where Status=exited.
	orch.RegisterStage("evaluation_worker", workers.NewEvaluationWorker(db, cfg, logger), "position_state_event")

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

	// Risk controller — monitors drawdown; transitions system mode (BALANCED/DEGRADED/HALTED).
	go func() {
		if err := workers.RunRiskController(ctx, db, cfg, logger); err != nil && err != ctx.Err() {
			logger.Error("risk_controller_failed", "error", err)
		}
	}()

	// Rollback watchdog — compares promoted strategy vs baseline; rolls back on degradation.
	go func() {
		interval := time.Duration(cfg.Learning.RollbackCheckIntervalSeconds) * time.Second
		if interval <= 0 {
			interval = 5 * time.Minute
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := workers.RunRollbackWatchdog(ctx, db, cfg, logger); err != nil && err != ctx.Err() {
					logger.Error("rollback_watchdog_failed", "error", err)
				}
			}
		}
	}()

	// Evaluator — aggregates learning records into windowed evaluation_events.
	go func() {
		interval := time.Duration(cfg.Learning.EvalWindowMinutes) * time.Minute
		if interval <= 0 {
			interval = 60 * time.Minute
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := workers.RunEvaluator(ctx, db, cfg, logger); err != nil && err != ctx.Err() {
					logger.Error("evaluator_failed", "error", err)
				}
			}
		}
	}()

	// Updater — consumes evaluation_events; proposes new strategy version candidates.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if err := workers.RunUpdater(ctx, db, cfg, logger); err != nil && err != ctx.Err() {
				logger.Error("updater_failed", "error", err)
			}
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return
			}
		}
	}()

	// Archive worker — moves aged processed events to events_archive.
	go func() {
		if err := workers.RunArchive(ctx, db, cfg, logger); err != nil && err != ctx.Err() {
			logger.Error("archive_worker_failed", "error", err)
		}
	}()

	// Solana ingestion — subscribes to Raydium v4 + PumpFun program logs and
	// emits market_data_event DTOs into the shared pipeline.
	// Gracefully noops when cfg.Solana.Programs is empty (Solana not configured)
	// or when no RPC client is injected (nil = no-op until a client is wired).
	go func() {
		if err := workers.RunIngestionSolana(ctx, db, cfg, solanaClient, logger); err != nil && err != ctx.Err() {
			logger.Error("solana_ingestion_failed", "error", err)
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

// buildSolanaExecutionModule constructs the Solana execution module from keypair files
// listed in cfg.Execution.Solana.WalletKeyPaths (env-expanded at config load time).
// Returns nil when no keypaths are configured or no valid keypairs can be loaded —
// the execution worker then runs in EVM-only mode without error.
func buildSolanaExecutionModule(sc *rpc.SolanaClient, cfg *config.Config, logger *slog.Logger) execution.SolanaExecutor {
	paths := cfg.Execution.Solana.WalletKeyPaths
	if len(paths) == 0 {
		logger.Info("solana_execution_disabled", "reason", "no_wallet_key_paths")
		return nil
	}

	var keypairs []*execution_solana.Keypair
	for _, p := range paths {
		if p == "" {
			continue
		}
		kp, err := execution_solana.LoadKeypair(p)
		if err != nil {
			logger.Warn("solana_keypair_load_failed", "path", p, "error", err)
			continue
		}
		keypairs = append(keypairs, kp)
	}
	if len(keypairs) == 0 {
		logger.Warn("solana_execution_disabled", "reason", "no_valid_keypairs_loaded")
		return nil
	}

	// Derive default market from the first Raydium program in cfg.Solana.Programs.
	// Falls back to "solana-raydium-v4" when the programs list is empty.
	defaultMarket := "solana-raydium-v4"
	for _, prog := range cfg.Solana.Programs {
		switch prog.Family {
		case "raydium-v4", "raydium":
			defaultMarket = "solana-raydium-v4"
		case "pumpfun":
			defaultMarket = "solana-pumpfun"
		}
		break // first program wins
	}

	mod, err := execution_solana.New(&cfg.Execution.Solana, sc, keypairs, defaultMarket, logger)
	if err != nil {
		logger.Error("solana_execution_module_init_failed", "error", err)
		return nil
	}

	logger.Info("solana_execution_ready",
		"keypair_count", len(keypairs),
		"default_market", defaultMarket,
	)
	return mod
}

// buildWalletShards constructs the wallet shard slice for the execution worker.
// It reads SNIPER_WALLET_N_ADDRESS and SNIPER_WALLET_N_KEY env vars for N in
// [0, wallet_shard_count). If none are set it falls back to the single wallet
// from config (wallet_address / wallet_private_key).
func buildWalletShards(cfg *config.Config) []execution.WalletConfig {
	shardCount := cfg.Execution.ConcurrencyLimit // re-use wallet_shard_count alias
	if shardCount <= 0 {
		shardCount = 1
	}

	var shards []execution.WalletConfig
	for i := 0; i < shardCount; i++ {
		addr := os.Getenv("SNIPER_WALLET_" + strconv.Itoa(i) + "_ADDRESS")
		key := os.Getenv("SNIPER_WALLET_" + strconv.Itoa(i) + "_KEY")
		if addr == "" || key == "" {
			break // stop at first missing shard; env vars are optional
		}
		shards = append(shards, execution.WalletConfig{
			Address:    addr,
			PrivateKey: key,
			ShardIndex: i,
		})
	}

	// Fall back to single wallet from config when no env vars are provided.
	if len(shards) == 0 && cfg.Capital.WalletAddress != "" {
		shards = []execution.WalletConfig{{
			Address:    cfg.Capital.WalletAddress,
			PrivateKey: cfg.Capital.WalletPrivateKey,
		}}
	}

	return shards
}
