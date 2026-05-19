package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"crypto-sniping-bot/database/engines/postgres"
	"crypto-sniping-bot/internal/ai"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/app/logging"
	"crypto-sniping-bot/internal/app/web"
	"crypto-sniping-bot/internal/modules/execution"
	"crypto-sniping-bot/internal/modules/execution_solana"
	"crypto-sniping-bot/internal/modules/ingestion_solana"
	"crypto-sniping-bot/internal/modules/learning"
	"crypto-sniping-bot/internal/modules/price_oracle"
	"crypto-sniping-bot/internal/modules/probes"
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

	startTime := time.Now().UTC()

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

	// Phase 3 (recovery): wire Pyth on-chain SOL/USD price feed.
	// Disabled when no RPC client OR no pyth account configured —
	// ingestion then falls back to cfg.Solana.SolEstimatedPriceUsd.
	var solUsdSource ingestion_solana.SolUsdSource
	if solanaRPCClient != nil && cfg.Solana.PythSolUsdAccount != "" {
		fetcher := &solanaPythFetcher{client: solanaRPCClient}
		provider := price_oracle.NewSolUsdProvider(fetcher, price_oracle.SolUsdConfig{
			Pubkey:     cfg.Solana.PythSolUsdAccount,
			TTL:        time.Duration(cfg.Solana.PythCacheTTLSeconds) * time.Second,
			StaleAfter: time.Duration(cfg.Solana.PythStaleAfterSeconds) * time.Second,
		})
		solUsdSource = &pythSolUsdShim{provider: provider, logger: logger}
		logger.Info("pyth_sol_usd_enabled",
			"account", cfg.Solana.PythSolUsdAccount,
			"ttl_seconds", cfg.Solana.PythCacheTTLSeconds,
		)
	}

	// Wire Telegram dispatcher (event bus → outbound) and poller (inbound commands).
	rescanTrigger := make(chan struct{}, 1)
	if dispatcher, poller := buildTelegramComponents(db, logger, cfg, startTime, rescanTrigger); dispatcher != nil {
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
	// Residual-risk #4: when probes.enabled, insert the optional probes
	// stage between market_data and data_quality. The probes worker
	// emits "market_data_enriched" events; DQ subscribes to that type
	// instead of the raw "market_data_event". When disabled, neither
	// the probes worker nor the routing change is applied — the
	// pipeline is identical to its pre-probes wiring.
	dqInputType := "market_data_event"
	if cfg.Probes.Enabled {
		probeList := buildMarketProbes(ctx, cfg, solanaRPCClient, solUsdSource, logger)
		probeWorker := workers.NewMarketProbesWorker(db, probeList, logger)
		if cfg.NameDedup.Enabled && len(cfg.NameDedup.KnownTokens) > 0 {
			probeWorker.WithNameDedup(cfg.NameDedup.KnownTokens)
			logger.Info("market_probes_name_dedup_enabled",
				"known_token_count", len(cfg.NameDedup.KnownTokens),
			)
		}
		if cfg.Probes.MaxProbesPerHour > 0 {
			probeWorker.WithProbeRateLimit(cfg.Probes.MaxProbesPerHour)
			logger.Info("market_probes_rate_limit_enabled",
				"max_probes_per_hour", cfg.Probes.MaxProbesPerHour,
			)
		}
		orch.RegisterStage(
			"market_probes_worker",
			probeWorker,
			"market_data_event",
		)
		dqInputType = workers.MarketDataEnrichedEventType
		logger.Info("market_probes_enabled", "probe_count", len(probeList), "dq_input", dqInputType)
	}
	orch.RegisterStage("dq_worker", workers.NewDataQualityWorker(db, cfg, logger), dqInputType)
	featuresWorker := workers.NewFeaturesWorker(db, cfg, logger)
	featuresWorker.HydrateBaselines(ctx) // residual-risk #1: load persisted ring buffers
	go featuresWorker.RunBaselinePersistence(ctx)
	orch.RegisterStage("features_worker", featuresWorker, "data_quality_event")
	edgeWorker := workers.NewEdgeWorker(db, cfg, logger)
	edgeWorker.HydrateBaselines(ctx)
	go edgeWorker.RunBaselinePersistence(ctx)
	orch.RegisterStage("edge_worker", edgeWorker, "feature_event")
	orch.RegisterStage("probability_worker", workers.NewProbabilityWorker(db, cfg, logger), "probability_feature_event")
	orch.RegisterStage("slippage_worker", workers.NewSlippageWorker(db, cfg, logger), "slippage_feature_event")
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
	// GAP-02 fix: wire a real price client so TP/SL/trailing stops can fetch
	// live token prices. DEXScreenerPriceClient uses the free public API and
	// returns priceNative (price in chain-native token) — same unit as EntryPrice.
	priceClient := rpc.NewDEXScreenerPriceClient(logger)
	go func() {
		if err := workers.RunPositionPoll(ctx, db, cfg, priceClient, logger); err != nil && err != ctx.Err() {
			logger.Error("position_poll_failed", "error", err)
		}
	}()

	// Rescan worker — Phase 10 (Layer 0.5). Disabled unless cfg.Rescan.Enabled.
	// Re-emits market_data_event for tokens in configured age bands so the
	// MOMENTUM_EDGE path can capture alpha that NEW_LAUNCH_EDGE missed.
	go func() {
		if err := workers.RunRescan(ctx, db, cfg, logger, rescanTrigger); err != nil && err != ctx.Err() {
			logger.Error("rescan_worker_exited", "error", err)
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

	// Learning recorder — builds LearningRecordDTOs from exited positions and
	// enriches FP/FN records with AI loss explanations (fail-open when AI disabled).
	var lossExplainer *learning.LossExplainer
	if cfg.AIEnrichment.Enabled && cfg.AIEnrichment.LossExplainer.Enabled {
		explainAICfg := ai.Config{
			Enabled:          true,
			Endpoint:         cfg.AIEnrichment.Endpoint,
			Model:            cfg.AIEnrichment.Model,
			TimeoutMs:        cfg.AIEnrichment.TimeoutMs,
			MaxRetries:       cfg.AIEnrichment.MaxRetries,
			MaxResponseBytes: cfg.AIEnrichment.MaxResponseBytes,
			RateLimitPerMin:  cfg.AIEnrichment.RateLimitPerMin,
			MaxPromptChars:   cfg.AIEnrichment.MaxPromptChars,
		}
		explainClient, explainErr := ai.NewGroqClient(explainAICfg, logger)
		if explainErr != nil {
			logger.Warn("loss_explainer_ai_skip", "reason", explainErr.Error())
		} else {
			explainClient.StartRateLimiter()
			lossExplainer = learning.NewLossExplainer(explainClient, logger)
			logger.Info("loss_explainer_registered", "model", explainAICfg.Model)
		}
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if err := workers.RunLearningRecord(ctx, db, cfg, lossExplainer, logger); err != nil && err != ctx.Err() {
				logger.Error("learning_recorder_failed", "error", err)
			}
			select {
			case <-time.After(100 * time.Millisecond):
			case <-ctx.Done():
				return
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

	// Alpha aggregator — derives per-market slippage α from realized fills
	// (residual risk #3 closure). Periodic; populates slippage_alpha_calibrations.
	go func() {
		if err := workers.RunAlphaAggregator(ctx, db, cfg, logger); err != nil && err != ctx.Err() {
			logger.Error("alpha_aggregator_failed", "error", err)
		}
	}()

	// Solana ingestion — subscribes to Raydium v4 + PumpFun program logs and
	// emits market_data_event DTOs into the shared pipeline.
	// Gracefully noops when cfg.Solana.Programs is empty (Solana not configured)
	// or when no RPC client is injected (nil = no-op until a client is wired).
	go func() {
		if err := workers.RunIngestionSolana(ctx, db, cfg, solanaClient, solUsdSource, logger); err != nil && err != ctx.Err() {
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

// solanaPythFetcher adapts *rpc.SolanaClient → price_oracle.AccountFetcher.
// Kept in cmd/ to avoid an internal/rpc → internal/modules import cycle.
type solanaPythFetcher struct {
	client *rpc.SolanaClient
}

func (f *solanaPythFetcher) GetAccountInfo(ctx context.Context, pubkey, commitment string) (*price_oracle.RawAccount, error) {
	acct, err := f.client.GetAccountInfo(ctx, pubkey, commitment)
	if err != nil {
		return nil, err
	}
	if acct == nil || len(acct.Data) == 0 {
		return nil, nil
	}
	return &price_oracle.RawAccount{DataB64: acct.Data[0], Slot: acct.Slot}, nil
}

// pythSolUsdShim adapts *price_oracle.SolUsdProvider → ingestion_solana.SolUsdSource.
// Returns ok=false on any error so callers fall back to the static
// SolEstimatedPriceUsd estimate (no fabricated prices).
type pythSolUsdShim struct {
	provider *price_oracle.SolUsdProvider
	logger   *slog.Logger
}

func (s *pythSolUsdShim) SolUsd(ctx context.Context) (float64, bool) {
	q, err := s.provider.Get(ctx)
	if err != nil || q == nil || q.Price <= 0 {
		if err != nil && s.logger != nil {
			s.logger.Debug("pyth_sol_usd_unavailable", "error", err)
		}
		return 0, false
	}
	return q.Price, true
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
	// Derive default market from the first Solana program in cfg.Solana.Programs.
	// Falls back to "solana-raydium-v4" when the programs list is empty.
	defaultMarket := "solana-raydium-v4"
	if len(cfg.Solana.Programs) > 0 {
		switch cfg.Solana.Programs[0].Family {
		case "raydium-v4", "raydium":
			defaultMarket = "solana-raydium-v4"
		case "pumpfun":
			defaultMarket = "solana-pumpfun"
		}
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

// buildMarketProbes constructs the probe slice for the optional market
// probes worker. Only probes whose per-probe `enabled` flag is true are
// instantiated. Returns an empty slice when no probes are configured —
// the worker then operates as pass-through.
//
// ctx must be the application-lifetime context; it is forwarded to
// each probe's StartEviction goroutine so eviction loops stop cleanly
// when the server shuts down.
//
// solanaRPC and solUsd may be nil — Solana probes are skipped when the
// Solana RPC client is unconfigured. The slice ordering defines probe
// execution order; later probes see the enrichment from earlier probes.
func buildMarketProbes(ctx context.Context, cfg *config.Config, solanaRPC *rpc.SolanaClient, solUsd ingestion_solana.SolUsdSource, logger *slog.Logger) []probes.MarketProbe {
	var out []probes.MarketProbe
	if cfg.Probes.HoneypotSim.Enabled {
		// rpc client is not yet wired here — production deployments
		// must inject a real eth_call client. Until then the probe
		// either short-circuits (empty SimulationContract) or returns
		// an error from a nil-rpc check.
		hpCfg := probes.HoneypotSimConfig{
			Enabled:            cfg.Probes.HoneypotSim.Enabled,
			SimulationContract: cfg.Probes.HoneypotSim.SimulationContract,
			TimeoutMs:          cfg.Probes.HoneypotSim.TimeoutMs,
		}
		out = append(out, probes.NewHoneypotSimProbe(nil, hpCfg, logger))
	}

	// Solana enrichment probes — require a live RPC client. Without one
	// they would always error; skip registration entirely.
	if solanaRPC != nil {
		probeRPC := &solanaProbeRPCAdapter{client: solanaRPC}
		// DAS probe runs first (fast path): a single Helius getAsset call
		// populates supply and social links so the downstream probes
		// (pumpfun_lp, metadata) can skip their own RPC/HTTP calls when
		// the corresponding *Known flag is already true.
		if cfg.Probes.SolanaDASAsset.Enabled {
			das := probes.NewSolanaDASAssetProbe(probeRPC, probes.SolanaDASAssetConfig{
				Enabled:   true,
				TimeoutMs: cfg.Probes.SolanaDASAsset.TimeoutMs,
			}, logger)
			das.StartEviction(ctx)
			out = append(out, das)
		}
		if cfg.Probes.SolanaAuthorities.Enabled {
			out = append(out, probes.NewSolanaAuthoritiesProbe(probeRPC, probes.SolanaAuthoritiesConfig{
				Enabled:    true,
				TimeoutMs:  cfg.Probes.SolanaAuthorities.TimeoutMs,
				Commitment: cfg.Probes.SolanaAuthorities.Commitment,
			}, logger))
		}
		if cfg.Probes.SolanaPumpfunLp.Enabled {
			out = append(out, probes.NewSolanaPumpfunLpProbe(probeRPC, &solUsdProbeAdapter{src: solUsd}, probes.SolanaPumpfunLpConfig{
				Enabled:    true,
				TimeoutMs:  cfg.Probes.SolanaPumpfunLp.TimeoutMs,
				Commitment: cfg.Probes.SolanaPumpfunLp.Commitment,
			}, logger))
		}
		if cfg.Probes.SolanaHolderDist.Enabled {
			hd := probes.NewSolanaHolderDistProbe(probeRPC, probes.SolanaHolderDistConfig{
				Enabled:    true,
				TimeoutMs:  cfg.Probes.SolanaHolderDist.TimeoutMs,
				Commitment: cfg.Probes.SolanaHolderDist.Commitment,
				TopK:       cfg.Probes.SolanaHolderDist.TopK,
			}, logger)
			hd.StartEviction(ctx)
			out = append(out, hd)
		}
	}

	// EVM enrichment probes — dormant until a concrete EVM RPC client is
	// wired in cmd/server.go (see TODO at honeypot_sim above). The probe
	// itself returns an error when invoked with a nil RPC, so do not
	// register it until a non-nil RPC implementation is available.
	// cfg.Probes.EVMPairReserves.Enabled is intentionally ignored here
	// until the EVM RPC client is wired — keeping the block as a TODO.
	// TODO(evm-rpc): pass real RPC client and enable registration.

	// Metadata probe — HTTP only, no RPC client required. Runs for any
	// Solana token with a non-empty MetadataURI. Registered last so it
	// sees the enriched DTO produced by the RPC probes above.
	if cfg.Probes.SolanaMetadata.Enabled {
		out = append(out, probes.NewSolanaMetadataProbe(nil, probes.SolanaMetadataConfig{
			Enabled:      true,
			TimeoutMs:    cfg.Probes.SolanaMetadata.TimeoutMs,
			IPFSGateway:  cfg.Probes.SolanaMetadata.IPFSGateway,
			MaxBodyBytes: cfg.Probes.SolanaMetadata.MaxBodyBytes,
		}, logger))
	}

	// Creator reputation probe — queries pump.fun's public API to populate
	// CreatorPrevTokenCount with ground-truth launch history. Closes the
	// cold-start serial-launcher gap (BLOCKER-2): without this, a fresh DB
	// always returns count=0 for every creator regardless of real history.
	// Registered after the metadata probe so the enriched DTO already has
	// social-link fields set when creator reputation is assessed.
	if cfg.Probes.SolanaCreatorReputation.Enabled {
		out = append(out, probes.NewSolanaCreatorReputationProbe(nil, probes.SolanaCreatorReputationConfig{
			Enabled:      true,
			TimeoutMs:    cfg.Probes.SolanaCreatorReputation.TimeoutMs,
			BaseURL:      cfg.Probes.SolanaCreatorReputation.BaseURL,
			MaxBodyBytes: cfg.Probes.SolanaCreatorReputation.MaxBodyBytes,
			PageLimit:    cfg.Probes.SolanaCreatorReputation.PageLimit,
			// HeliusRPCURL enables the Helius DAS circuit-breaker fallback when
			// pump.fun is unreachable. The URL embeds the API key from the
			// SOLANA_RPC_HTTP_2 env var — never hardcoded in config files.
			HeliusRPCURL: findHeliusHTTPURL(cfg.Solana.RPCEndpoints),
		}, logger))
	}

	// AI narrative probe — registered last so it sees the fully-enriched DTO
	// (social links, creator reputation) produced by all prior probes.
	// Requires GROQ_API_KEY env var. Skipped (with a warning) when
	// the key is absent or the API client fails to initialise.
	if cfg.AIEnrichment.Enabled && cfg.AIEnrichment.NarrativeProbe.Enabled {
		aiCfg := ai.Config{
			Enabled:          true,
			Endpoint:         cfg.AIEnrichment.Endpoint,
			Model:            cfg.AIEnrichment.Model,
			TimeoutMs:        cfg.AIEnrichment.TimeoutMs,
			MaxRetries:       cfg.AIEnrichment.MaxRetries,
			MaxResponseBytes: cfg.AIEnrichment.MaxResponseBytes,
			RateLimitPerMin:  cfg.AIEnrichment.RateLimitPerMin,
			MaxPromptChars:   cfg.AIEnrichment.MaxPromptChars,
		}
		aiClient, aiErr := ai.NewGroqClient(aiCfg, logger)
		if aiErr != nil {
			logger.Warn("ai_narrative_probe_skip", "reason", aiErr.Error())
		} else {
			aiClient.StartRateLimiter()
			narrativeCfg := probes.AINarrativeConfig{
				Enabled:             true,
				MaxDescriptionChars: cfg.AIEnrichment.NarrativeProbe.MaxDescriptionChars,
				TrendingNarratives:  cfg.AIEnrichment.TrendingNarratives,
			}
			out = append(out, probes.NewAINarrativeProbe(aiClient, narrativeCfg, logger))
			logger.Info("ai_narrative_probe_registered", "model", aiCfg.Model, "rate_limit_per_min", aiCfg.RateLimitPerMin)
		}
	}

	return out
}

// findHeliusHTTPURL scans cfg.Solana.RPCEndpoints for the first Helius HTTP
// endpoint and returns its URL (which embeds the API key as ?api-key=...).
// This URL is passed to the creator-reputation probe as the Helius DAS
// circuit-breaker fallback. Returns "" when no Helius HTTP endpoint is found.
//
// Provider detection: explicit Provider=="helius" field takes precedence;
// falls back to URL substring matching ("helius-rpc.com", "helius.dev").
func findHeliusHTTPURL(endpoints []config.SolanaRPCEndpoint) string {
	for _, ep := range endpoints {
		if ep.Kind != "http" || ep.URL == "" {
			continue
		}
		provider := strings.ToLower(ep.Provider)
		urlLower := strings.ToLower(ep.URL)
		isHelius := provider == "helius" ||
			strings.Contains(urlLower, "helius-rpc.com") ||
			strings.Contains(urlLower, "helius.dev")
		if isHelius {
			return ep.URL
		}
	}
	return ""
}

// solanaProbeRPCAdapter adapts *rpc.SolanaClient → probes.SolanaProbeRPCClient.
type solanaProbeRPCAdapter struct {
	client *rpc.SolanaClient
}

func (a *solanaProbeRPCAdapter) GetAccountInfo(ctx context.Context, pubkey, commitment string) (*probes.SolanaAccountData, error) {
	acct, err := a.client.GetAccountInfo(ctx, pubkey, commitment)
	if err != nil {
		return nil, err
	}
	if acct == nil || len(acct.Data) == 0 {
		return nil, nil
	}
	return &probes.SolanaAccountData{
		DataB64: acct.Data[0],
		Owner:   acct.Owner,
		Slot:    acct.Slot,
	}, nil
}

func (a *solanaProbeRPCAdapter) GetTokenLargestAccounts(ctx context.Context, mint, commitment string) ([]probes.SolanaTokenHolder, error) {
	holders, err := a.client.GetTokenLargestAccounts(ctx, mint, commitment)
	if err != nil {
		return nil, err
	}
	out := make([]probes.SolanaTokenHolder, 0, len(holders))
	for _, h := range holders {
		out = append(out, probes.SolanaTokenHolder{
			Address:  h.Address,
			Amount:   h.Amount,
			Decimals: h.Decimals,
		})
	}
	return out, nil
}

func (a *solanaProbeRPCAdapter) GetDASAsset(ctx context.Context, mint string) (*probes.DASAsset, error) {
	asset, err := a.client.GetDASAsset(ctx, mint)
	if err != nil {
		return nil, err
	}
	if asset == nil {
		return nil, nil
	}
	return &probes.DASAsset{
		Supply:   asset.Supply,
		Decimals: asset.Decimals,
		Twitter:  asset.Twitter,
		Telegram: asset.Telegram,
		Website:  asset.Website,
		Name:     asset.Name,
		Symbol:   asset.Symbol,
	}, nil
}

// solUsdProbeAdapter bridges ingestion_solana.SolUsdSource → probes.SolUsdSource.
// Both interfaces share the same method signature; Go requires an
// explicit adapter to convert between unrelated interface types.
type solUsdProbeAdapter struct {
	src ingestion_solana.SolUsdSource
}

func (a *solUsdProbeAdapter) SolUsd(ctx context.Context) (float64, bool) {
	if a.src == nil {
		return 0, false
	}
	return a.src.SolUsd(ctx)
}
