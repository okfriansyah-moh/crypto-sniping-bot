// Package ingestion_solana implements Layer 0 DEX event ingestion for Solana.
// It subscribes to program log notifications via the Solana WebSocket API,
// fetches transaction data, normalizes pool creation and swap events into
// MarketDataDTOs, and emits them via the EventEmitter callback.
//
// Architecture invariants:
//   - This package MUST NOT import database/ — receives EventEmitter from the worker.
//   - This package MUST NOT import other modules — only contracts/ and standard libs.
//   - All normalization functions are deterministic: same input = same DTO.
//   - Chain = "solana"; market = "solana-raydium-v4" | "solana-pumpfun".
//   - EventID = SHA256("solana|" + signature + "|" + instructionIndex)[:16].
package ingestion_solana

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// EventEmitter is the callback through which the module delivers normalized
// MarketDataDTOs to the worker/orchestrator.
type EventEmitter func(ctx context.Context, dto contracts.MarketDataDTO) error

// SolanaRPCClient is the minimal interface the module requires from a Solana
// RPC provider. Defined here so tests can inject mocks without network access.
type SolanaRPCClient interface {
	// SubscribeLogs opens a logsSubscribe WebSocket subscription for the given
	// program. Sends log notifications on the returned channel until ctx is done.
	SubscribeLogs(ctx context.Context, programID string) (<-chan LogsNotification, error)

	// GetTransaction fetches the full transaction for the given signature.
	// Returns nil if not found (slot < commitment).
	GetTransaction(ctx context.Context, signature string) (*TransactionResult, error)

	// GetLatestBlockhash returns the most recent blockhash and its last-valid slot.
	GetLatestBlockhash(ctx context.Context, commitment string) (blockhash string, lastValidSlot uint64, err error)

	// GetSlot returns the current slot at the given commitment level.
	GetSlot(ctx context.Context, commitment string) (uint64, error)

	// GetSignaturesForAddress returns signatures for a program within a slot range.
	// Used during gap recovery. Returns newest-first.
	GetSignaturesForAddress(ctx context.Context, programID string, fromSlot, toSlot uint64, limit int) ([]string, error)
}

// LogsNotification is a Solana logsSubscribe or transactionSubscribe event.
type LogsNotification struct {
	Signature string
	Logs      []string
	Slot      uint64
	Err       interface{} // non-nil if the transaction failed on-chain
	// Transaction is populated by transactionSubscribe when the WS payload
	// includes the full transaction body. When non-nil, processNotification
	// normalizes in-process and skips the HTTP getTransaction call.
	Transaction *TransactionResult
}

// TransactionResult holds the decoded transaction data needed for normalization.
type TransactionResult struct {
	Signature       string
	Slot            uint64
	BlockTime       int64 // Unix timestamp
	Instructions    []InstructionData
	AccountKeys     []string
	RecentBlockhash string
}

// InstructionData holds a single instruction's program ID, accounts, and data.
type InstructionData struct {
	ProgramID string
	Accounts  []string // account public keys in instruction order
	Data      []byte   // base58-decoded instruction data
	Index     int      // instruction index within the transaction
}

// Module manages the ingestion lifecycle for Solana.
// It is a pure component — no database handles.
type Module struct {
	cfg       config.SolanaConfig
	versionID string
	emit      EventEmitter
	logger    *slog.Logger
	client    SolanaRPCClient // injected via WithClient; nil = noop

	// solUsdSource is the optional Phase-3 live SOL/USD price provider.
	// When set, NormalizePumpFunCreateFromLogs is fed the live (or
	// last-good) Pyth price; when nil or when the provider returns an
	// error, normalization falls back to cfg.SolEstimatedPriceUsd.
	solUsdSource SolUsdSource

	// preFilterReader is the optional Task-25 creator profile reader used by
	// the L0 pre-cohort filter. When nil the filter is fully disabled and all
	// tokens pass through to DQ (fail-open). Injected via WithCreatorProfileReader.
	preFilterReader CreatorProfileReader

	mu     sync.Mutex
	stopFn context.CancelFunc

	// rateLimitUntil is the Unix-nano deadline before which GetTransaction calls
	// are suppressed after receiving an RPC -32003 rate-limit error.
	// Zero means no active backoff.
	rateLimitUntil atomic.Int64

	// Creator-identity guard counters (Task 7). Aggregate across all program
	// subscriptions and persist across reconnects (module-level lifetime).
	creatorGuardTotal     atomic.Int64 // factory program ID found as CreatorAddress
	creatorGuardCorrected atomic.Int64 // corrected to event-derived fallback wallet
	creatorGuardCleared   atomic.Int64 // cleared to "" — no valid fallback available

	// preFilterDropped counts tokens dropped by the L0 pre-cohort filter (Task 25).
	// Visible in every heartbeat for observability.
	preFilterDropped atomic.Int64

	// systemMintRejected counts tokens dropped by the L0 system-mint guard (Task 14).
	systemMintRejected atomic.Int64

	// validTokenEmitted counts successful emissions where TokenAddress is not a
	// configured system/stable mint (Task 17 throughput metrics).
	validTokenEmitted atomic.Int64

	// mintPairSwapped counts emissions where ResolveTradableMint flipped a stable
	// base mint to pick the quote-side project token (Task 17).
	mintPairSwapped atomic.Int64
}

// SolUsdSource is the minimal interface the ingestion module consumes from
// the Phase-3 price oracle.  Implementations MUST return ok=false (rather
// than fabricating a number) when no usable quote is available, so the
// caller can fall back to the static config estimate without confusion.
type SolUsdSource interface {
	SolUsd(ctx context.Context) (price float64, ok bool)
}

// CreatorProfileReader is the minimal interface the ingestion module uses to
// consult the creator_profiles cache for the L0 pre-cohort filter (Task 25).
// The interface is defined here (not imported from another module) so that the
// ingestion module remains decoupled — the adapter-backed implementation is
// wired by the orchestrator.
//
// GetCount returns the total prior token launches for (chain, creator).
// When known=false (row absent or probe pending) the caller MUST fail-open
// and emit the MarketDataDTO normally — DQ remains the authoritative gate.
type CreatorProfileReader interface {
	GetCount(ctx context.Context, chain, creator string) (count int32, known bool, err error)
}

// New creates a Module ready to Start.
func New(cfg config.SolanaConfig, versionID string, emit EventEmitter, logger *slog.Logger) *Module {
	if logger == nil {
		logger = slog.Default()
	}
	ConfigureStableMints(cfg.SystemMintReject.EffectiveMints())
	return &Module{
		cfg:       cfg,
		versionID: versionID,
		emit:      emit,
		logger:    logger,
	}
}

// WithClient injects a fixed Solana RPC client. Used in tests.
func (m *Module) WithClient(c SolanaRPCClient) *Module {
	m.client = c
	return m
}

// WithSolUsdSource injects the Phase-3 live SOL/USD price provider.
// Call sites that pass nil (or never call this) get the legacy behaviour:
// LiquidityUsd derived from cfg.SolEstimatedPriceUsd.
func (m *Module) WithSolUsdSource(s SolUsdSource) *Module {
	m.solUsdSource = s
	return m
}

// WithCreatorProfileReader injects the Task-25 pre-cohort filter reader.
// When r is nil (or this method is never called) the filter is disabled and
// every token passes through to DQ unchanged (fail-open).
func (m *Module) WithCreatorProfileReader(r CreatorProfileReader) *Module {
	m.preFilterReader = r
	return m
}

// applySystemMintReject drops DTOs whose TokenAddress is a configured system/stable
// mint (WSOL, USDC, USDT). Returns true when the event must not be emitted.
func (m *Module) applySystemMintReject(dto *contracts.MarketDataDTO) bool {
	if dto == nil || !m.cfg.SystemMintReject.ShouldRejectToken(dto.TokenAddress) {
		return false
	}
	m.systemMintRejected.Add(1)
	m.logger.Debug("ingestion_system_mint_rejected",
		"chain", dto.Chain,
		"token", dto.TokenAddress,
		"market", dto.Market,
		"tx", dto.TxHash,
	)
	return true
}

// recordEmitTelemetry updates module-level throughput counters after a successful
// market_data_event emission (Task 17).
func (m *Module) recordEmitTelemetry(dto *contracts.MarketDataDTO) {
	if dto == nil {
		return
	}
	if !IsSystemMint(dto.TokenAddress) {
		m.validTokenEmitted.Add(1)
	}
	if dto.Token0Address != "" && dto.Token1Address != "" &&
		MintPairWasSwapped(dto.Token0Address, dto.Token1Address) {
		m.mintPairSwapped.Add(1)
	}
}

// txFeePayer returns the first account key (fee payer) when present.
func txFeePayer(tx *TransactionResult) string {
	if tx == nil || len(tx.AccountKeys) == 0 {
		return ""
	}
	return tx.AccountKeys[0]
}

// applyCreatorGuard enforces that dto.CreatorAddress is never a known pump.fun
// factory program ID. When a factory program is detected, it is replaced with
// fallback (when valid) or cleared. Telemetry counters are incremented and a
// structured log entry is emitted on every correction.
// fallback is the event-derived human wallet from the normalizer's source event
// (e.g. event.User / event.Creator). Pass "" when no secondary candidate exists.
func (m *Module) applyCreatorGuard(dto *contracts.MarketDataDTO, fallback string) {
	if dto == nil || !IsFactoryProgram(dto.CreatorAddress) {
		return
	}
	original := dto.CreatorAddress
	m.creatorGuardTotal.Add(1)
	resolved, unresolvable := GuardCreatorAddress(dto.CreatorAddress, fallback)
	dto.CreatorAddress = resolved
	if unresolvable {
		m.creatorGuardCleared.Add(1)
		m.logger.Warn("ingestion_creator_identity_unresolved",
			"chain", dto.Chain,
			"token", dto.TokenAddress,
			"program_id", original,
			"market", dto.Market,
			"tx", dto.TxHash,
			"note", "factory_program_as_creator_cleared",
		)
	} else {
		m.creatorGuardCorrected.Add(1)
		m.logger.Debug("ingestion_creator_identity_corrected",
			"chain", dto.Chain,
			"token", dto.TokenAddress,
			"original_program_id", original,
			"corrected_creator", resolved,
			"market", dto.Market,
			"tx", dto.TxHash,
		)
	}
}

// applyPreFilter checks the L0 pre-cohort filter (Task 25) for a DTO that has
// already passed applyCreatorGuard. Returns true when the token should be
// dropped (market_data_event MUST NOT be emitted); false when the token passes.
//
// Fail-open contract: if the reader is nil, the creator address is empty, the
// filter is disabled in config, or the profile is not yet known, the function
// returns false — DQ remains the authoritative gate and no token is silently
// lost due to a probe or DB issue.
//
// When dropping, a structured log entry keyed "ingestion_pre_filter_drop" is
// written (this is the observability "system_event" for this L0 gate) and the
// module-level preFilterDropped counter is incremented (visible in heartbeat).
func (m *Module) applyPreFilter(ctx context.Context, dto *contracts.MarketDataDTO) (dropped bool) {
	cfg := m.cfg.PreFilter
	// Fast path: filter disabled in config or threshold unset.
	if !cfg.Enabled || cfg.MaxCreatorPrevTokenCount == 0 {
		return false
	}
	// Fail-open: no reader injected.
	if m.preFilterReader == nil {
		return false
	}
	// Fail-open: no creator to check.
	if dto == nil || dto.CreatorAddress == "" {
		return false
	}

	count, known, err := m.preFilterReader.GetCount(ctx, dto.Chain, dto.CreatorAddress)
	if err != nil || !known {
		// Fail-open: probe failed or row absent — DQ handles it.
		return false
	}

	if count > cfg.MaxCreatorPrevTokenCount {
		m.preFilterDropped.Add(1)
		m.logger.Warn("ingestion_pre_filter_drop",
			"token", dto.TokenAddress,
			"creator", dto.CreatorAddress,
			"creator_total_tokens", count,
			"max_allowed", cfg.MaxCreatorPrevTokenCount,
			"reason", "creator_above_pre_filter_cap",
			"chain", dto.Chain,
			"market", dto.Market,
		)
		return true
	}
	return false
}

// resolveSolPriceUsd returns the SOL/USD price to use for the next
// normalization. It prefers the live provider, falls back to the static
// config value when the provider is absent or returns ok=false.
func (m *Module) resolveSolPriceUsd(ctx context.Context) float64 {
	if m.solUsdSource != nil {
		if price, ok := m.solUsdSource.SolUsd(ctx); ok && price > 0 {
			return price
		}
	}
	return m.cfg.SolEstimatedPriceUsd
}

// Start begins the ingestion loop for all configured programs.
// Each program gets an independent subscription goroutine with backoff reconnect.
// Blocks until ctx is cancelled.
func (m *Module) Start(ctx context.Context) error {
	if m.client == nil {
		m.logger.Info("solana_ingestion_no_client_noop")
		<-ctx.Done()
		return ctx.Err()
	}

	m.logger.Info("solana_ingestion_starting",
		"programs", len(m.cfg.Programs),
		"chain_id", m.cfg.ChainID,
	)

	innerCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.stopFn = cancel
	m.mu.Unlock()
	defer cancel()

	var wg sync.WaitGroup
	for i, prog := range m.cfg.Programs {
		if prog.Disabled {
			m.logger.Info("ingestion_program_skipped",
				"program_id", prog.ProgramID,
				"family", prog.Family,
				"reason", "disabled_in_config",
			)
			continue
		}
		prog := prog // capture
		stagger := time.Duration(i) * time.Duration(m.cfg.WsSubscribeStaggerMs) * time.Millisecond
		wg.Add(1)
		go func() {
			defer wg.Done()
			if stagger > 0 {
				select {
				case <-time.After(stagger):
				case <-innerCtx.Done():
					return
				}
			}
			m.runProgramLoop(innerCtx, prog)
		}()
	}
	wg.Wait()
	return ctx.Err()
}

// Stop signals the module to shut down gracefully.
func (m *Module) Stop() {
	m.mu.Lock()
	fn := m.stopFn
	m.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// runProgramLoop runs the subscribe-normalize-emit loop for a single program
// with exponential backoff on failure.
func (m *Module) runProgramLoop(ctx context.Context, prog config.SolanaProgramConfig) {
	attempt := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := m.runSubscribeLoop(ctx, prog); err != nil {
			if ctx.Err() != nil {
				return
			}
			attempt++
			delay := nextDelay(m.cfg.IngestionBackoff, attempt)
			m.logger.Warn("solana_ingestion_reconnecting",
				"program", prog.ProgramID,
				"family", prog.Family,
				"attempt", attempt,
				"delay_ms", delay.Milliseconds(),
				"error", err,
			)
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		} else {
			attempt = 0
		}
	}
}

// solanaHeartbeatInterval controls how often the ingestion loop logs an
// INFO-level throughput summary. Visible at LOG_LEVEL=info so operators
// can confirm events are flowing even when no qualifying creates appear.
const solanaHeartbeatInterval = 60 * time.Second

// defaultProcessingWorkers is the fallback concurrency for the per-program
// worker pool when SolanaConfig.ProcessingWorkers is unset or non-positive.
const defaultProcessingWorkers = 8

// workerCount returns the configured processing-worker count or the default.
func workerCount(cfg config.SolanaConfig) int {
	if cfg.ProcessingWorkers > 0 {
		return cfg.ProcessingWorkers
	}
	return defaultProcessingWorkers
}

// nowUTC returns the current wall-clock time formatted as RFC3339 in UTC.
// Isolated as a var so tests can override for deterministic ingest_at fields.
var nowUTC = func() string { return time.Now().UTC().Format(time.RFC3339) }

// pumpFunFactoryProgramIDs is the set of pump.fun platform-level program IDs
// that must never appear as MarketDataDTO.CreatorAddress. These identify the
// pump.fun protocol itself, not the human wallet that initiated the transaction.
// Per PRODUCTION_GATE_ANALYSIS § 3 Problem B — Task 7 ingestion guard.
var pumpFunFactoryProgramIDs = map[string]struct{}{
	"6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P": {}, // bonding-curve factory program
	"pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA": {}, // AMM graduation factory program
}

// IsFactoryProgram reports whether addr is a known pump.fun factory program ID.
// Factory programs are platform-level identities, not human wallets, and must
// never propagate downstream as a token creator identity.
func IsFactoryProgram(addr string) bool {
	_, ok := pumpFunFactoryProgramIDs[addr]
	return ok
}

// GuardCreatorAddress enforces that the creator identity is never a known
// pump.fun factory program. Returns the resolved address and whether resolution
// failed (unresolvable=true means the caller should clear the field).
//
//   - creator is not a factory program → (creator, false) — no change.
//   - creator IS factory AND fallback is a valid non-factory address → (fallback, false).
//   - creator IS factory AND no valid fallback → ("", true).
func GuardCreatorAddress(creator, fallback string) (resolved string, unresolvable bool) {
	if !IsFactoryProgram(creator) {
		return creator, false
	}
	if fallback != "" && !IsFactoryProgram(fallback) {
		return fallback, false
	}
	return "", true
}

// runSubscribeLoop opens a single logsSubscribe session and processes events.
//
// Two processing paths run inside this loop:
//
//  1. Pump.fun log-decode (fast path). When prog.Family == "pumpfun" and
//     SolanaConfig.PumpfunDecodeFromLogs is true, the CreateEvent is decoded
//     directly from the WS log payload and emitted synchronously. No
//     getTransaction RPC is issued. This eliminates the rate-limit-induced
//     backlog that previously caused events_emitted=0 heartbeats.
//
//  2. Worker-pool tx-fetch (slow path). For Raydium-V4 (and Pump.fun when the
//     decode flag is off) each notification is dispatched to a bounded
//     goroutine pool (size = SolanaConfig.ProcessingWorkers). This prevents a
//     single slow getTransaction from blocking the WS read loop and lets
//     concurrent fetches drain the rate-limiter in parallel.
func (m *Module) runSubscribeLoop(ctx context.Context, prog config.SolanaProgramConfig) error {
	// Open the subscription channel. Programs with subscription_method:
	// "transactionSubscribe" use the Helius-extended transactionSubscribe path
	// (reduces credit burn by 99.95% for high-volume programs like Raydium V4).
	// All other programs use the standard logsSubscribe path.
	var notifs <-chan LogsNotification
	var subscribeErr error

	if prog.SubscriptionMethod == "transactionSubscribe" {
		ts, ok := m.client.(TransactionSubscriber)
		if !ok {
			// Client does not implement TransactionSubscriber (e.g. a basic test
			// mock). Fall back to logsSubscribe so the program loop keeps running;
			// in production SolanaClient always satisfies TransactionSubscriber.
			m.logger.Warn("ingestion_subscribe_method_fallback",
				"program_id", prog.ProgramID,
				"family", prog.Family,
				"requested_method", "transactionSubscribe",
				"fallback", "logsSubscribe",
				"reason", "client_does_not_implement_TransactionSubscriber",
			)
			notifs, subscribeErr = m.client.SubscribeLogs(ctx, prog.ProgramID)
			if subscribeErr != nil {
				return fmt.Errorf("subscribe_logs_fallback: %w", subscribeErr)
			}
		} else {
			notifs, subscribeErr = ts.SubscribeTransactions(ctx, prog.ProgramID, prog.AccountFilter)
			if subscribeErr != nil {
				return fmt.Errorf("subscribe_transactions: %w", subscribeErr)
			}
			m.logger.Info("ingestion_subscription_method",
				"program_id", prog.ProgramID,
				"family", prog.Family,
				"method", "transactionSubscribe",
				"account_filter", prog.AccountFilter,
			)
		}
	} else {
		notifs, subscribeErr = m.client.SubscribeLogs(ctx, prog.ProgramID)
		if subscribeErr != nil {
			return fmt.Errorf("subscribe_logs: %w", subscribeErr)
		}
		m.logger.Info("ingestion_subscription_method",
			"program_id", prog.ProgramID,
			"family", prog.Family,
			"method", "logsSubscribe",
		)
	}

	var totalNotifs, failedTx, emitted atomic.Int64
	// Breakdown counters — shown in every heartbeat so operators know exactly
	// where notifications go instead of seeing only events_emitted=0.
	var nilTx, normalizeSkip, noInstrMatch, processErrors atomic.Int64
	// logFilterSkip counts notifications dropped by the log pre-filter (no RPC call made).
	var logFilterSkip atomic.Int64
	// rateLimitSkip counts notifications skipped during an active rate-limit backoff.
	var rateLimitSkip atomic.Int64
	// dtoNilSkip counts instructions where the leading tag IS a recognized
	// instruction (e.g. Raydium V4 Initialize2/SwapBaseIn/SwapBaseOut, or a
	// Pump.fun Create) but the per-family normalizer still produced nil — i.e.
	// a likely decoder bug or an account-layout mismatch worth investigating.
	var dtoNilSkip atomic.Int64
	// skippedUnknownInstruction counts instructions whose leading tag is NOT a
	// recognized opcode for this family (e.g. Raydium V4 SetParams/Withdraw,
	// Pump.fun non-Create). These are irrelevant by design — distinct from
	// decoder bugs that mis-handle a recognized opcode.
	var skippedUnknownInstruction atomic.Int64
	// raydiumInitFallbackFetch counts raydium-v4 getTransaction retries when the
	// embedded transactionSubscribe body had no matching program instructions.
	var raydiumInitFallbackFetch atomic.Int64
	// emittedFromLogs counts events produced by the Pump.fun log-decode path.
	var emittedFromLogs atomic.Int64
	// sampleSeq is incremented for every successfully-fetched notification
	// and used to gate 1-in-sampleRate INFO log lines.
	var sampleSeq atomic.Int64

	workers := workerCount(m.cfg)
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	// Drain in-flight worker goroutines before returning so counters are
	// monotonic across heartbeats and the pool does not leak on reconnect.
	defer wg.Wait()

	logPath := prog.Family == "pumpfun" && m.cfg.PumpfunDecodeFromLogs

	heartbeat := time.NewTicker(solanaHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-heartbeat.C:
			m.logger.Info("solana_ingestion_heartbeat",
				"program", prog.ProgramID,
				"family", prog.Family,
				"notifications_received", totalNotifs.Load(),
				"failed_tx", failedTx.Load(),
				"log_filter_skip", logFilterSkip.Load(),
				"rate_limit_skip", rateLimitSkip.Load(),
				"nil_tx", nilTx.Load(),
				"no_instr_match", noInstrMatch.Load(),
				"normalize_skip", normalizeSkip.Load(),
				"dto_nil_skip", dtoNilSkip.Load(),
				"skipped_unknown_instruction", skippedUnknownInstruction.Load(),
				"process_errors", processErrors.Load(),
				"in_flight", len(sem),
				"events_emitted", emitted.Load(),
				"events_emitted_from_logs", emittedFromLogs.Load(),
				"creator_guard_total", m.creatorGuardTotal.Load(),
				"creator_guard_corrected", m.creatorGuardCorrected.Load(),
				"creator_guard_cleared", m.creatorGuardCleared.Load(),
				"pre_filter_dropped", m.preFilterDropped.Load(),
				"system_mint_rejected", m.systemMintRejected.Load(),
				"valid_token_emitted", m.validTokenEmitted.Load(),
				"mint_pair_swapped", m.mintPairSwapped.Load(),
				"raydium_init_fallback_fetch", raydiumInitFallbackFetch.Load(),
			)

		case notif, ok := <-notifs:
			if !ok {
				return fmt.Errorf("subscription channel closed")
			}
			totalNotifs.Add(1)
			if notif.Err != nil {
				failedTx.Add(1)
				m.logger.Debug("solana_ingestion_failed_tx",
					"signature", notif.Signature,
					"slot", notif.Slot,
				)
				continue
			}

			if logPath {
				m.handlePumpfunFromLogs(ctx, notif, prog, &emitted, &emittedFromLogs, &logFilterSkip, &processErrors)
				continue
			}

			// Slow path: log pre-filter + tx fetch + normalize.
			// Embedded WS transactions skip log heuristics — the payload is
			// already the full tx from transactionSubscribe.
			if notif.Transaction == nil && !ShouldFetchTransaction(notif, prog) {
				logFilterSkip.Add(1)
				continue
			}
			seq := sampleSeq.Add(1)

			// Acquire a worker slot. Block on the semaphore so back-pressure
			// flows naturally to the WS reader; ctx cancel unblocks promptly.
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return nil
			}
			wg.Add(1)
			go func(n LogsNotification, s int64) {
				defer wg.Done()
				defer func() { <-sem }()
				if err := m.processNotification(ctx, n, prog, s, &emitted, &nilTx, &noInstrMatch, &normalizeSkip, &rateLimitSkip, &dtoNilSkip, &skippedUnknownInstruction, &raydiumInitFallbackFetch); err != nil {
					processErrors.Add(1)
					m.logger.Warn("solana_ingestion_process_error",
						"signature", n.Signature,
						"error", err,
					)
				}
			}(notif, seq)
		}
	}
}

// pathLabel returns the heartbeat path tag corresponding to the active
// processing mode.
func pathLabel(logPath bool) string {
	if logPath {
		return "logs_only"
	}
	return "tx_fetch"
}

// handlePumpfunFromLogs processes a logsSubscribe notification entirely from
// its log payload. No getTransaction RPC is issued. The function is
// synchronous; it executes on the WS reader goroutine because log decoding
// is CPU-bound and microsecond-fast.
func (m *Module) handlePumpfunFromLogs(
	ctx context.Context,
	notif LogsNotification,
	prog config.SolanaProgramConfig,
	emitted, emittedFromLogs, logFilterSkip, processErrors *atomic.Int64,
) {
	event, err := DecodePumpFunCreateFromLogs(notif.Logs)
	if err != nil {
		processErrors.Add(1)
		m.logger.Warn("solana_pumpfun_log_decode_error",
			"signature", notif.Signature,
			"slot", notif.Slot,
			"error", err,
		)
		return
	}
	if event == nil {
		// Most notifications are buys/sells/withdraws — no CreateEvent.
		logFilterSkip.Add(1)
		return
	}
	dto := NormalizePumpFunCreateFromLogs(notif.Signature, notif.Slot, event, m.versionID, nowUTC(),
		m.cfg.PumpfunVirtualSolLamports, m.resolveSolPriceUsd(ctx))
	if dto == nil {
		return
	}
	// Guard: factory program IDs must never propagate as creator identity.
	// event.User is already in dto.CreatorAddress; pass "" as no separate
	// fallback exists when the event-derived user IS the factory program.
	m.applyCreatorGuard(dto, "")
	if m.applySystemMintReject(dto) {
		return
	}
	// Pre-cohort filter (Task 25): drop serial launchers at L0 before probe calls.
	// Fail-open: if reader unavailable or creator unknown, emit normally.
	if m.applyPreFilter(ctx, dto) {
		return
	}
	if err := m.emit(ctx, *dto); err != nil {
		processErrors.Add(1)
		m.logger.Warn("solana_ingestion_emit_error",
			"signature", notif.Signature,
			"error", err,
		)
		return
	}
	emitted.Add(1)
	emittedFromLogs.Add(1)
	m.recordEmitTelemetry(dto)
	m.logger.Info("solana_ingestion_emitted",
		"event_id", dto.EventID,
		"trace_id", dto.TraceID,
		"version_id", dto.VersionID,
		"market", dto.Market,
		"token", dto.TokenAddress,
		"symbol", dto.Symbol,
		"name", dto.Name,
		"tx", notif.Signature,
		"slot", notif.Slot,
		"path", "logs_only",
	)
}

// solanaLogSampleRate controls how often activity is sampled to INFO.
// Every sampleRate-th successfully-fetched notification produces one INFO line
// so operators see real traffic without log flooding.
const solanaLogSampleRate int64 = 100

// processNotification fetches (or reuses embedded) transaction data and emits DTOs.
// seq is the monotonically increasing counter used for 1-in-sampleRate sampling.
func (m *Module) processNotification(
	ctx context.Context,
	notif LogsNotification,
	prog config.SolanaProgramConfig,
	seq int64,
	emitted, nilTx, noInstrMatch, normalizeSkip, rateLimitSkip, dtoNilSkip, skippedUnknownInstruction, raydiumInitFallbackFetch *atomic.Int64,
) error {
	var tx *TransactionResult
	var err error
	txSource := "fetched"

	if notif.Transaction != nil {
		tx = notif.Transaction
		txSource = "ws_tx"
	} else {
		tx, err = m.fetchTransactionRateLimited(ctx, notif.Signature, prog.Family, rateLimitSkip)
		if err != nil {
			return err
		}
		if tx == nil {
			nilTx.Add(1)
			if seq%solanaLogSampleRate == 0 {
				m.logger.Info("solana_tx_sample",
					"family", prog.Family,
					"signature", notif.Signature,
					"slot", notif.Slot,
					"result", "nil_tx",
					"note", "1-in-100 sample: tx not yet at commitment",
				)
			}
			return nil
		}
	}

	if seq%solanaLogSampleRate == 0 {
		m.logger.Info("solana_tx_sample",
			"family", prog.Family,
			"signature", notif.Signature,
			"slot", tx.Slot,
			"instructions", len(tx.Instructions),
			"result", txSource,
			"note", "1-in-100 sample",
		)
	}

	processTx := func(tx *TransactionResult) (int, error) {
		instrMatched := 0
		for _, instr := range tx.Instructions {
			if instr.ProgramID != prog.ProgramID {
				continue
			}
			instrMatched++
			var dto *contracts.MarketDataDTO
			var normErr error

			switch prog.Family {
			case "pumpfun":
				dto, normErr = NormalizePumpFunCreate(tx, instr, m.versionID)
			case "raydium-v4":
				res := NormalizeRaydiumV4Instruction(tx, instr, m.versionID)
				switch res.Kind {
				case RaydiumV4KindUnknown:
					skippedUnknownInstruction.Add(1)
					continue
				case RaydiumV4KindSwapBaseIn, RaydiumV4KindSwapBaseOut:
					skippedUnknownInstruction.Add(1)
					continue
				}
				dto, normErr = res.DTO, res.Err
			case "pumpfun-amm":
				dto, normErr = NormalizePumpFunAMMCreatePool(tx, instr, m.versionID)
			case "raydium-clmm":
				dto, normErr = NormalizeRaydiumCLMMCreatePool(tx, instr, m.versionID)
			case "orca-whirlpool":
				dto, normErr = NormalizeOrcaWhirlpoolInitPool(tx, instr, m.versionID)
			case "meteora-dlmm":
				dto, normErr = NormalizeMeteoraDLMMInitLbPair(tx, instr, m.versionID)
			default:
				continue
			}
			if normErr != nil {
				normalizeSkip.Add(1)
				m.logger.Debug("solana_ingestion_normalize_skip",
					"family", prog.Family,
					"signature", notif.Signature,
					"instr_index", instr.Index,
					"reason", normErr,
				)
				if seq%solanaLogSampleRate == 0 {
					m.logger.Info("solana_tx_sample",
						"family", prog.Family,
						"signature", notif.Signature,
						"instr_index", instr.Index,
						"result", "normalize_skip",
						"reason", normErr,
						"note", "1-in-100 sample: most skips are swaps, not pool-inits/creates",
					)
				}
				continue
			}
			if dto == nil {
				dtoNilSkip.Add(1)
				continue
			}
			m.applyCreatorGuard(dto, txFeePayer(tx))
			if m.applySystemMintReject(dto) {
				continue
			}
			if m.applyPreFilter(ctx, dto) {
				continue
			}

			if err := m.emit(ctx, *dto); err != nil {
				return instrMatched, fmt.Errorf("emit %s: %w", dto.EventID, err)
			}
			emitted.Add(1)
			m.recordEmitTelemetry(dto)
			m.logger.Info("solana_ingestion_emitted",
				"event_id", dto.EventID,
				"trace_id", dto.TraceID,
				"version_id", dto.VersionID,
				"market", dto.Market,
				"token", dto.TokenAddress,
				"symbol", dto.Symbol,
				"name", dto.Name,
				"tx", notif.Signature,
				"slot", notif.Slot,
			)
		}
		return instrMatched, nil
	}

	instrMatched, err := processTx(tx)
	if err != nil {
		return err
	}

	// Task 16: embedded transactionSubscribe bodies may omit CPI inner instructions.
	// When logs suggest Initialize2 but ws_tx had no program match, retry via HTTP.
	if instrMatched == 0 && prog.Family == "raydium-v4" && txSource == "ws_tx" && LogsSuggestRaydiumPoolInit(notif.Logs) {
		fallbackTx, fetchErr := m.fetchTransactionRateLimited(ctx, notif.Signature, prog.Family, rateLimitSkip)
		if fetchErr != nil {
			return fetchErr
		}
		if fallbackTx != nil {
			raydiumInitFallbackFetch.Add(1)
			m.logger.Debug("solana_raydium_init_fallback_fetch",
				"signature", notif.Signature,
				"ws_instructions", len(tx.Instructions),
				"fetched_instructions", len(fallbackTx.Instructions),
			)
			matched, procErr := processTx(fallbackTx)
			if procErr != nil {
				return procErr
			}
			if matched > 0 {
				instrMatched = matched
			}
		}
	}

	if instrMatched == 0 {
		noInstrMatch.Add(1)
	}
	return nil
}

// fetchTransactionRateLimited calls GetTransaction respecting the module rate-limit
// circuit breaker and RPC error truncation invariant.
func (m *Module) fetchTransactionRateLimited(
	ctx context.Context,
	signature, family string,
	rateLimitSkip *atomic.Int64,
) (*TransactionResult, error) {
	if m.client == nil {
		return nil, nil
	}
	if until := m.rateLimitUntil.Load(); until > 0 && time.Now().UnixNano() < until {
		rateLimitSkip.Add(1)
		return nil, nil
	}

	tx, err := m.client.GetTransaction(ctx, signature)
	if err != nil {
		if IsRateLimitError(err) {
			backoff := rateLimitBackoff(m.cfg)
			until := time.Now().Add(backoff).UnixNano()
			m.rateLimitUntil.Store(until)
			rateLimitSkip.Add(1)
			m.logger.Warn("solana_rate_limit_backoff",
				"family", family,
				"backoff_s", int(backoff.Seconds()),
				"note", "getTransaction quota exhausted; suppressing calls until backoff expires",
			)
			return nil, nil
		}
		return nil, fmt.Errorf("get_transaction %s: %s", signature, truncateRPCError(err))
	}
	return tx, nil
}

// truncateRPCError caps RPC error strings before they surface in logs or returns.
func truncateRPCError(err error) string {
	if err == nil {
		return ""
	}
	const maxLen = 200
	msg := err.Error()
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen]
}

// ShouldFetchTransaction returns false when the notification log content makes
// it certain the transaction is NOT a pool-init or token-create instruction,
// allowing the module to skip the GetTransaction RPC call entirely.
//
// PumpFun is an Anchor program that logs the instruction name verbatim:
//
//	"Program log: Instruction: Create"   → pool/token creation (fetch)
//	"Program log: Instruction: Buy"      → swap (skip)
//	"Program log: Instruction: Sell"     → swap (skip)
//	"Program log: Instruction: Withdraw" → LP action (skip)
//
// Raydium V4 is not an Anchor program and does not log instruction names.
// However, it uses the ray_log! macro to emit packed event data for every
// swap (SwapBaseIn, SwapBaseOut), deposit, and withdraw operation via
// "Program log: ray_log: <base58data>". The Initialize2 (new pool creation)
// instruction does NOT emit ray_log: entries — it produces no AmmInfo event.
// Filtering on ray_log: presence eliminates >99% of swap notifications before
// issuing a getTransaction call (fail-open: no ray_log: → fetch).
func ShouldFetchTransaction(notif LogsNotification, prog config.SolanaProgramConfig) bool {
	switch prog.Family {
	case "pumpfun":
		for _, l := range notif.Logs {
			if strings.Contains(l, "Instruction: Create") {
				return true
			}
		}
		// No "Instruction: Create" found — this is a swap/buy/sell, skip it.
		return false
	// P4: PumpFun AMM emits "Instruction: CreatePool" for graduation events.
	case "pumpfun-amm":
		for _, l := range notif.Logs {
			if strings.Contains(l, "Instruction: CreatePool") {
				return true
			}
		}
		return false
	// P4: Anchor programs with known create-pool instruction names.
	// Filter by log prefix to avoid getTransaction on swap notifications.
	case "raydium-clmm":
		for _, l := range notif.Logs {
			if strings.Contains(l, "Instruction: CreatePool") {
				return true
			}
		}
		return false
	case "orca-whirlpool":
		for _, l := range notif.Logs {
			if strings.Contains(l, "Instruction: InitializePool") {
				return true
			}
		}
		return false
	case "meteora-dlmm":
		for _, l := range notif.Logs {
			if strings.Contains(l, "Instruction: InitializeLbPair") {
				return true
			}
		}
		return false
	case "raydium-v4":
		// Raydium V4 emits "Program log: ray_log: ..." for every swap
		// (SwapBaseIn / SwapBaseOut), deposit, and withdraw via the ray_log!
		// macro. Initialize2 (pool creation) is the only instruction that does
		// NOT emit ray_log: entries. Filtering on ray_log: presence eliminates
		// >99% of Helius getTransaction calls that would otherwise be wasted on
		// swaps and discarded after normalization.
		//
		// Fail-open: if no ray_log: is present, we might be looking at an
		// Initialize2 or a rare admin instruction — fetch and let the normalizer
		// decide. A missed swap fetch is cheaper than a missed pool creation.
		for _, l := range notif.Logs {
			if strings.Contains(l, "ray_log:") {
				return false // swap / deposit / withdraw — skip getTransaction
			}
		}
		return true // no ray_log: → likely Initialize2, fetch
	default:
		// Unknown families: fetch and let normalize decide.
		return true
	}
}

// IsRateLimitError returns true when the error is an RPC -32003 daily quota error.
func IsRateLimitError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "-32003")
}

// rateLimitBackoff returns the configured circuit-breaker cooldown.
// Falls back to 60 seconds when not configured.
func rateLimitBackoff(cfg config.SolanaConfig) time.Duration {
	ms := cfg.RateLimitBackoffMs
	if ms <= 0 {
		ms = 60_000
	}
	return time.Duration(ms) * time.Millisecond
}
