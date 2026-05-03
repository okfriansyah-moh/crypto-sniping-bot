// Package execution — unit tests for Layer 8: Execution Engine.
// All EVMClient calls are mocked; no real RPC node required.
// Tests are deterministic, network-free, and GPU-free.
package execution

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	geth_common "github.com/ethereum/go-ethereum/common"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// ── Mock EVMClient ─────────────────────────────────────────────────────────────

type mockEVMClient struct {
	gasPrice    *big.Int
	gasPriceErr error
	txHash      string
	sendErr     error
	receipt     *TxReceipt
	receiptErr  error
	amountsOut  []*big.Int
	amountsErr  error
}

func (m *mockEVMClient) GetGasPrice(_ context.Context) (*big.Int, error) {
	return m.gasPrice, m.gasPriceErr
}

func (m *mockEVMClient) GetTransactionCount(_ context.Context, _ string) (uint64, error) {
	return 0, nil
}

func (m *mockEVMClient) SendRawTransaction(_ context.Context, _ string) (string, error) {
	return m.txHash, m.sendErr
}

func (m *mockEVMClient) GetTransactionReceipt(_ context.Context, _ string) (*TxReceipt, error) {
	return m.receipt, m.receiptErr
}

func (m *mockEVMClient) GetAmountsOut(_ context.Context, _ string, amountIn *big.Int, _ []string) ([]*big.Int, error) {
	if m.amountsErr != nil {
		return nil, m.amountsErr
	}
	if m.amountsOut != nil {
		return m.amountsOut, nil
	}
	// Default: 1:1 ratio so existing tests are unaffected.
	out := new(big.Int).Set(amountIn)
	return []*big.Int{amountIn, out}, nil
}

// ── Fixtures ──────────────────────────────────────────────────────────────────

// testPrivKey is a well-known non-zero private key — never holds real funds.
const testPrivKey = "0000000000000000000000000000000000000000000000000000000000000001"

// testBaseTokenAddr is a dummy WETH-like address used in tests.
const testBaseTokenAddr = "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"

func testCapitalCfg() *config.CapitalConfig {
	return &config.CapitalConfig{
		FixedEntrySizeUsd: 10.0,
		MaxSizeUsd:        100.0,
		TTLSeconds:        3,
		WalletAddress:     "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
	}
}

func confirmedReceipt() *TxReceipt {
	return &TxReceipt{Status: 1, BlockNumber: 18_000_001, GasUsed: 150_000}
}

func revertedReceipt() *TxReceipt {
	return &TxReceipt{Status: 0, BlockNumber: 18_000_002, GasUsed: 21_000}
}

func successfulMock() *mockEVMClient {
	return &mockEVMClient{
		gasPrice: big.NewInt(20_000_000_000), // 20 gwei
		txHash:   "0xdeadbeef",
		receipt:  confirmedReceipt(),
	}
}

func allocationFixture() contracts.AllocationDTO {
	return contracts.AllocationDTO{
		EventID:          "alloc-001",
		TraceID:          "trace-001",
		CorrelationID:    "corr-001",
		VersionID:        "v1",
		TokenLifecycleID: "lc-001",
		ExecutionID:      "exec-id-001",
		TokenAddress:     "0xdAC17F958D2ee523a2206206994597C13D831ec7",
		Chain:            "eth",
		SizeUsd:          10.0,
		WalletAddress:    "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
		MaxSlippageBps:   200,
		Rejected:         false,
		AllocatedAt:      "2026-04-26T00:00:00Z",
	}
}

// ── Constructor tests ─────────────────────────────────────────────────────────

func TestNew_ValidPrivKey_Succeeds(t *testing.T) {
	// Arrange
	client := successfulMock()

	// Act
	mod, err := New(testCapitalCfg(), nil, client, testPrivKey, 1, testBaseTokenAddr)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mod == nil {
		t.Fatal("expected non-nil module")
	}
}

func TestNew_InvalidPrivKey_ReturnsError(t *testing.T) {
	// Arrange
	client := successfulMock()

	// Act
	mod, err := New(testCapitalCfg(), nil, client, "not-a-valid-hex-key", 1, testBaseTokenAddr)

	// Assert
	if err == nil {
		t.Fatal("expected error for invalid private key")
	}
	if mod != nil {
		t.Error("expected nil module on error")
	}
	if !strings.Contains(err.Error(), "invalid private key") {
		t.Errorf("error should mention 'invalid private key', got: %v", err)
	}
}

func TestNew_EmptyPrivKey_ReturnsError(t *testing.T) {
	// Arrange / Act
	_, err := New(testCapitalCfg(), nil, successfulMock(), "", 1, testBaseTokenAddr)

	// Assert
	if err == nil {
		t.Fatal("expected error for empty private key")
	}
}

// ── Process: rejected allocation ─────────────────────────────────────────────

func TestProcess_RejectedAllocation_SkipsExecution(t *testing.T) {
	// Arrange
	mod, _ := New(testCapitalCfg(), nil, successfulMock(), testPrivKey, 1, testBaseTokenAddr)
	in := allocationFixture()
	in.Rejected = true
	in.RejectReason = "max_open_positions_reached:1"

	// Act
	result, err := mod.Process(context.Background(), in, 0, "0xrouter")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "rejected" {
		t.Errorf("expected status=rejected, got %q", result.Status)
	}
	if result.Success {
		t.Error("expected Success=false for rejected allocation")
	}
	if result.Attempts != 0 {
		t.Errorf("expected Attempts=0 for skipped, got %d", result.Attempts)
	}
	if result.RejectionReason != in.RejectReason {
		t.Errorf("RejectionReason not propagated: got %q", result.RejectionReason)
	}
	if result.EventID == "" {
		t.Error("EventID must be set")
	}
	if result.CausationID != in.EventID {
		t.Errorf("CausationID should be input EventID; got %q", result.CausationID)
	}
}

func TestProcess_RejectedAllocation_Deterministic(t *testing.T) {
	// Arrange
	mod, _ := New(testCapitalCfg(), nil, successfulMock(), testPrivKey, 1, testBaseTokenAddr)
	in := allocationFixture()
	in.Rejected = true
	in.RejectReason = "edge_not_validated"

	// Act
	r1, _ := mod.Process(context.Background(), in, 0, "0xrouter")
	r2, _ := mod.Process(context.Background(), in, 0, "0xrouter")

	// Assert: EventID must be deterministic (content-addressable)
	if r1.EventID != r2.EventID {
		t.Errorf("non-deterministic EventID: %q vs %q", r1.EventID, r2.EventID)
	}
}

// ── Process: gas price failure ────────────────────────────────────────────────

func TestProcess_GasPriceError_ReturnsFailResult(t *testing.T) {
	// Arrange
	client := &mockEVMClient{gasPriceErr: errors.New("rpc timeout")}
	mod, _ := New(testCapitalCfg(), nil, client, testPrivKey, 1, testBaseTokenAddr)

	// Act
	result, err := mod.Process(context.Background(), allocationFixture(), 0, "0xrouter")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("expected status=failed, got %q", result.Status)
	}
	if result.Success {
		t.Error("expected Success=false on gas price error")
	}
	if !strings.Contains(result.ErrorCode, "get_gas_price") {
		t.Errorf("ErrorCode should contain 'get_gas_price', got %q", result.ErrorCode)
	}
}

// ── Process: Phase 2 live execution safety gate ───────────────────────────────
//
// Phase 2 hard-blocks all on-chain execution because amountOutMin=0 makes
// every swap trivially sandwich-attackable. These tests verify the gate fires
// for every non-rejected allocation that reaches the live execution path.
// Phase 3 removes this gate and restores send/receipt/confirmed/reverted tests.

func TestProcess_LiveExecution_BlockedBySlippageGuard(t *testing.T) {
	// Arrange: mock returns a tiny output (0.5% of input) simulating 99.5% slippage.
	// With MaxSlippageBps=200, the slippage guard must fire and block the trade.
	amountIn := usdToWei(10.0, 3500.0)
	tinyOut := new(big.Int).Div(amountIn, big.NewInt(200)) // 0.5% of input → ~9950 bps slippage
	client := &mockEVMClient{
		gasPrice:   big.NewInt(20_000_000_000),
		amountsOut: []*big.Int{amountIn, tinyOut},
		txHash:     "0xdeadbeef",
		receipt:    confirmedReceipt(),
	}
	mod, _ := New(testCapitalCfg(), nil, client, testPrivKey, 1, testBaseTokenAddr)
	in := allocationFixture() // MaxSlippageBps=200

	// Act
	result, err := mod.Process(context.Background(), in, 0, "0xrouter")

	// Assert: Phase 2 guard must fire — no transaction is broadcast.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("expected status=failed from slippage guard, got %q", result.Status)
	}
	if result.Success {
		t.Error("Success must be false when slippage guard blocks execution")
	}
	if !strings.Contains(result.ErrorCode, "slippage_guard") {
		t.Errorf("ErrorCode should contain 'slippage_guard', got %q", result.ErrorCode)
	}
	// Trace fields must still propagate even on guard failure.
	if result.EventID == "" {
		t.Error("EventID must be set")
	}
	if result.CausationID != in.EventID {
		t.Errorf("CausationID should be input EventID; got %q", result.CausationID)
	}
}

func TestProcess_SlippageGuard_TraceFieldsPropagated(t *testing.T) {
	// Verify that trace / correlation IDs are carried through the guard path.
	mod, _ := New(testCapitalCfg(), nil, successfulMock(), testPrivKey, 1, testBaseTokenAddr)
	in := allocationFixture()
	in.TraceID = "my-trace"
	in.CorrelationID = "my-corr"
	in.VersionID = "v42"

	result, _ := mod.Process(context.Background(), in, 0, "0xrouter")

	if result.TraceID != "my-trace" {
		t.Errorf("TraceID not propagated: %q", result.TraceID)
	}
	if result.CorrelationID != "my-corr" {
		t.Errorf("CorrelationID not propagated: %q", result.CorrelationID)
	}
	if result.VersionID != "v42" {
		t.Errorf("VersionID not propagated: %q", result.VersionID)
	}
}

func TestProcess_SlippageGuard_DifferentAllocations_Deterministic(t *testing.T) {
	// Two identical live-path calls should produce the same EventID (content-addressed).
	mod, _ := New(testCapitalCfg(), nil, successfulMock(), testPrivKey, 1, testBaseTokenAddr)
	in := allocationFixture()

	r1, _ := mod.Process(context.Background(), in, 0, "0xrouter")
	r2, _ := mod.Process(context.Background(), in, 0, "0xrouter")

	if r1.EventID != r2.EventID {
		t.Errorf("non-deterministic EventID: %q vs %q", r1.EventID, r2.EventID)
	}
}

// ── Process: context cancellation ────────────────────────────────────────────

func TestProcess_ContextCancelled_ReturnsError(t *testing.T) {
	// Even with a cancelled context the slippage guard fires before any network call.
	mod, _ := New(testCapitalCfg(), nil, successfulMock(), testPrivKey, 1, testBaseTokenAddr)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := mod.Process(ctx, allocationFixture(), 0, "0xrouter")

	// Guard fires synchronously — either a failed result or context error is acceptable.
	if err == nil && result.Status != "failed" {
		t.Errorf("expected error or failed status, got status=%q", result.Status)
	}
}

// ── Trace field propagation ───────────────────────────────────────────────────

func TestProcess_TraceFieldsPropagated(t *testing.T) {
	// Arrange
	mod, _ := New(testCapitalCfg(), nil, successfulMock(), testPrivKey, 1, testBaseTokenAddr)
	in := allocationFixture()
	in.TraceID = "my-trace"
	in.CorrelationID = "my-corr"
	in.VersionID = "v42"

	// Act
	result, _ := mod.Process(context.Background(), in, 0, "0xrouter")

	// Assert
	if result.TraceID != "my-trace" {
		t.Errorf("TraceID not propagated: %q", result.TraceID)
	}
	if result.CorrelationID != "my-corr" {
		t.Errorf("CorrelationID not propagated: %q", result.CorrelationID)
	}
	if result.VersionID != "v42" {
		t.Errorf("VersionID not propagated: %q", result.VersionID)
	}
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func TestUsdToWei_Deterministic(t *testing.T) {
	// Arrange / Act
	w1 := usdToWei(10.0, 3500.0)
	w2 := usdToWei(10.0, 3500.0)

	// Assert
	if w1.Cmp(w2) != 0 {
		t.Errorf("usdToWei not deterministic: %s vs %s", w1, w2)
	}
	if w1.Sign() <= 0 {
		t.Error("usdToWei should return positive wei")
	}
}

func TestUsdToWei_ZeroUsd_ReturnsZero(t *testing.T) {
	// Arrange / Act
	w := usdToWei(0.0, 3500.0)

	// Assert
	if w.Sign() != 0 {
		t.Errorf("expected 0 wei for 0 USD, got %s", w)
	}
}

func TestPadLeft32_ShortInput_Pads(t *testing.T) {
	// Arrange
	input := []byte{0x01}

	// Act
	result := padLeft32(input)

	// Assert
	if len(result) != 32 {
		t.Errorf("expected 32 bytes, got %d", len(result))
	}
	if result[31] != 0x01 {
		t.Errorf("expected last byte=0x01, got 0x%02x", result[31])
	}
	for i := 0; i < 31; i++ {
		if result[i] != 0x00 {
			t.Errorf("expected leading zero at byte %d, got 0x%02x", i, result[i])
		}
	}
}

func TestPadLeft32_ExactLength_Unchanged(t *testing.T) {
	// Arrange
	input := make([]byte, 32)
	input[0] = 0xAB

	// Act
	result := padLeft32(input)

	// Assert
	if len(result) != 32 {
		t.Errorf("expected 32 bytes, got %d", len(result))
	}
	if result[0] != 0xAB {
		t.Errorf("expected first byte=0xAB, got 0x%02x", result[0])
	}
}

func TestBuildSwapCalldata_HasCorrectSelectorAndLength(t *testing.T) {
	// Arrange: swapExactETHForTokens(uint256,address[],address,uint256)
	// selector = keccak256(sig)[:4] = 0x7ff36ab5
	amountOutMin := big.NewInt(0)
	path := []geth_common.Address{
		geth_common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"),
		geth_common.HexToAddress("0xdAC17F958D2ee523a2206206994597C13D831ec7"),
	}
	to := geth_common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266")
	deadline := big.NewInt(9_999_999_999)

	// Act
	calldata, err := buildSwapCalldata(amountOutMin, path, to, deadline)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calldata) < 4 {
		t.Fatalf("calldata too short: %d bytes", len(calldata))
	}
	// Verify function selector: 0x7ff36ab5
	if calldata[0] != 0x7f || calldata[1] != 0xf3 || calldata[2] != 0x6a || calldata[3] != 0xb5 {
		t.Errorf("wrong selector: % x", calldata[:4])
	}
	// 4 bytes selector + 5 slots (amountOutMin, pathOffset, to, deadline, pathLen) + 2 path entries
	expectedLen := 4 + 32*7
	if len(calldata) != expectedLen {
		t.Errorf("expected calldata length %d, got %d", expectedLen, len(calldata))
	}
}

// ── Slippage guard security tests ─────────────────────────────────────────────

func TestProcess_GetAmountsOut_Failure_RejectsTransaction(t *testing.T) {
	// Arrange — quote RPC call fails
	client := &mockEVMClient{
		gasPrice:   big.NewInt(20_000_000_000),
		amountsErr: errors.New("rpc error: method not found"),
		txHash:     "0xdeadbeef",
		receipt:    confirmedReceipt(),
	}
	mod, _ := New(testCapitalCfg(), nil, client, testPrivKey, 1, testBaseTokenAddr)
	in := allocationFixture()

	// Act
	result, err := mod.Process(context.Background(), in, 0, "0xrouter")

	// Assert — fail-closed: tx must not be submitted when quote fails
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected Success=false when GetAmountsOut fails")
	}
	if result.TxHash != "" {
		t.Error("expected no TxHash when GetAmountsOut fails")
	}
	if !strings.Contains(result.ErrorCode, "get_amounts_out") {
		t.Errorf("expected ErrorCode to mention get_amounts_out, got: %q", result.ErrorCode)
	}
}

func TestProcess_GetAmountsOut_ZeroOutput_RejectsTransaction(t *testing.T) {
	// Arrange — quote returns zero expected output
	amountIn := usdToWei(10.0, 3500.0)
	client := &mockEVMClient{
		gasPrice:   big.NewInt(20_000_000_000),
		amountsOut: []*big.Int{amountIn, big.NewInt(0)},
		txHash:     "0xdeadbeef",
		receipt:    confirmedReceipt(),
	}
	mod, _ := New(testCapitalCfg(), nil, client, testPrivKey, 1, testBaseTokenAddr)
	in := allocationFixture()

	// Act
	result, err := mod.Process(context.Background(), in, 0, "0xrouter")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected Success=false when expected output is zero")
	}
	if !strings.Contains(result.ErrorCode, "zero_expected_output") {
		t.Errorf("expected ErrorCode zero_expected_output, got: %q", result.ErrorCode)
	}
}

func TestProcess_SlippageGuard_PopulatedInResult(t *testing.T) {
	// Arrange — valid quote, slippage bps set to 200
	client := successfulMock()
	mod, _ := New(testCapitalCfg(), nil, client, testPrivKey, 1, testBaseTokenAddr)
	in := allocationFixture()
	in.MaxSlippageBps = 200

	// Act
	result, err := mod.Process(context.Background(), in, 0, "0xrouter")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SlippageGuardBps != 200 {
		t.Errorf("expected SlippageGuardBps=200, got=%d", result.SlippageGuardBps)
	}
}

func TestProcess_SlippageGuard_FallbackToDefault_WhenInvalid(t *testing.T) {
	// Arrange — invalid slippage (0) should fall back to defaultSlippageBps
	client := successfulMock()
	mod, _ := New(testCapitalCfg(), nil, client, testPrivKey, 1, testBaseTokenAddr)
	in := allocationFixture()
	in.MaxSlippageBps = 0

	// Act
	result, err := mod.Process(context.Background(), in, 0, "0xrouter")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SlippageGuardBps != defaultSlippageBps {
		t.Errorf("expected SlippageGuardBps=%d (default), got=%d", defaultSlippageBps, result.SlippageGuardBps)
	}
}
