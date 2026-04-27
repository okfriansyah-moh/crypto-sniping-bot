package execution_solana_test

import (
"context"
"crypto/rand"
"encoding/base64"
"testing"
"time"

"crypto-sniping-bot/contracts"
"crypto-sniping-bot/internal/app/config"
"crypto-sniping-bot/internal/modules/execution_solana"
)

// ── Mock client ───────────────────────────────────────────────────────────────

type mockSolanaExecClient struct {
sentTxs   []string
statusMap map[string]*execution_solana.SignatureStatus
blockhash string
lastSlot  uint64
sendErr   error
statusErr error
}

func (m *mockSolanaExecClient) SendTransaction(_ context.Context, tx string) (string, error) {
if m.sendErr != nil {
return "", m.sendErr
}
m.sentTxs = append(m.sentTxs, tx)
return "Sig" + tx[:minLen(8, len(tx))], nil
}

func (m *mockSolanaExecClient) GetSignatureStatus(_ context.Context, sig string) (*execution_solana.SignatureStatus, error) {
if m.statusErr != nil {
return nil, m.statusErr
}
if s, ok := m.statusMap[sig]; ok {
return s, nil
}
return nil, nil
}

func (m *mockSolanaExecClient) GetLatestBlockhash(_ context.Context, _ string) (string, uint64, error) {
return m.blockhash, m.lastSlot, nil
}

func (m *mockSolanaExecClient) GetSlot(_ context.Context, _ string) (uint64, error) {
return m.lastSlot, nil
}

func minLen(a, b int) int {
if a < b {
return a
}
return b
}

// ── Fixtures ─────────────────────────────────────────────────────────────────

func fixtureKeypair(t *testing.T) *execution_solana.Keypair {
t.Helper()
seed := make([]byte, 32)
if _, err := rand.Read(seed); err != nil {
t.Fatalf("rand seed: %v", err)
}
kp, err := execution_solana.KeypairFromSeed(seed)
if err != nil {
t.Fatalf("keypair: %v", err)
}
return kp
}

func fixtureConfig() *config.SolanaExecutionConfig {
return &config.SolanaExecutionConfig{
SlippageCapBps:        200,
ConfirmTimeoutMs:      2000,
ReceiptPollIntervalMs: 100,
MaxSendAttempts:       3,
}
}

func fixtureAlloc() contracts.AllocationDTO {
return contracts.AllocationDTO{
EventID:          "evt1",
ExecutionID:      "execid1",
TokenLifecycleID: "lc1",
TraceID:          "trace1",
CorrelationID:    "corr1",
VersionID:        "v1",
Chain:            "solana",
TokenAddress:     "TokenMintPubkey1111111111111111111111111111",
SizeUsd:          100.0,
MaxSlippageBps:   150,
}
}

const (
testMarket      = "solana-raydium-v4"
testPoolAddress = "PoolPubkey11111111111111111111111111111111"
)

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestKeypairFromSeed_RoundTrip(t *testing.T) {
seed := make([]byte, 32)
for i := range seed {
seed[i] = byte(i + 1)
}
kp, err := execution_solana.KeypairFromSeed(seed)
if err != nil {
t.Fatalf("KeypairFromSeed: %v", err)
}
if len(kp.PublicKey) != 32 {
t.Errorf("PublicKey length: got %d, want 32", len(kp.PublicKey))
}
msg := []byte("test message")
sig := kp.Sign(msg)
if len(sig) != 64 {
t.Errorf("signature length: got %d, want 64", len(sig))
}
}

func TestKeypairFromSeed_InvalidSeed(t *testing.T) {
_, err := execution_solana.KeypairFromSeed([]byte{1, 2, 3}) // too short
if err == nil {
t.Error("expected error for short seed")
}
}

func TestBuildAndSignTransaction_RaydiumSwap(t *testing.T) {
kp := fixtureKeypair(t)
alloc := fixtureAlloc()
cfg := fixtureConfig()

instr, err := execution_solana.BuildSwapInstruction(alloc, testMarket, testPoolAddress, cfg)
if err != nil {
t.Fatalf("BuildSwapInstruction: %v", err)
}

txB64, err := execution_solana.BuildAndSignTransaction(kp, "recentblockhash1", instr)
if err != nil {
t.Fatalf("BuildAndSignTransaction: %v", err)
}

decoded, err := base64.StdEncoding.DecodeString(txB64)
if err != nil {
t.Fatalf("decode base64: %v", err)
}
if len(decoded) < 65 {
t.Errorf("transaction too short: %d bytes", len(decoded))
}
}

func TestBuildAndSignTransaction_PumpFunBuy(t *testing.T) {
kp := fixtureKeypair(t)
alloc := fixtureAlloc()
cfg := fixtureConfig()

instr, err := execution_solana.BuildSwapInstruction(alloc, "solana-pumpfun", testPoolAddress, cfg)
if err != nil {
t.Fatalf("BuildSwapInstruction: %v", err)
}

_, err = execution_solana.BuildAndSignTransaction(kp, "recentblockhash2", instr)
if err != nil {
t.Fatalf("BuildAndSignTransaction PumpFun: %v", err)
}
}

func TestBuildSwapInstruction_UnknownMarket(t *testing.T) {
alloc := fixtureAlloc()
cfg := fixtureConfig()
_, err := execution_solana.BuildSwapInstruction(alloc, "unknown-market", "", cfg)
if err == nil {
t.Error("expected error for unknown market")
}
}

func TestExecute_Confirmed(t *testing.T) {
kp := fixtureKeypair(t)
cfg := fixtureConfig()

client := &mockSolanaExecClient{
blockhash: "FakeBlockhash1111111",
lastSlot:  99000,
statusMap: map[string]*execution_solana.SignatureStatus{},
}

mod, err := execution_solana.New(cfg, client, []*execution_solana.Keypair{kp}, testMarket, nil)
if err != nil {
t.Fatalf("New: %v", err)
}
_ = mod

alloc := fixtureAlloc()

client2 := &confirmingClient{inner: client, slot: 99001}
mod2, _ := execution_solana.New(cfg, client2, []*execution_solana.Keypair{kp}, testMarket, nil)

ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

result, err := mod2.Execute(ctx, alloc, testMarket, testPoolAddress)
if err != nil {
t.Fatalf("Execute: %v", err)
}
if result.Status != "confirmed" {
t.Errorf("Status: got %s, want confirmed", result.Status)
}
if result.TxHash == "" {
t.Error("expected non-empty TxHash")
}
if result.Success != true {
t.Error("expected Success=true for confirmed tx")
}
}

func TestExecute_ContextCancelled(t *testing.T) {
kp := fixtureKeypair(t)
cfg := fixtureConfig()
client := &mockSolanaExecClient{blockhash: "FakeHash", lastSlot: 1}
mod, _ := execution_solana.New(cfg, client, []*execution_solana.Keypair{kp}, testMarket, nil)

ctx, cancel := context.WithCancel(context.Background())
cancel() // cancel immediately

alloc := fixtureAlloc()
result, _ := mod.Execute(ctx, alloc, "", "")
if result.Status != "failed" {
t.Errorf("Expected failed status for cancelled context, got %s", result.Status)
}
}

func TestNew_NilConfig(t *testing.T) {
_, err := execution_solana.New(nil, nil, nil, "", nil)
if err == nil {
t.Error("expected error for nil config")
}
}

func TestNew_NoKeypairs(t *testing.T) {
cfg := fixtureConfig()
_, err := execution_solana.New(cfg, nil, nil, "", nil)
if err == nil {
t.Error("expected error for nil keypairs")
}
}

func TestNew_MaxSendAttemptsCapped(t *testing.T) {
kp := fixtureKeypair(t)
cfg := &config.SolanaExecutionConfig{
MaxSendAttempts:       99, // should be capped to 5
ConfirmTimeoutMs:      1000,
ReceiptPollIntervalMs: 100,
}
mod, err := execution_solana.New(cfg, &mockSolanaExecClient{blockhash: "h", lastSlot: 1}, []*execution_solana.Keypair{kp}, "", nil)
if err != nil {
t.Fatalf("New: %v", err)
}
_ = mod
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// confirmingClient wraps mockSolanaExecClient and returns confirmed status for any sig.
type confirmingClient struct {
inner *mockSolanaExecClient
slot  uint64
}

func (c *confirmingClient) SendTransaction(ctx context.Context, tx string) (string, error) {
return c.inner.SendTransaction(ctx, tx)
}

func (c *confirmingClient) GetSignatureStatus(_ context.Context, _ string) (*execution_solana.SignatureStatus, error) {
return &execution_solana.SignatureStatus{
Slot:               c.slot,
ConfirmationStatus: "confirmed",
}, nil
}

func (c *confirmingClient) GetLatestBlockhash(_ context.Context, _ string) (string, uint64, error) {
return c.inner.blockhash, c.inner.lastSlot, nil
}

func (c *confirmingClient) GetSlot(_ context.Context, _ string) (uint64, error) {
return c.inner.lastSlot, nil
}
