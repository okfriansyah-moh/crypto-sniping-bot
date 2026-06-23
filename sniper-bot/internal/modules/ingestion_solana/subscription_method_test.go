package ingestion_solana_test

// subscription_method_test.go — verifies that runSubscribeLoop routes
// subscription calls based on SolanaProgramConfig.SubscriptionMethod.
//
// Tests:
//   1. TransactionSubscribe is called when subscription_method == "transactionSubscribe"
//   2. SubscribeLogs is called when subscription_method is empty (default)
//   3. When client does not implement TransactionSubscriber, falls back to SubscribeLogs
//      (no panic, warning logged internally).

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/sniper-bot/internal/modules/ingestion_solana"
)

// txSubscriberMockClient implements SolanaRPCClient AND TransactionSubscriber.
// It tracks which subscribe method was called and captures the account filter.
type txSubscriberMockClient struct {
	logsSubscribeCalls  atomic.Int64
	txSubscribeCalls    atomic.Int64
	capturedProgramID   string
	capturedAccountFilt string
}

func (c *txSubscriberMockClient) SubscribeLogs(ctx context.Context, _ string) (<-chan ingestion_solana.LogsNotification, error) {
	c.logsSubscribeCalls.Add(1)
	ch := make(chan ingestion_solana.LogsNotification)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

// SubscribeTransactions satisfies ingestion_solana.TransactionSubscriber.
func (c *txSubscriberMockClient) SubscribeTransactions(ctx context.Context, programID string, accountFilter string) (<-chan ingestion_solana.LogsNotification, error) {
	c.txSubscribeCalls.Add(1)
	c.capturedProgramID = programID
	c.capturedAccountFilt = accountFilter
	ch := make(chan ingestion_solana.LogsNotification)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

func (c *txSubscriberMockClient) GetTransaction(_ context.Context, _ string) (*ingestion_solana.TransactionResult, error) {
	return nil, nil
}

func (c *txSubscriberMockClient) GetLatestBlockhash(_ context.Context, _ string) (string, uint64, error) {
	return "hash1111", 999, nil
}

func (c *txSubscriberMockClient) GetSlot(_ context.Context, _ string) (uint64, error) {
	return 12345, nil
}

func (c *txSubscriberMockClient) GetSignaturesForAddress(_ context.Context, _ string, _, _ uint64, _ int) ([]string, error) {
	return nil, nil
}

// basicMockClientNoTS implements SolanaRPCClient only — no TransactionSubscriber.
// Used to test the fallback path when client cannot serve transactionSubscribe.
type basicMockClientNoTS struct {
	subscribeCalls atomic.Int64
}

func (c *basicMockClientNoTS) SubscribeLogs(ctx context.Context, _ string) (<-chan ingestion_solana.LogsNotification, error) {
	c.subscribeCalls.Add(1)
	ch := make(chan ingestion_solana.LogsNotification)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

func (c *basicMockClientNoTS) GetTransaction(_ context.Context, _ string) (*ingestion_solana.TransactionResult, error) {
	return nil, nil
}

func (c *basicMockClientNoTS) GetLatestBlockhash(_ context.Context, _ string) (string, uint64, error) {
	return "hash1111", 999, nil
}

func (c *basicMockClientNoTS) GetSlot(_ context.Context, _ string) (uint64, error) {
	return 12345, nil
}

func (c *basicMockClientNoTS) GetSignaturesForAddress(_ context.Context, _ string, _, _ uint64, _ int) ([]string, error) {
	return nil, nil
}

func nopEmit(_ context.Context, _ contracts.MarketDataDTO) error { return nil }

// TestModuleStart_TransactionSubscribeCalledForRaydiumV4 asserts that a program
// configured with subscription_method "transactionSubscribe" causes
// SubscribeTransactions to be called with the correct program ID and account filter,
// and that SubscribeLogs is never called for that program.
func TestModuleStart_TransactionSubscribeCalledForRaydiumV4(t *testing.T) {
	const raydiumV4ProgramID = "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8"
	const poolCreationAuthority = "5Q544fKrFoe6tsEbD7S8EmxGTJYAKtTVhAW5Q5pge4j1"

	client := &txSubscriberMockClient{}

	cfg := config.SolanaConfig{
		ChainID: "solana",
		Programs: []config.SolanaProgramConfig{
			{
				ProgramID:          raydiumV4ProgramID,
				Family:             "raydium-v4",
				Disabled:           false,
				SubscriptionMethod: "transactionSubscribe",
				AccountFilter:      poolCreationAuthority,
			},
		},
		IngestionBackoff: config.IngestionBackoff{InitialMs: 50, MaxMs: 200, Multiplier: 2.0},
	}

	mod := ingestion_solana.New(cfg, "v1", nopEmit, nil).WithClient(client)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	if err := mod.Start(ctx); err != nil && err != context.DeadlineExceeded {
		t.Logf("module exit (expected): %v", err)
	}

	// Allow the goroutine scheduler to route the subscription call.
	time.Sleep(20 * time.Millisecond)

	if got := client.txSubscribeCalls.Load(); got == 0 {
		t.Errorf("expected SubscribeTransactions to be called at least once, got %d", got)
	}
	if got := client.logsSubscribeCalls.Load(); got != 0 {
		t.Errorf("expected SubscribeLogs to NOT be called for transactionSubscribe program, got %d", got)
	}
	if client.capturedProgramID != raydiumV4ProgramID {
		t.Errorf("SubscribeTransactions programID: want %q, got %q", raydiumV4ProgramID, client.capturedProgramID)
	}
	if client.capturedAccountFilt != poolCreationAuthority {
		t.Errorf("SubscribeTransactions accountFilter: want %q, got %q", poolCreationAuthority, client.capturedAccountFilt)
	}
}

// TestModuleStart_LogsSubscribeUsedForEmptySubscriptionMethod asserts that a program
// with an empty subscription_method uses the default logsSubscribe path.
func TestModuleStart_LogsSubscribeUsedForEmptySubscriptionMethod(t *testing.T) {
	client := &txSubscriberMockClient{}

	cfg := config.SolanaConfig{
		ChainID: "solana",
		Programs: []config.SolanaProgramConfig{
			{
				ProgramID:          "pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA",
				Family:             "pumpfun-amm",
				Disabled:           false,
				SubscriptionMethod: "", // empty → logsSubscribe
			},
		},
		IngestionBackoff: config.IngestionBackoff{InitialMs: 50, MaxMs: 200, Multiplier: 2.0},
	}

	mod := ingestion_solana.New(cfg, "v1", nopEmit, nil).WithClient(client)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	if err := mod.Start(ctx); err != nil && err != context.DeadlineExceeded {
		t.Logf("module exit (expected): %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	if got := client.logsSubscribeCalls.Load(); got == 0 {
		t.Errorf("expected SubscribeLogs to be called at least once, got %d", got)
	}
	if got := client.txSubscribeCalls.Load(); got != 0 {
		t.Errorf("expected SubscribeTransactions NOT to be called for empty method, got %d", got)
	}
}

// TestModuleStart_FallbackToLogsSubscribeWhenTransactionSubscriberNotImplemented
// asserts that when a client does NOT implement TransactionSubscriber but the
// program is configured with subscription_method "transactionSubscribe", the loop
// falls back to logsSubscribe without panicking.
func TestModuleStart_FallbackToLogsSubscribeWhenTransactionSubscriberNotImplemented(t *testing.T) {
	client := &basicMockClientNoTS{}

	cfg := config.SolanaConfig{
		ChainID: "solana",
		Programs: []config.SolanaProgramConfig{
			{
				ProgramID:          "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8",
				Family:             "raydium-v4",
				Disabled:           false,
				SubscriptionMethod: "transactionSubscribe",
				AccountFilter:      "5Q544fKrFoe6tsEbD7S8EmxGTJYAKtTVhAW5Q5pge4j1",
			},
		},
		IngestionBackoff: config.IngestionBackoff{InitialMs: 50, MaxMs: 200, Multiplier: 2.0},
	}

	mod := ingestion_solana.New(cfg, "v1", nopEmit, nil).WithClient(client)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	// Must not panic — the fallback path calls SubscribeLogs instead.
	if err := mod.Start(ctx); err != nil && err != context.DeadlineExceeded {
		t.Logf("module exit (expected): %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	if got := client.subscribeCalls.Load(); got == 0 {
		t.Errorf("expected fallback SubscribeLogs to be called, got %d", got)
	}
}
