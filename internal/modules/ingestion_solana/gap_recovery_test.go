package ingestion_solana_test

// gap_recovery_test.go verifies that RecoverGap always stamps IngestedAt with
// the current wall-clock time, regardless of tx.BlockTime. When BlockTime==0
// the legacy blockTimestamp helper returned "", which caused tokens recovered
// via gap recovery to be permanently invisible to the rescan age-window query
// (NULLIF('','') IS NULL → age comparisons evaluate to NULL → excluded).

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/ingestion_solana"
)

// stubGapRPC implements ingestion_solana.SolanaRPCClient with canned responses.
type stubGapRPC struct {
	sigs []string
	txs  map[string]*ingestion_solana.TransactionResult
}

func (s *stubGapRPC) SubscribeLogs(_ context.Context, _ string) (<-chan ingestion_solana.LogsNotification, error) {
	ch := make(chan ingestion_solana.LogsNotification)
	return ch, nil
}

func (s *stubGapRPC) GetLatestBlockhash(_ context.Context, _ string) (string, uint64, error) {
	return "GapBlockhash", 999, nil
}

func (s *stubGapRPC) GetSlot(_ context.Context, _ string) (uint64, error) {
	return 600, nil
}

func (s *stubGapRPC) GetTransaction(_ context.Context, sig string) (*ingestion_solana.TransactionResult, error) {
	if tx, ok := s.txs[sig]; ok {
		return tx, nil
	}
	return nil, nil
}

func (s *stubGapRPC) GetSignaturesForAddress(_ context.Context, _ string, _, _ uint64, _ int) ([]string, error) {
	return s.sigs, nil
}

func buildGapTx(sig string, blockTime int64) *ingestion_solana.TransactionResult {
	instrData := buildPumpFunCreateData("GapToken", "GAP", "https://example.com/gap")
	return &ingestion_solana.TransactionResult{
		Signature:       sig,
		Slot:            500,
		BlockTime:       blockTime,
		RecentBlockhash: "GapBlockhash11111111111111111111111111111111",
		Instructions: []ingestion_solana.InstructionData{
			{
				ProgramID: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P",
				Index:     0,
				Data:      instrData,
				Accounts: []string{
					fixtureMint, "MintAuthority111111111111111111111111111111",
					fixtureBonding, "AssocBonding111111111111111111111111111111",
					"Global111111111111111111111111111111111111",
					"TokenMeta111111111111111111111111111111111",
					"Metadata11111111111111111111111111111111111",
					"User111111111111111111111111111111111111111",
				},
			},
		},
	}
}

func TestRecoverGap_IngestedAt_ZeroBlockTime(t *testing.T) {
	// BlockTime == 0 → legacy code produced IngestedAt="" → rescan invisible.
	// After fix: IngestedAt must be a non-empty RFC3339 wall-clock timestamp.
	const sig = "GapSig_ZeroBlockTime_111111111111111111111111"
	rpc := &stubGapRPC{
		sigs: []string{sig},
		txs:  map[string]*ingestion_solana.TransactionResult{sig: buildGapTx(sig, 0)},
	}

	var emitted []contracts.MarketDataDTO
	emit := func(_ context.Context, dto contracts.MarketDataDTO) error {
		emitted = append(emitted, dto)
		return nil
	}

	before := time.Now().UTC().Truncate(time.Second)
	_, err := ingestion_solana.RecoverGap(
		context.Background(),
		rpc,
		config.SolanaProgramConfig{
			ProgramID: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P",
			Family:    "pumpfun",
		},
		0, 600, "v1",
		emit, 1000,
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("RecoverGap error: %v", err)
	}
	if len(emitted) != 1 {
		t.Fatalf("expected 1 emitted DTO, got %d", len(emitted))
	}
	dto := emitted[0]
	if dto.IngestedAt == "" {
		t.Fatal("IngestedAt must not be empty when BlockTime==0")
	}
	ts, parseErr := time.Parse(time.RFC3339, dto.IngestedAt)
	if parseErr != nil {
		t.Fatalf("IngestedAt %q is not valid RFC3339: %v", dto.IngestedAt, parseErr)
	}
	after := time.Now().UTC().Add(time.Second)
	if ts.Before(before) || ts.After(after) {
		t.Errorf("IngestedAt %v is outside the expected [%v, %v] window", ts, before, after)
	}
}

func TestRecoverGap_IngestedAt_NonZeroBlockTime(t *testing.T) {
	// When BlockTime is valid (e.g. a token created 30 min ago), IngestedAt
	// must still reflect NOW (when our system processed the gap), not the
	// on-chain creation time. This prevents old-gap tokens from appearing
	// stale in the rescan window relative to when WE discovered them.
	const sig = "GapSig_OldBlockTime_111111111111111111111111"
	oldBlockTime := time.Now().Add(-30 * time.Minute).Unix()
	rpc := &stubGapRPC{
		sigs: []string{sig},
		txs:  map[string]*ingestion_solana.TransactionResult{sig: buildGapTx(sig, oldBlockTime)},
	}

	var emitted []contracts.MarketDataDTO
	emit := func(_ context.Context, dto contracts.MarketDataDTO) error {
		emitted = append(emitted, dto)
		return nil
	}

	before := time.Now().UTC().Truncate(time.Second)
	_, err := ingestion_solana.RecoverGap(
		context.Background(),
		rpc,
		config.SolanaProgramConfig{
			ProgramID: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P",
			Family:    "pumpfun",
		},
		0, 600, "v1",
		emit, 1000,
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("RecoverGap error: %v", err)
	}
	if len(emitted) != 1 {
		t.Fatalf("expected 1 emitted DTO, got %d", len(emitted))
	}
	dto := emitted[0]
	ts, parseErr := time.Parse(time.RFC3339, dto.IngestedAt)
	if parseErr != nil {
		t.Fatalf("IngestedAt %q is not valid RFC3339: %v", dto.IngestedAt, parseErr)
	}
	after := time.Now().UTC().Add(time.Second)
	if ts.Before(before) || ts.After(after) {
		t.Errorf("IngestedAt %v must be now-ish, not the 30-min-old block time", ts)
	}
	// BlockTimestamp should still carry the original on-chain time.
	if dto.BlockTimestamp == "" {
		t.Error("BlockTimestamp must be set from tx.BlockTime (used for chain-time analytics)")
	}
}
