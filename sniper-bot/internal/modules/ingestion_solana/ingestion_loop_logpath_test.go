package ingestion_solana

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// panicGetTxClient is a SolanaClient whose GetTransaction call panics. It
// proves the log-decode fast path never invokes RPC for pumpfun creates.
type panicGetTxClient struct {
	notifications []LogsNotification
	getTxCalls    atomic.Int64
}

func (p *panicGetTxClient) SubscribeLogs(_ context.Context, _ string) (<-chan LogsNotification, error) {
	ch := make(chan LogsNotification, len(p.notifications))
	for _, n := range p.notifications {
		ch <- n
	}
	close(ch)
	return ch, nil
}

func (p *panicGetTxClient) GetTransaction(_ context.Context, _ string) (*TransactionResult, error) {
	p.getTxCalls.Add(1)
	panic("getTransaction must not be called in pumpfun log-decode mode")
}

func (p *panicGetTxClient) GetLatestBlockhash(_ context.Context, _ string) (string, uint64, error) {
	return "", 0, nil
}

func (p *panicGetTxClient) GetSlot(_ context.Context, _ string) (uint64, error) { return 0, nil }

func (p *panicGetTxClient) GetSignaturesForAddress(_ context.Context, _ string, _, _ uint64, _ int) ([]string, error) {
	return nil, nil
}

// TestRunSubscribeLoop_PumpfunFromLogs_NoRPCCall verifies that with
// PumpfunDecodeFromLogs=true the loop:
//   - emits a MarketDataDTO derived purely from the log payload, and
//   - never calls GetTransaction (validated by panic-on-call client).
func TestRunSubscribeLoop_PumpfunFromLogs_NoRPCCall(t *testing.T) {
	mint := [32]byte{0x11}
	bonding := [32]byte{0x22}
	user := [32]byte{0x33}

	sig := "LogPathSignature11111111111111111111111111111"
	logs := []string{
		"Program 6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P invoke [1]",
		"Program log: Instruction: Create",
		buildPumpFunCreateEventLogLine(t, "LogTok", "LTK", "ipfs://qm", mint, bonding, user),
		"Program 6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P success",
	}

	client := &panicGetTxClient{
		notifications: []LogsNotification{
			{Signature: sig, Slot: 12345, Logs: logs},
		},
	}

	cfg := config.SolanaConfig{
		ChainID: "solana",
		Programs: []config.SolanaProgramConfig{
			{ProgramID: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P", Family: "pumpfun"},
		},
		IngestionBackoff:      config.IngestionBackoff{InitialMs: 100, MaxMs: 1000, Multiplier: 2.0},
		ProcessingWorkers:     4,
		PumpfunDecodeFromLogs: true,
	}

	emitted := make(chan contracts.MarketDataDTO, 4)
	emit := func(_ context.Context, dto contracts.MarketDataDTO) error {
		emitted <- dto
		return nil
	}

	mod := New(cfg, "v1", emit, nil).WithClient(client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = mod.Start(ctx) }()

	select {
	case dto := <-emitted:
		// Log path uses canonical instruction-index 0 in the EventID schema
		// so toggling pumpfun_decode_from_logs (or gap-recovery via
		// getTransaction) produces an identical ID for the same tx.
		expectedID := contracts.ContentIDFromString("solana|" + sig + "|0")
		if dto.EventID != expectedID {
			t.Errorf("EventID: got %s, want %s", dto.EventID, expectedID)
		}
		if dto.Market != "solana-pumpfun" {
			t.Errorf("Market: got %s, want solana-pumpfun", dto.Market)
		}
		if dto.EventTopic != "PumpFunCreate" {
			t.Errorf("EventTopic: got %s, want PumpFunCreate", dto.EventTopic)
		}
		if dto.TxHash != sig {
			t.Errorf("TxHash: got %s, want %s", dto.TxHash, sig)
		}
		if dto.BlockNumber != 12345 {
			t.Errorf("BlockNumber: got %d, want 12345", dto.BlockNumber)
		}
		if dto.Symbol != "LTK" || dto.Name != "LogTok" {
			t.Errorf("Symbol/Name: got %s/%s", dto.Symbol, dto.Name)
		}
		if dto.Transport != "ws" {
			t.Errorf("Transport: got %s, want ws", dto.Transport)
		}
		if dto.BaseAddress != "So11111111111111111111111111111111111111112" {
			t.Errorf("BaseAddress: got %s", dto.BaseAddress)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for emitted DTO from log-only path")
	}

	if got := client.getTxCalls.Load(); got != 0 {
		t.Errorf("expected 0 GetTransaction calls in log-decode mode, got %d", got)
	}
}

// TestRunSubscribeLoop_PumpfunFromLogs_NonCreateLogsSkipped verifies that
// notifications without a CreateEvent payload are not emitted and do not
// trigger RPC calls.
func TestRunSubscribeLoop_PumpfunFromLogs_NonCreateLogsSkipped(t *testing.T) {
	client := &panicGetTxClient{
		notifications: []LogsNotification{
			{
				Signature: "BuySig1111111111111111111111111111111111111",
				Slot:      100,
				Logs: []string{
					"Program 6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P invoke [1]",
					"Program log: Instruction: Buy",
					"Program 6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P success",
				},
			},
		},
	}

	cfg := config.SolanaConfig{
		ChainID: "solana",
		Programs: []config.SolanaProgramConfig{
			{ProgramID: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P", Family: "pumpfun"},
		},
		IngestionBackoff:      config.IngestionBackoff{InitialMs: 100, MaxMs: 1000, Multiplier: 2.0},
		ProcessingWorkers:     4,
		PumpfunDecodeFromLogs: true,
	}

	emitted := make(chan contracts.MarketDataDTO, 4)
	emit := func(_ context.Context, dto contracts.MarketDataDTO) error {
		emitted <- dto
		return nil
	}

	mod := New(cfg, "v1", emit, nil).WithClient(client)

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	_ = mod.Start(ctx)

	select {
	case dto := <-emitted:
		t.Fatalf("expected no DTO emitted for Buy logs, got %+v", dto)
	default:
	}
	if got := client.getTxCalls.Load(); got != 0 {
		t.Errorf("expected 0 GetTransaction calls, got %d", got)
	}
}
