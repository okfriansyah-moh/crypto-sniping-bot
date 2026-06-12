package ingestion_solana

import (
	"context"
	"encoding/binary"
	"sync/atomic"
	"testing"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

func buildRaydiumInitialize2WireBytes() []byte {
	buf := []byte{RaydiumV4OpInitialize2, 0xFE}
	var le [8]byte
	binary.LittleEndian.PutUint64(le[:], 1_700_000_000)
	buf = append(buf, le[:]...)
	binary.LittleEndian.PutUint64(le[:], 5_000_000_000)
	buf = append(buf, le[:]...)
	binary.LittleEndian.PutUint64(le[:], 2_500_000_000_000)
	buf = append(buf, le[:]...)
	return buf
}

// txSubscribePanicClient proves transactionSubscribe notifications with an
// embedded Transaction never invoke GetTransaction.
type txSubscribePanicClient struct {
	notif      LogsNotification
	getTxCalls atomic.Int64
}

func (c *txSubscribePanicClient) SubscribeLogs(_ context.Context, _ string) (<-chan LogsNotification, error) {
	ch := make(chan LogsNotification)
	close(ch)
	return ch, nil
}

func (c *txSubscribePanicClient) SubscribeTransactions(_ context.Context, _ string, _ string) (<-chan LogsNotification, error) {
	ch := make(chan LogsNotification, 1)
	ch <- c.notif
	close(ch)
	return ch, nil
}

func (c *txSubscribePanicClient) GetTransaction(_ context.Context, _ string) (*TransactionResult, error) {
	c.getTxCalls.Add(1)
	panic("getTransaction must not be called when transactionSubscribe embeds full tx")
}

func (c *txSubscribePanicClient) GetLatestBlockhash(_ context.Context, _ string) (string, uint64, error) {
	return "", 0, nil
}

func (c *txSubscribePanicClient) GetSlot(_ context.Context, _ string) (uint64, error) { return 0, nil }

func (c *txSubscribePanicClient) GetSignaturesForAddress(_ context.Context, _ string, _, _ uint64, _ int) ([]string, error) {
	return nil, nil
}

func buildRaydiumInitialize2EmbeddedTx(sig string) *TransactionResult {
	data := buildRaydiumInitialize2WireBytes()
	return &TransactionResult{
		Signature: sig,
		Slot:      99000,
		BlockTime: 1_700_000_000,
		Instructions: []InstructionData{
			{
				ProgramID: "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8",
				Accounts: []string{
					"tok", "spl", "sys", "rent",
					"AmmPool111", "auth", "orders", "lp",
					"CoinMint111", "PcMint11111", "extra",
				},
				Data:  data,
				Index: 0,
			},
		},
	}
}

func TestRunSubscribeLoop_TransactionSubscribe_EmbeddedTx_NoGetTransaction(t *testing.T) {
	const raydiumV4ProgramID = "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8"
	const poolCreationAuthority = "5Q544fKrFoe6tsEbD7S8EmxGTJYAKtTVhAW5Q5pge4j1"
	sig := "TxSubscribeSig1111111111111111111111111111111"

	client := &txSubscribePanicClient{
		notif: LogsNotification{
			Signature:   sig,
			Slot:        99000,
			Logs:        []string{"Program log: Initialize2"},
			Transaction: buildRaydiumInitialize2EmbeddedTx(sig),
		},
	}

	cfg := config.SolanaConfig{
		ChainID: "solana",
		Programs: []config.SolanaProgramConfig{
			{
				ProgramID:          raydiumV4ProgramID,
				Family:             "raydium-v4",
				SubscriptionMethod: "transactionSubscribe",
				AccountFilter:      poolCreationAuthority,
			},
		},
		IngestionBackoff:  config.IngestionBackoff{InitialMs: 100, MaxMs: 1000, Multiplier: 2.0},
		ProcessingWorkers: 4,
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
		if dto.Market != "solana-raydium-v4" {
			t.Errorf("Market: got %s want solana-raydium-v4", dto.Market)
		}
		if dto.EventTopic != "PoolCreated" {
			t.Errorf("EventTopic: got %s want PoolCreated", dto.EventTopic)
		}
		if dto.TxHash != sig {
			t.Errorf("TxHash: got %s want %s", dto.TxHash, sig)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for emitted DTO from transactionSubscribe path")
	}

	if got := client.getTxCalls.Load(); got != 0 {
		t.Errorf("expected 0 GetTransaction calls, got %d", got)
	}
}

func TestRunSubscribeLoop_TransactionSubscribe_CPINestedInitialize2_Emits(t *testing.T) {
	const sig = "RaydiumCPISig11111111111111111111111111111"
	data := buildRaydiumInitialize2WireBytes()
	// CPI-style index: outer wrapper at 0, Raydium Initialize2 at inner offset.
	embedded := &TransactionResult{
		Signature: sig,
		Slot:      99000,
		BlockTime: 1_700_000_000,
		Instructions: []InstructionData{
			{
				ProgramID: "WrapperProgram111111111111111111111111111",
				Accounts:  []string{"payer"},
				Data:      []byte{0x00},
				Index:     0,
			},
			{
				ProgramID: RaydiumV4ProgramID,
				Accounts: []string{
					"tok", "spl", "sys", "rent",
					"AmmPoolCPI", "auth", "orders", "lp",
					"CoinMintCPI", "PcMintCPI11", "extra",
				},
				Data:  data,
				Index: 1000,
			},
		},
	}

	client := &txSubscribePanicClient{
		notif: LogsNotification{
			Signature:   sig,
			Slot:        99000,
			Logs:        []string{"Program log: Initialize2"},
			Transaction: embedded,
		},
	}

	cfg := config.SolanaConfig{
		ChainID: "solana",
		Programs: []config.SolanaProgramConfig{{
			ProgramID:          RaydiumV4ProgramID,
			Family:             "raydium-v4",
			SubscriptionMethod: "transactionSubscribe",
			AccountFilter:      "5Q544fKrFoe6tsEbD7S8EmxGTJYAKtTVhAW5Q5pge4j1",
		}},
		IngestionBackoff:  config.IngestionBackoff{InitialMs: 100, MaxMs: 1000, Multiplier: 2.0},
		ProcessingWorkers: 4,
	}

	emitted := make(chan contracts.MarketDataDTO, 1)
	mod := New(cfg, "v1", func(_ context.Context, dto contracts.MarketDataDTO) error {
		emitted <- dto
		return nil
	}, nil).WithClient(client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = mod.Start(ctx) }()

	select {
	case dto := <-emitted:
		if dto.EventTopic != "PoolCreated" {
			t.Errorf("EventTopic = %q, want PoolCreated", dto.EventTopic)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for CPI-nested Initialize2 emission")
	}
}
