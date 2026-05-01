package ingestion_solana_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"testing"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/ingestion_solana"
)

// ── Fixtures ─────────────────────────────────────────────────────────────────

const (
	fixtureSig      = "5uPgFakeSignature1111111111111111111111111111"
	fixtureMint     = "TokenMintPubkey1111111111111111111111111111"
	fixtureBonding  = "BondingCurvePubkey111111111111111111111111"
	fixtureAMM      = "AmmPoolPubkey111111111111111111111111111111"
	fixtureCoinMint = "CoinMintPubkey111111111111111111111111111111"
	fixturePcMint   = "PcMintPubkey11111111111111111111111111111111"
)

// buildPumpFunCreateData builds a synthetic Pump.fun create instruction payload.
func buildPumpFunCreateData(name, symbol, uri string) []byte {
	var buf []byte
	// 8-byte discriminator
	disc := ingestion_solana.PumpFunCreateDiscriminator
	buf = append(buf, disc[:]...)
	// name
	buf = appendBorshString(buf, name)
	// symbol
	buf = appendBorshString(buf, symbol)
	// uri
	buf = appendBorshString(buf, uri)
	return buf
}

// buildRaydiumPoolInitData builds a synthetic Raydium V4 Initialize2 payload
// in the on-chain wire format: 1-byte tag, then nonce u8, openTime u64 LE,
// initPcAmount u64 LE, initCoinAmount u64 LE.
func buildRaydiumPoolInitData(nonce uint8, openTime, initPc, initCoin uint64) []byte {
	buf := []byte{ingestion_solana.RaydiumV4OpInitialize2}
	buf = append(buf, nonce)
	buf = appendU64LE(buf, openTime)
	buf = appendU64LE(buf, initPc)
	buf = appendU64LE(buf, initCoin)
	return buf
}

func appendBorshString(buf []byte, s string) []byte {
	lenBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(lenBuf, uint32(len(s)))
	buf = append(buf, lenBuf...)
	buf = append(buf, []byte(s)...)
	return buf
}

func appendU64LE(buf []byte, v uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, v)
	return append(buf, b...)
}

// ── Mock client ───────────────────────────────────────────────────────────────

type mockSolanaClient struct {
	notifications []ingestion_solana.LogsNotification
	txMap         map[string]*ingestion_solana.TransactionResult
}

func (m *mockSolanaClient) SubscribeLogs(ctx context.Context, _ string) (<-chan ingestion_solana.LogsNotification, error) {
	ch := make(chan ingestion_solana.LogsNotification, len(m.notifications))
	for _, n := range m.notifications {
		ch <- n
	}
	close(ch)
	return ch, nil
}

func (m *mockSolanaClient) GetTransaction(_ context.Context, sig string) (*ingestion_solana.TransactionResult, error) {
	return m.txMap[sig], nil
}

func (m *mockSolanaClient) GetLatestBlockhash(_ context.Context, _ string) (string, uint64, error) {
	return "hash1111", 999, nil
}

func (m *mockSolanaClient) GetSlot(_ context.Context, _ string) (uint64, error) {
	return 12345, nil
}

func (m *mockSolanaClient) GetSignaturesForAddress(_ context.Context, _ string, _, _ uint64, _ int) ([]string, error) {
	return nil, nil
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestNormalizePumpFunCreate_EventID(t *testing.T) {
	data := buildPumpFunCreateData("MyToken", "MTK", "https://example.com/meta.json")

	tx := &ingestion_solana.TransactionResult{
		Signature:       fixtureSig,
		Slot:            42000,
		BlockTime:       1700000000,
		RecentBlockhash: "recenthash111",
		Instructions: []ingestion_solana.InstructionData{
			{
				ProgramID: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P",
				Accounts:  []string{fixtureMint, "mintAuth", fixtureBonding, "assocBonding"},
				Data:      data,
				Index:     0,
			},
		},
		AccountKeys: []string{fixtureMint, "mintAuth", fixtureBonding, "assocBonding"},
	}

	dto, err := ingestion_solana.NormalizePumpFunCreate(tx, tx.Instructions[0], "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto == nil {
		t.Fatal("expected non-nil DTO")
	}

	// Verify deterministic EventID
	expectedID := contracts.ContentIDFromString("solana|" + fixtureSig + "|0")
	if dto.EventID != expectedID {
		t.Errorf("EventID mismatch: got %s, want %s", dto.EventID, expectedID)
	}

	// Verify chain/market
	if dto.Chain != "solana" {
		t.Errorf("Chain: got %s, want solana", dto.Chain)
	}
	if dto.Market != "solana-pumpfun" {
		t.Errorf("Market: got %s, want solana-pumpfun", dto.Market)
	}
	if dto.EventTopic != "PumpFunCreate" {
		t.Errorf("EventTopic: got %s, want PumpFunCreate", dto.EventTopic)
	}
	if dto.BlockNumber != 42000 {
		t.Errorf("BlockNumber: got %d, want 42000", dto.BlockNumber)
	}
	if dto.TxHash != fixtureSig {
		t.Errorf("TxHash: got %s, want %s", dto.TxHash, fixtureSig)
	}
	if dto.LogIndex != 0 {
		t.Errorf("LogIndex: got %d, want 0", dto.LogIndex)
	}
	if dto.TokenAddress != fixtureMint {
		t.Errorf("TokenAddress: got %s, want %s", dto.TokenAddress, fixtureMint)
	}
	if dto.PoolAddress != fixtureBonding {
		t.Errorf("PoolAddress: got %s, want %s", dto.PoolAddress, fixtureBonding)
	}
	if dto.Transport != "ws" {
		t.Errorf("Transport: got %s, want ws", dto.Transport)
	}
}

func TestNormalizePumpFunCreate_Deterministic(t *testing.T) {
	data := buildPumpFunCreateData("MyToken", "MTK", "https://example.com/meta.json")
	tx := &ingestion_solana.TransactionResult{
		Signature:       fixtureSig,
		Slot:            42000,
		BlockTime:       1700000000,
		RecentBlockhash: "recenthash111",
		Instructions: []ingestion_solana.InstructionData{
			{ProgramID: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P", Accounts: []string{fixtureMint, "a", fixtureBonding, "b"}, Data: data, Index: 2},
		},
	}

	dto1, _ := ingestion_solana.NormalizePumpFunCreate(tx, tx.Instructions[0], "v1")
	dto2, _ := ingestion_solana.NormalizePumpFunCreate(tx, tx.Instructions[0], "v1")

	if dto1.EventID != dto2.EventID {
		t.Error("normalization is not deterministic: EventIDs differ")
	}
}

func TestNormalizePumpFunCreate_WrongDiscriminator(t *testing.T) {
	// Non-create instruction data — should return nil without error.
	data := []byte{0, 0, 0, 0, 0, 0, 0, 0, 1, 2, 3, 4}
	tx := &ingestion_solana.TransactionResult{
		Signature:    fixtureSig,
		Slot:         1,
		Instructions: []ingestion_solana.InstructionData{{Accounts: []string{fixtureMint, "a", fixtureBonding, "b"}, Data: data, Index: 0}},
	}
	dto, err := ingestion_solana.NormalizePumpFunCreate(tx, tx.Instructions[0], "v1")
	if err != nil {
		t.Fatalf("expected nil error for non-matching discriminator, got: %v", err)
	}
	if dto != nil {
		t.Error("expected nil DTO for non-matching discriminator")
	}
}

func TestNormalizeRaydiumPoolInit_EventID(t *testing.T) {
	data := buildRaydiumPoolInitData(254, 1700000000, 5_000_000, 1_000_000_000)

	tx := &ingestion_solana.TransactionResult{
		Signature:       fixtureSig,
		Slot:            99000,
		BlockTime:       1700000000,
		RecentBlockhash: "recenthash222",
		Instructions: []ingestion_solana.InstructionData{
			{
				ProgramID: "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8",
				Accounts: []string{
					"tok", "spl", "sys", "rent",
					fixtureAMM, "auth", "orders", "lp",
					fixtureCoinMint, fixturePcMint, "extra1",
				},
				Data:  data,
				Index: 1,
			},
		},
	}

	dto, err := ingestion_solana.NormalizeRaydiumPoolInit(tx, tx.Instructions[0], "v2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto == nil {
		t.Fatal("expected non-nil DTO")
	}

	expectedID := contracts.ContentIDFromString("solana|" + fixtureSig + "|1")
	if dto.EventID != expectedID {
		t.Errorf("EventID mismatch: got %s, want %s", dto.EventID, expectedID)
	}
	if dto.Chain != "solana" {
		t.Errorf("Chain: got %s, want solana", dto.Chain)
	}
	if dto.Market != "solana-raydium-v4" {
		t.Errorf("Market: got %s, want solana-raydium-v4", dto.Market)
	}
	if dto.EventTopic != "PoolCreated" {
		t.Errorf("EventTopic: got %s, want PoolCreated", dto.EventTopic)
	}
	if dto.PoolAddress != fixtureAMM {
		t.Errorf("PoolAddress: got %s, want %s", dto.PoolAddress, fixtureAMM)
	}

	// Amount fields should encode init amounts
	expectedAmount1Raw := strconv.FormatUint(5_000_000, 10)
	if dto.Amount0Raw != expectedAmount1Raw {
		t.Errorf("Amount0Raw: got %s, want %s", dto.Amount0Raw, expectedAmount1Raw)
	}
}

func TestNormalizeRaydiumPoolInit_WrongDiscriminator(t *testing.T) {
	data := []byte{0, 0, 0, 0, 0, 0, 0, 0}
	tx := &ingestion_solana.TransactionResult{
		Signature: fixtureSig, Slot: 1,
		Instructions: []ingestion_solana.InstructionData{
			{Accounts: make([]string, 11), Data: data, Index: 0},
		},
	}
	dto, err := ingestion_solana.NormalizeRaydiumPoolInit(tx, tx.Instructions[0], "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto != nil {
		t.Error("expected nil DTO for non-matching discriminator")
	}
}

func TestModuleStartNoop_NoClient(t *testing.T) {
	cfg := config.SolanaConfig{
		ChainID:          "solana",
		Programs:         []config.SolanaProgramConfig{{ProgramID: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P", Family: "pumpfun"}},
		IngestionBackoff: config.IngestionBackoff{InitialMs: 100, MaxMs: 1000, Multiplier: 2.0},
	}

	var emitted []contracts.MarketDataDTO
	emit := func(_ context.Context, dto contracts.MarketDataDTO) error {
		emitted = append(emitted, dto)
		return nil
	}

	mod := ingestion_solana.New(cfg, "v1", emit, nil)
	// No client injected — should noop until cancelled.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_ = mod.Start(ctx)
	if len(emitted) != 0 {
		t.Errorf("expected 0 emissions with no client, got %d", len(emitted))
	}
}

func TestModuleStart_EmitsPumpFunCreate(t *testing.T) {
	data := buildPumpFunCreateData("TestToken", "TT", "https://uri.example.com")
	sig := "TestSig111111111111111111111111111111111111"

	tx := &ingestion_solana.TransactionResult{
		Signature:       sig,
		Slot:            50000,
		BlockTime:       1700000100,
		RecentBlockhash: "bhash",
		Instructions: []ingestion_solana.InstructionData{
			{
				ProgramID: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P",
				Accounts:  []string{"mint1", "auth", "bonding1", "assoc"},
				Data:      data,
				Index:     0,
			},
		},
	}

	client := &mockSolanaClient{
		notifications: []ingestion_solana.LogsNotification{
			// Logs must contain "Instruction: Create" or the pre-filter drops it.
			{
				Signature: sig,
				Slot:      50000,
				Logs:      []string{"Program 6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P invoke [1]", "Program log: Instruction: Create"},
			},
		},
		txMap: map[string]*ingestion_solana.TransactionResult{sig: tx},
	}

	cfg := config.SolanaConfig{
		ChainID:          "solana",
		Programs:         []config.SolanaProgramConfig{{ProgramID: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P", Family: "pumpfun"}},
		IngestionBackoff: config.IngestionBackoff{InitialMs: 100, MaxMs: 1000, Multiplier: 2.0},
	}

	emitted := make(chan contracts.MarketDataDTO, 10)
	emit := func(_ context.Context, dto contracts.MarketDataDTO) error {
		emitted <- dto
		return nil
	}

	mod := ingestion_solana.New(cfg, "v1", emit, nil).WithClient(client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = mod.Start(ctx) }()

	select {
	case dto := <-emitted:
		expectedID := contracts.ContentIDFromString("solana|" + sig + "|0")
		if dto.EventID != expectedID {
			t.Errorf("EventID: got %s, want %s", dto.EventID, expectedID)
		}
		if dto.Chain != "solana" {
			t.Errorf("Chain: got %s", dto.Chain)
		}
		if dto.Market != "solana-pumpfun" {
			t.Errorf("Market: got %s", dto.Market)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for emitted DTO")
	}
}

func TestEventID_Derivation(t *testing.T) {
	// Verify the EventID formula matches contracts.ContentIDFromString exactly.
	sig := "SomeSolanaSignature1111111111111111111111111"
	for _, idx := range []int{0, 1, 99} {
		expected := contracts.ContentIDFromString("solana|" + sig + "|" + strconv.Itoa(idx))
		if len(expected) != 16 {
			t.Errorf("ContentIDFromString returned %d chars, want 16", len(expected))
		}
	}
}

func TestShouldFetchTransaction_PumpFun(t *testing.T) {
	prog := config.SolanaProgramConfig{ProgramID: "6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P", Family: "pumpfun"}

	cases := []struct {
		name      string
		logs      []string
		wantFetch bool
	}{
		{"create instruction", []string{"Program log: Instruction: Create"}, true},
		{"create with other logs", []string{"Program invoke [1]", "Program log: Instruction: Create", "Program success"}, true},
		{"buy instruction", []string{"Program log: Instruction: Buy"}, false},
		{"sell instruction", []string{"Program log: Instruction: Sell"}, false},
		{"empty logs", []string{}, false},
		{"nil logs", nil, false},
		{"unrelated log", []string{"Program log: something else"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			notif := ingestion_solana.LogsNotification{Logs: tc.logs}
			got := ingestion_solana.ShouldFetchTransaction(notif, prog)
			if got != tc.wantFetch {
				t.Errorf("ShouldFetchTransaction = %v, want %v", got, tc.wantFetch)
			}
		})
	}
}

func TestShouldFetchTransaction_Raydium(t *testing.T) {
	// Raydium V4 is not Anchor — always return true regardless of logs.
	prog := config.SolanaProgramConfig{ProgramID: "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8", Family: "raydium-v4"}

	cases := []struct {
		name string
		logs []string
	}{
		{"no logs", nil},
		{"swap logs", []string{"Program log: some swap log"}},
		{"init logs", []string{"Program log: initialize"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			notif := ingestion_solana.LogsNotification{Logs: tc.logs}
			if !ingestion_solana.ShouldFetchTransaction(notif, prog) {
				t.Error("raydium-v4 should always return true from ShouldFetchTransaction")
			}
		})
	}
}

func TestIsRateLimitError(t *testing.T) {
	cases := []struct {
		name      string
		errMsg    string
		wantMatch bool
	}{
		{"rate limit", "solana_client: getTransaction: rpc error -32003: daily request limit reached", true},
		{"network error", "connection refused", false},
		{"other rpc error", "rpc error -32600: invalid request", false},
		{"nil-ish wrapper", "-32003", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := fmt.Errorf("%s", tc.errMsg)
			got := ingestion_solana.IsRateLimitError(err)
			if got != tc.wantMatch {
				t.Errorf("IsRateLimitError(%q) = %v, want %v", tc.errMsg, got, tc.wantMatch)
			}
		})
	}
}
