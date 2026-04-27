// Package execution_solana implements Layer 8 for Solana transactions.
// It submits and confirms Solana transactions using ed25519 keypairs and
// the Solana JSON-RPC API. Pure module — no database imports.
//
// Architecture invariants:
//   - No imports from database/ — returns ExecutionResultDTO to the worker.
//   - Wallet sharding: keypair selected by hash(tokenAddress) % len(keypairs).
//   - Idempotency: each AllocationDTO.ExecutionID maps to exactly one signature.
//   - Max send attempts: bounded by config (hard max 5).
//   - Blockhash expiry: retry with fresh blockhash on BLOCKHASH_NOT_FOUND error.
package execution_solana

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// SolanaClient is the minimal interface the module requires from a Solana RPC node.
type SolanaClient interface {
	// SendTransaction submits a base64-encoded signed transaction.
	// Returns the transaction signature on success.
	SendTransaction(ctx context.Context, txBase64 string) (string, error)

	// GetSignatureStatus polls for confirmation of a signature.
	// Returns nil if the signature is not yet confirmed.
	GetSignatureStatus(ctx context.Context, signature string) (*SignatureStatus, error)

	// GetLatestBlockhash returns the most recent blockhash and last-valid slot.
	GetLatestBlockhash(ctx context.Context, commitment string) (blockhash string, lastValidSlot uint64, err error)

	// GetSlot returns the current confirmed slot.
	GetSlot(ctx context.Context, commitment string) (uint64, error)
}

// SignatureStatus is the confirmation status for a submitted signature.
type SignatureStatus struct {
	Slot               uint64
	Confirmations      *int64 // nil = finalized
	Err                interface{}
	ConfirmationStatus string // processed | confirmed | finalized
}

// Module is the Solana execution engine.
type Module struct {
	cfg           *config.SolanaExecutionConfig
	client        SolanaClient
	keys          []*Keypair
	defaultMarket string // "solana-raydium-v4" | "solana-pumpfun"
	logger        *slog.Logger
}

// New creates a new Solana execution Module.
// keypairs must have at least one entry.
// defaultMarket is used when the allocation doesn't specify a market explicitly.
func New(cfg *config.SolanaExecutionConfig, client SolanaClient, keypairs []*Keypair, defaultMarket string, logger *slog.Logger) (*Module, error) {
	if cfg == nil {
		return nil, fmt.Errorf("execution_solana: nil config")
	}
	if client == nil {
		return nil, fmt.Errorf("execution_solana: nil solana client")
	}
	if len(keypairs) == 0 {
		return nil, fmt.Errorf("execution_solana: no keypairs provided")
	}
	if logger == nil {
		logger = slog.Default()
	}
	maxAttempts := cfg.MaxSendAttempts
	if maxAttempts > 5 {
		maxAttempts = 5 // hard cap per spec
		logger.Warn("execution_solana_max_send_attempts_capped", "cap", 5)
	}
	if defaultMarket == "" {
		defaultMarket = "solana-raydium-v4"
	}
	cfgCopy := *cfg
	cfgCopy.MaxSendAttempts = maxAttempts
	return &Module{
		cfg:           &cfgCopy,
		client:        client,
		keys:          keypairs,
		defaultMarket: defaultMarket,
		logger:        logger,
	}, nil
}

// Execute submits a Solana swap transaction for the given allocation.
// market overrides the module's defaultMarket when non-empty.
// poolAddress is the AMM pool or bonding curve; may be empty for Pump.fun.
// Returns ExecutionResultDTO with confirmed status or failure details.
func (m *Module) Execute(ctx context.Context, alloc contracts.AllocationDTO, market, poolAddress string) (contracts.ExecutionResultDTO, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	start := time.Now()

	if err := ctx.Err(); err != nil {
		return failResult(alloc, "CONTEXT_CANCELLED", now), err
	}

	if market == "" {
		market = m.defaultMarket
	}

	// Select keypair by deterministic wallet sharding.
	keypair := selectKeypair(m.keys, alloc.TokenAddress)

	// Fetch initial blockhash.
	blockhash, _, err := m.client.GetLatestBlockhash(ctx, "confirmed")
	if err != nil {
		return failResult(alloc, "BLOCKHASH_FETCH_FAILED", now), fmt.Errorf("get_blockhash: %w", err)
	}

	// Build the instruction for the target program.
	instr, err := BuildSwapInstruction(alloc, market, poolAddress, m.cfg)
	if err != nil {
		return failResult(alloc, "BUILD_INSTRUCTION_FAILED", now), fmt.Errorf("build_instruction: %w", err)
	}

	var signature string
	for attempt := 1; attempt <= m.cfg.MaxSendAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return failResult(alloc, "CONTEXT_CANCELLED", now), ctx.Err()
		default:
		}

		// Build and sign the transaction.
		txBase64, buildErr := BuildAndSignTransaction(keypair, blockhash, instr)
		if buildErr != nil {
			return failResult(alloc, "SIGN_FAILED", now), fmt.Errorf("sign_tx: %w", buildErr)
		}

		sig, sendErr := m.client.SendTransaction(ctx, txBase64)
		if sendErr != nil {
			// Blockhash expired — refresh and retry.
			if isBlockhashExpired(sendErr) && attempt < m.cfg.MaxSendAttempts {
				m.logger.Warn("execution_solana_blockhash_expired",
					"execution_id", alloc.ExecutionID,
					"attempt", attempt,
				)
				newHash, _, bhErr := m.client.GetLatestBlockhash(ctx, "confirmed")
				if bhErr == nil {
					blockhash = newHash
				}
				continue
			}
			return failResult(alloc, "SEND_FAILED", now), fmt.Errorf("send_tx attempt %d: %w", attempt, sendErr)
		}
		signature = sig
		break
	}

	if signature == "" {
		return failResult(alloc, "MAX_ATTEMPTS_EXCEEDED", now), fmt.Errorf("execution_solana: max send attempts exceeded")
	}

	// Confirm the transaction.
	slot, confirmErr := m.confirmSignature(ctx, signature)
	latencyMs := int32(time.Since(start).Milliseconds())

	if confirmErr != nil {
		return contracts.ExecutionResultDTO{
			EventID:          contracts.ContentIDFromString("exec-sol-fail:" + alloc.ExecutionID),
			TraceID:          alloc.TraceID,
			CorrelationID:    alloc.CorrelationID,
			CausationID:      alloc.EventID,
			VersionID:        alloc.VersionID,
			TokenLifecycleID: alloc.TokenLifecycleID,
			ExecutionID:      alloc.ExecutionID,
			AllocationID:     alloc.EventID,
			Status:           "failed",
			Success:          false,
			TxHash:           signature,
			BlockNumber:      slot,
			ErrorCode:        "CONFIRM_FAILED",
			LatencyMs:        latencyMs,
			CompletedAt:      now,
		}, nil
	}

	confirmedAt := time.Now().UTC().Format(time.RFC3339Nano)
	m.logger.Info("execution_solana_confirmed",
		"execution_id", alloc.ExecutionID,
		"signature", signature,
		"slot", slot,
		"latency_ms", latencyMs,
	)

	return contracts.ExecutionResultDTO{
		EventID:          contracts.ContentIDFromString("exec-sol:" + alloc.ExecutionID),
		TraceID:          alloc.TraceID,
		CorrelationID:    alloc.CorrelationID,
		CausationID:      alloc.EventID,
		VersionID:        alloc.VersionID,
		TokenLifecycleID: alloc.TokenLifecycleID,
		ExecutionID:      alloc.ExecutionID,
		AllocationID:     alloc.EventID,
		Status:           "confirmed",
		Success:          true,
		TxHash:           signature,
		BlockNumber:      slot,
		LatencyMs:        latencyMs,
		CompletedAt:      confirmedAt,
	}, nil
}

// confirmSignature polls until the signature reaches the configured commitment.
func (m *Module) confirmSignature(ctx context.Context, signature string) (uint64, error) {
	return confirmWithTimeout(ctx, m.client, signature, m.cfg)
}

// failResult builds a failed ExecutionResultDTO.
func failResult(alloc contracts.AllocationDTO, code, now string) contracts.ExecutionResultDTO {
	return contracts.ExecutionResultDTO{
		EventID:          contracts.ContentIDFromString("exec-sol-fail:" + alloc.ExecutionID + ":" + code),
		TraceID:          alloc.TraceID,
		CorrelationID:    alloc.CorrelationID,
		CausationID:      alloc.EventID,
		VersionID:        alloc.VersionID,
		TokenLifecycleID: alloc.TokenLifecycleID,
		ExecutionID:      alloc.ExecutionID,
		AllocationID:     alloc.EventID,
		Status:           "failed",
		Success:          false,
		ErrorCode:        code,
		CompletedAt:      now,
	}
}

// isBlockhashExpired returns true if the send error indicates an expired blockhash.
func isBlockhashExpired(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "BlockhashNotFound") ||
		contains(msg, "blockhash not found") ||
		contains(msg, "Blockhash not found")
}

func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
