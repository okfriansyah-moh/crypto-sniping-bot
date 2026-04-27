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
	"sync"
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
	Signature        string
	Slot             uint64
	BlockTime        int64  // Unix timestamp
	Instructions     []InstructionData
	AccountKeys      []string
	RecentBlockhash  string
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

// runSubscribeLoop opens a single logsSubscribe session and processes events.
func (m *Module) runSubscribeLoop(ctx context.Context, prog config.SolanaProgramConfig) error {
	notifs, err := m.client.SubscribeLogs(ctx, prog.ProgramID)
	if err != nil {
		return fmt.Errorf("subscribe_logs: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case notif, ok := <-notifs:
			if !ok {
				return fmt.Errorf("subscription channel closed")
			}
			if notif.Err != nil {
				m.logger.Debug("solana_ingestion_failed_tx",
					"signature", notif.Signature,
					"slot", notif.Slot,
				)
				continue
			}
			if err := m.processNotification(ctx, notif, prog); err != nil {
				m.logger.Warn("solana_ingestion_process_error",
					"signature", notif.Signature,
					"error", err,
				)
			}
		}
	}
}

// processNotification fetches the full transaction and emits DTOs.
func (m *Module) processNotification(ctx context.Context, notif LogsNotification, prog config.SolanaProgramConfig) error {
	tx, err := m.client.GetTransaction(ctx, notif.Signature)
	if err != nil {
		return fmt.Errorf("get_transaction %s: %w", notif.Signature, err)
	}
	if tx == nil {
		return nil // not yet finalized at commitment
	}

	for _, instr := range tx.Instructions {
		if instr.ProgramID != prog.ProgramID {
			continue
		}
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
			m.logger.Debug("solana_ingestion_normalize_skip",
				"family", prog.Family,
				"signature", notif.Signature,
				"instr_index", instr.Index,
				"reason", normErr,
			)
			continue
		}
		if dto == nil {
			continue
		}

		if err := m.emit(ctx, *dto); err != nil {
			return fmt.Errorf("emit %s: %w", dto.EventID, err)
		}
		m.logger.Debug("solana_ingestion_emitted",
			"event_id", dto.EventID,
			"market", dto.Market,
			"token", dto.TokenAddress,
		)
	}
	return nil
}
