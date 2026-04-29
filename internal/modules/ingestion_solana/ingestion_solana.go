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

	"crypto-sniping-bot/contracts"
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

// LogsNotification is a Solana logsSubscribe event.
type LogsNotification struct {
	Signature string
	Logs      []string
	Slot      uint64
	Err       interface{} // non-nil if the transaction failed on-chain
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

	mu     sync.Mutex
	stopFn context.CancelFunc

	// rateLimitUntil is the Unix-nano deadline before which GetTransaction calls
	// are suppressed after receiving an RPC -32003 rate-limit error.
	// Zero means no active backoff.
	rateLimitUntil atomic.Int64
}

// New creates a Module ready to Start.
func New(cfg config.SolanaConfig, versionID string, emit EventEmitter, logger *slog.Logger) *Module {
	if logger == nil {
		logger = slog.Default()
	}
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
	for _, prog := range m.cfg.Programs {
		prog := prog // capture
		wg.Add(1)
		go func() {
			defer wg.Done()
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

// runSubscribeLoop opens a single logsSubscribe session and processes events.
func (m *Module) runSubscribeLoop(ctx context.Context, prog config.SolanaProgramConfig) error {
	notifs, err := m.client.SubscribeLogs(ctx, prog.ProgramID)
	if err != nil {
		return fmt.Errorf("subscribe_logs: %w", err)
	}

	var totalNotifs, failedTx, emitted atomic.Int64
	// Breakdown counters — shown in every heartbeat so operators know exactly
	// where notifications go instead of seeing only events_emitted=0.
	var nilTx, normalizeSkip, noInstrMatch, processErrors atomic.Int64
	// logFilterSkip counts notifications dropped by the log pre-filter (no RPC call made).
	var logFilterSkip atomic.Int64
	// rateLimitSkip counts notifications skipped during an active rate-limit backoff.
	var rateLimitSkip atomic.Int64
	// sampleSeq is incremented for every successfully-fetched notification
	// and used to gate 1-in-sampleRate INFO log lines.
	var sampleSeq atomic.Int64

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
				"process_errors", processErrors.Load(),
				"events_emitted", emitted.Load(),
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
			// Log pre-filter: skip GetTransaction if log content makes it clear
			// this notification is not a pool-init/create instruction.
			if !ShouldFetchTransaction(notif, prog) {
				logFilterSkip.Add(1)
				continue
			}
			seq := sampleSeq.Add(1)
			if err := m.processNotification(ctx, notif, prog, seq, &emitted, &nilTx, &noInstrMatch, &normalizeSkip, &rateLimitSkip); err != nil {
				processErrors.Add(1)
				m.logger.Warn("solana_ingestion_process_error",
					"signature", notif.Signature,
					"error", err,
				)
			}
		}
	}
}

// solanaLogSampleRate controls how often activity is sampled to INFO.
// Every sampleRate-th successfully-fetched notification produces one INFO line
// so operators see real traffic without log flooding.
const solanaLogSampleRate int64 = 100

// processNotification fetches the full transaction and emits DTOs.
// seq is the monotonically increasing counter used for 1-in-sampleRate sampling.
func (m *Module) processNotification(
	ctx context.Context,
	notif LogsNotification,
	prog config.SolanaProgramConfig,
	seq int64,
	emitted, nilTx, noInstrMatch, normalizeSkip, rateLimitSkip *atomic.Int64,
) error {
	// Circuit breaker: skip GetTransaction during an active rate-limit backoff.
	if until := m.rateLimitUntil.Load(); until > 0 && time.Now().UnixNano() < until {
		rateLimitSkip.Add(1)
		return nil
	}

	tx, err := m.client.GetTransaction(ctx, notif.Signature)
	if err != nil {
		if IsRateLimitError(err) {
			backoff := rateLimitBackoff(m.cfg)
			until := time.Now().Add(backoff).UnixNano()
			m.rateLimitUntil.Store(until)
			rateLimitSkip.Add(1)
			m.logger.Warn("solana_rate_limit_backoff",
				"family", prog.Family,
				"backoff_s", int(backoff.Seconds()),
				"note", "getTransaction quota exhausted; suppressing calls until backoff expires",
			)
			return nil
		}
		return fmt.Errorf("get_transaction %s: %w", notif.Signature, err)
	}
	if tx == nil {
		// Transaction not yet at commitment level — normal for confirmed vs finalized.
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

	if seq%solanaLogSampleRate == 0 {
		m.logger.Info("solana_tx_sample",
			"family", prog.Family,
			"signature", notif.Signature,
			"slot", tx.Slot,
			"instructions", len(tx.Instructions),
			"result", "fetched",
			"note", "1-in-100 sample",
		)
	}

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
			dto, normErr = NormalizeRaydiumPoolInit(tx, instr, m.versionID)
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
				// Sampled visibility: tells operator most skips are swaps, not bugs.
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
			continue
		}

		if err := m.emit(ctx, *dto); err != nil {
			return fmt.Errorf("emit %s: %w", dto.EventID, err)
		}
		emitted.Add(1)
		m.logger.Info("solana_ingestion_emitted",
			"event_id", dto.EventID,
			"market", dto.Market,
			"token", dto.TokenAddress,
			"symbol", dto.Symbol,
			"name", dto.Name,
			"tx", notif.Signature,
			"slot", notif.Slot,
		)
	}
	if instrMatched == 0 {
		noInstrMatch.Add(1)
	}
	return nil
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
// Raydium V4 is not an Anchor program and does not log instruction names,
// so we cannot filter it by log content and always return true.
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
	default:
		// raydium-v4 and unknown families: fetch and let normalize decide.
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
