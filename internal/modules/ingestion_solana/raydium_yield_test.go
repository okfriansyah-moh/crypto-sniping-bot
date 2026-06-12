package ingestion_solana_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/ingestion_solana"
)

func TestLogsSuggestRaydiumPoolInit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		logs []string
		want bool
	}{
		{"empty", nil, false},
		{"initialize2 log", []string{"Program log: Initialize2"}, true},
		{"raydium invoke no ray_log", []string{
			"Program " + ingestion_solana.RaydiumV4ProgramID + " invoke [1]",
		}, true},
		{"swap ray_log", []string{"Program log: ray_log: abc"}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ingestion_solana.LogsSuggestRaydiumPoolInit(tc.logs); got != tc.want {
				t.Fatalf("LogsSuggestRaydiumPoolInit() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNormalizeRaydiumV4Instruction_SwapProducesNoDTO(t *testing.T) {
	t.Parallel()
	tx := &ingestion_solana.TransactionResult{
		Signature: "RaydiumSwapSig",
		Slot:      1,
		BlockTime: 1_700_000_000,
		Instructions: []ingestion_solana.InstructionData{{
			ProgramID: ingestion_solana.RaydiumV4ProgramID,
			Accounts:  []string{"pool", "user"},
			Data:      []byte{ingestion_solana.RaydiumV4OpSwapBaseIn, 0x01, 0x02},
			Index:     0,
		}},
	}
	res := ingestion_solana.NormalizeRaydiumV4Instruction(tx, tx.Instructions[0], "v1")
	if res.DTO != nil {
		t.Fatal("swap instruction must not produce MarketDataDTO")
	}
	if res.Kind != ingestion_solana.RaydiumV4KindSwapBaseIn {
		t.Errorf("Kind = %v, want SwapBaseIn", res.Kind)
	}
}

func TestNormalizeRaydiumPoolInit_ResolvesWSOLQuoteMint(t *testing.T) {
	t.Parallel()
	const wsol = ingestion_solana.WrappedSOLMint
	const meme = "83BxvsC93JULEAgZJ68A1TzNChtC3kPVLnvVyKPVD8Uj"
	data := realisticInitialize2Bytes()
	tx := &ingestion_solana.TransactionResult{
		Signature: "RaydiumWSOLQuoteSig",
		Slot:      1,
		BlockTime: 1_700_000_000,
		Instructions: []ingestion_solana.InstructionData{{
			ProgramID: ingestion_solana.RaydiumV4ProgramID,
			Accounts: []string{
				"tok", "spl", "sys", "rent",
				"AmmPool111", "auth", "orders", "lp",
				meme, wsol, "extra",
			},
			Data:  data,
			Index: 0,
		}},
	}
	dto, err := ingestion_solana.NormalizeRaydiumPoolInit(tx, tx.Instructions[0], "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto == nil {
		t.Fatal("expected DTO")
	}
	if dto.TokenAddress != meme {
		t.Errorf("TokenAddress = %q, want %q", dto.TokenAddress, meme)
	}
	if dto.BaseAddress != wsol {
		t.Errorf("BaseAddress = %q, want WSOL", dto.BaseAddress)
	}
}

// raydiumFallbackClient delivers an incomplete embedded tx then a full tx via GetTransaction.
type raydiumFallbackClient struct {
	notif         ingestion_solana.LogsNotification
	getTxCalls    atomic.Int64
	fullTx        *ingestion_solana.TransactionResult
}

func (c *raydiumFallbackClient) SubscribeLogs(_ context.Context, _ string) (<-chan ingestion_solana.LogsNotification, error) {
	ch := make(chan ingestion_solana.LogsNotification)
	close(ch)
	return ch, nil
}

func (c *raydiumFallbackClient) SubscribeTransactions(_ context.Context, _ string, _ string) (<-chan ingestion_solana.LogsNotification, error) {
	ch := make(chan ingestion_solana.LogsNotification, 1)
	ch <- c.notif
	close(ch)
	return ch, nil
}

func (c *raydiumFallbackClient) GetTransaction(_ context.Context, _ string) (*ingestion_solana.TransactionResult, error) {
	c.getTxCalls.Add(1)
	return c.fullTx, nil
}

func (c *raydiumFallbackClient) GetLatestBlockhash(_ context.Context, _ string) (string, uint64, error) {
	return "", 0, nil
}

func (c *raydiumFallbackClient) GetSlot(_ context.Context, _ string) (uint64, error) { return 0, nil }

func (c *raydiumFallbackClient) GetSignaturesForAddress(_ context.Context, _ string, _, _ uint64, _ int) ([]string, error) {
	return nil, nil
}

func TestRunSubscribeLoop_RaydiumV4_FallbackFetchOnIncompleteEmbeddedTx(t *testing.T) {
	const sig = "RaydiumFallbackSig111111111111111111111111111"
	data := realisticInitialize2Bytes()
	fullTx := &ingestion_solana.TransactionResult{
		Signature: sig,
		Slot:      99001,
		BlockTime: 1_700_000_000,
		Instructions: []ingestion_solana.InstructionData{{
			ProgramID: ingestion_solana.RaydiumV4ProgramID,
			Accounts: []string{
				"tok", "spl", "sys", "rent",
				"AmmPool222", "auth", "orders", "lp",
				"CoinMint222", ingestion_solana.WrappedSOLMint, "extra",
			},
			Data:  data,
			Index: 0,
		}},
	}
	incompleteTx := &ingestion_solana.TransactionResult{
		Signature:    sig,
		Slot:         99001,
		BlockTime:    1_700_000_000,
		Instructions: []ingestion_solana.InstructionData{},
	}

	client := &raydiumFallbackClient{
		notif: ingestion_solana.LogsNotification{
			Signature:   sig,
			Slot:        99001,
			Logs:        []string{"Program log: Initialize2"},
			Transaction: incompleteTx,
		},
		fullTx: fullTx,
	}

	cfg := config.SolanaConfig{
		ChainID: "solana",
		Programs: []config.SolanaProgramConfig{{
			ProgramID:          ingestion_solana.RaydiumV4ProgramID,
			Family:             "raydium-v4",
			SubscriptionMethod: "transactionSubscribe",
			AccountFilter:      "5Q544fKrFoe6tsEbD7S8EmxGTJYAKtTVhAW5Q5pge4j1",
		}},
		IngestionBackoff:  config.IngestionBackoff{InitialMs: 100, MaxMs: 1000, Multiplier: 2.0},
		ProcessingWorkers: 4,
	}

	emitted := make(chan contracts.MarketDataDTO, 1)
	mod := ingestion_solana.New(cfg, "v1", func(_ context.Context, dto contracts.MarketDataDTO) error {
		emitted <- dto
		return nil
	}, nil).WithClient(client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = mod.Start(ctx) }()

	select {
	case dto := <-emitted:
		if dto.Market != "solana-raydium-v4" {
			t.Errorf("Market = %q, want solana-raydium-v4", dto.Market)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for raydium fallback emission")
	}

	if got := client.getTxCalls.Load(); got != 1 {
		t.Errorf("expected 1 GetTransaction fallback call, got %d", got)
	}
}
