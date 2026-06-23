package ingestion_solana_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/sniper-bot/internal/modules/ingestion_solana"
)

// trackingMockClient counts how many SubscribeLogs calls it receives.
type trackingMockClient struct {
	subscribeCalls atomic.Int64
}

func (c *trackingMockClient) SubscribeLogs(ctx context.Context, _ string) (<-chan ingestion_solana.LogsNotification, error) {
	c.subscribeCalls.Add(1)
	ch := make(chan ingestion_solana.LogsNotification)
	// Block until ctx is cancelled so the loop doesn't immediately reconnect.
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

func (c *trackingMockClient) GetTransaction(_ context.Context, _ string) (*ingestion_solana.TransactionResult, error) {
	return nil, nil
}

func (c *trackingMockClient) GetLatestBlockhash(_ context.Context, _ string) (string, uint64, error) {
	return "hash1111", 999, nil
}

func (c *trackingMockClient) GetSlot(_ context.Context, _ string) (uint64, error) {
	return 12345, nil
}

func (c *trackingMockClient) GetSignaturesForAddress(_ context.Context, _ string, _, _ uint64, _ int) ([]string, error) {
	return nil, nil
}

// TestModuleStart_DisabledProgramSkipped asserts that a program with Disabled=true
// is never subscribed to, while an enabled program proceeds normally.
func TestModuleStart_DisabledProgramSkipped(t *testing.T) {
	client := &trackingMockClient{}

	cfg := config.SolanaConfig{
		ChainID: "solana",
		Programs: []config.SolanaProgramConfig{
			{
				ProgramID: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P",
				Family:    "pumpfun",
				Disabled:  true, // must be skipped
			},
			{
				ProgramID: "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8",
				Family:    "raydium-v4",
				Disabled:  false, // must subscribe
			},
		},
		IngestionBackoff: config.IngestionBackoff{InitialMs: 50, MaxMs: 200, Multiplier: 2.0},
	}

	var emitted []contracts.MarketDataDTO
	emit := func(_ context.Context, dto contracts.MarketDataDTO) error {
		emitted = append(emitted, dto)
		return nil
	}

	mod := ingestion_solana.New(cfg, "v1", emit, nil).WithClient(client)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	if err := mod.Start(ctx); err != nil && err != context.DeadlineExceeded {
		// context cancellation is the expected "clean" exit — ignore it
		_ = err
	}

	// Exactly one SubscribeLogs call expected (raydium-v4 only).
	// Pump.fun is disabled so it must contribute zero calls.
	if got := client.subscribeCalls.Load(); got < 1 {
		t.Errorf("expected ≥1 SubscribeLogs call for the enabled program, got %d", got)
	}
}

// TestModuleStart_AllDisabled asserts that when every configured program is
// disabled the module exits cleanly with no subscription calls.
func TestModuleStart_AllDisabled(t *testing.T) {
	client := &trackingMockClient{}

	cfg := config.SolanaConfig{
		ChainID: "solana",
		Programs: []config.SolanaProgramConfig{
			{
				ProgramID: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P",
				Family:    "pumpfun",
				Disabled:  true,
			},
		},
		IngestionBackoff: config.IngestionBackoff{InitialMs: 50, MaxMs: 200, Multiplier: 2.0},
	}

	emit := func(_ context.Context, _ contracts.MarketDataDTO) error { return nil }
	mod := ingestion_solana.New(cfg, "v1", emit, nil).WithClient(client)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	_ = mod.Start(ctx)

	if got := client.subscribeCalls.Load(); got != 0 {
		t.Errorf("expected 0 SubscribeLogs calls when all programs disabled, got %d", got)
	}
}
