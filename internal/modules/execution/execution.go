// Package execution implements Layer 8: Execution Engine.
// Consumes AllocationDTO + nonce and emits ExecutionResultDTO.
// Pure function: EVM client is injected, no DB, no shared state.
package execution

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"

	geth_common "github.com/ethereum/go-ethereum/common"
	geth_core "github.com/ethereum/go-ethereum/core/types"
	geth_crypto "github.com/ethereum/go-ethereum/crypto"
)

// EVMClient is the minimal interface needed by the execution module.
// Defined here so the module is testable without a real RPC node.
type EVMClient interface {
	// GetGasPrice returns the current suggested gas price in wei.
	GetGasPrice(ctx context.Context) (*big.Int, error)

	// GetTransactionCount returns the confirmed on-chain nonce for an address.
	GetTransactionCount(ctx context.Context, address string) (uint64, error)

	// SendRawTransaction broadcasts a signed transaction and returns the tx hash.
	SendRawTransaction(ctx context.Context, rawHexTx string) (string, error)

	// GetTransactionReceipt polls for the receipt of a tx hash.
	// Returns nil, nil if the tx is not yet mined.
	GetTransactionReceipt(ctx context.Context, txHash string) (*TxReceipt, error)

	// GetAmountsOut calls the router's getAmountsOut(amountIn, path) view function.
	// Returns the expected output token amounts for each step of the path.
	// The last element is the final output amount after all swaps.
	// routerAddress is the DEX router contract (e.g. UniswapV2Router02).
	GetAmountsOut(ctx context.Context, routerAddress string, amountIn *big.Int, path []string) ([]*big.Int, error)
}

// defaultSlippageBps is the fallback slippage tolerance used when AllocationDTO.MaxSlippageBps
// is zero or invalid. 100 bps = 1% maximum slippage.
const defaultSlippageBps int32 = 100

// TxReceipt is the minimal receipt needed by the execution module.
type TxReceipt struct {
	Status      uint64 // 1 = success, 0 = reverted
	BlockNumber uint64
	GasUsed     uint64
}

// Module is the execution engine.
type Module struct {
	cfg           *config.CapitalConfig
	client        EVMClient
	privKey       *ecdsa.PrivateKey
	chainID       *big.Int
	baseTokenAddr string // chain-specific base token address (e.g. WETH on ETH mainnet)
}

// New creates a new execution Module.
// privKeyHex is the hex-encoded private key (no 0x prefix).
// baseTokenAddr is the chain-specific base token address (e.g. WETH on ETH mainnet);
// sourced from config/chains.yaml base_tokens[0].address for the active chain.
func New(cfg *config.CapitalConfig, client EVMClient, privKeyHex string, chainID int64, baseTokenAddr string) (*Module, error) {
	privKey, err := geth_crypto.HexToECDSA(strings.TrimPrefix(privKeyHex, "0x"))
	if err != nil {
		return nil, fmt.Errorf("execution: invalid private key: %w", err)
	}
	return &Module{
		cfg:           cfg,
		client:        client,
		privKey:       privKey,
		chainID:       big.NewInt(chainID),
		baseTokenAddr: strings.TrimSpace(baseTokenAddr),
	}, nil
}

// Process signs and submits a UniswapV2 swapExactETHForTokens transaction.
// Returns ExecutionResultDTO with confirmed status.
// Phase 2: single wallet, no sharding, no replacement loop, public mempool only.
func (m *Module) Process(
	ctx context.Context,
	in contracts.AllocationDTO,
	nonce uint64,
	routerAddress string,
) (contracts.ExecutionResultDTO, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	start := time.Now()

	if in.Rejected {
		eventID := contracts.ContentIDFromString(fmt.Sprintf("exec-skip:%s", in.EventID))
		return contracts.ExecutionResultDTO{
			EventID:       eventID,
			TraceID:       in.TraceID,
			CorrelationID: in.CorrelationID,
			CausationID:   in.EventID,
			VersionID:     in.VersionID,

			TokenLifecycleID: in.TokenLifecycleID,
			ExecutionID:      in.ExecutionID,
			AllocationID:     in.EventID,

			Status:          "rejected",
			Success:         false,
			Attempts:        0,
			MempoolRoute:    "public",
			WalletAddress:   in.WalletAddress,
			RejectionReason: in.RejectReason,
			CompletedAt:     now,
		}, nil
	}

	// Fetch gas price (validates RPC connectivity; consumed by Phase 3 signTx).
	gasPrice, err := m.client.GetGasPrice(ctx)
	if err != nil {
		return m.failResult(in, now, fmt.Sprintf("get_gas_price:%v", err))
	}

	// Validate required calldata inputs before attempting the guard.
	if m.baseTokenAddr == "" {
		return m.failResult(in, now, "build_calldata:missing_base_token_address")
	}
	if in.AllocatedAt == "" {
		return m.failResult(in, now, "build_calldata:missing_allocated_at")
	}
	allocatedAt, parseErr := time.Parse(time.RFC3339Nano, in.AllocatedAt)
	if parseErr != nil {
		allocatedAt, parseErr = time.Parse(time.RFC3339, in.AllocatedAt)
		if parseErr != nil {
			return m.failResult(in, now, "build_calldata:invalid_allocated_at")
		}
	}

	// Convert USD size to wei (simplified: treat USD as ETH for Phase 2 test).
	valueWei := usdToWei(in.SizeUsd)

	// Slippage guard: get expected output from router and apply MaxSlippageBps.
	// Fail-closed: if the quote fails or returns zero, the tx is not submitted.
	slippageBps := in.MaxSlippageBps
	if slippageBps <= 0 || slippageBps >= 10000 {
		slippageBps = defaultSlippageBps
	}
	path := []geth_common.Address{
		geth_common.HexToAddress(m.baseTokenAddr),
		geth_common.HexToAddress(in.TokenAddress),
	}
	quotePath := []string{m.baseTokenAddr, in.TokenAddress}
	amounts, quoteErr := m.client.GetAmountsOut(ctx, routerAddress, valueWei, quotePath)
	if quoteErr != nil {
		return m.failResult(in, now, fmt.Sprintf("get_amounts_out:%v", quoteErr))
	}
	if len(amounts) < 2 {
		return m.failResult(in, now, "get_amounts_out:invalid_response_length")
	}
	expectedOut := amounts[len(amounts)-1]
	if expectedOut == nil || expectedOut.Sign() <= 0 {
		return m.failResult(in, now, "get_amounts_out:zero_expected_output")
	}
	// amountOutMin = expectedOut × (10000 − slippageBps) / 10000
	amountOutMin := new(big.Int).Mul(expectedOut, big.NewInt(int64(10000-slippageBps)))
	amountOutMin.Div(amountOutMin, big.NewInt(10000))

	deadline := big.NewInt(allocatedAt.Add(3 * time.Minute).Unix())
	to := geth_common.HexToAddress(in.WalletAddress)

	calldata, err := buildSwapCalldata(amountOutMin, path, to, deadline)
	if err != nil {
		return m.failResult(in, now, fmt.Sprintf("build_calldata:%v", err))
	}

	// Build and sign the transaction.
	rawTx, txHash, err := m.signTx(nonce, routerAddress, valueWei, gasPrice, 300_000, calldata)
	if err != nil {
		return m.failResult(in, now, fmt.Sprintf("sign_tx:%v", err))
	}

	// Submit to mempool.
	submittedHash, err := m.client.SendRawTransaction(ctx, rawTx)
	if err != nil {
		return m.failResult(in, now, fmt.Sprintf("send_tx:%v", err))
	}
	if submittedHash != "" {
		txHash = submittedHash
	}

	// Poll for receipt (up to 30s, 3s interval — Phase 2 simple polling).
	receipt, err := m.pollReceipt(ctx, txHash)
	if err != nil {
		return m.failResult(in, now, fmt.Sprintf("poll_receipt:%v", err))
	}

	latencyMs := int32(time.Since(start).Milliseconds())
	eventID := contracts.ContentIDFromString(fmt.Sprintf("exec:%s:%s", in.EventID, txHash))

	status := "confirmed"
	success := true
	if receipt == nil {
		// Receipt not observed within the polling deadline is not a confirmed revert.
		// Treat as pending so callers can retry or reconcile later.
		status = "pending"
		success = false
	} else if receipt.Status == 0 {
		status = "reverted"
		success = false
	}

	return contracts.ExecutionResultDTO{
		EventID:       eventID,
		TraceID:       in.TraceID,
		CorrelationID: in.CorrelationID,
		CausationID:   in.EventID,
		VersionID:     in.VersionID,

		TokenLifecycleID: in.TokenLifecycleID,
		ExecutionID:      in.ExecutionID,
		AllocationID:     in.EventID,

		Status:           status,
		Success:          success,
		TxHash:           txHash,
		Attempts:         1,
		MempoolRoute:     "public",
		NonceUsed:        nonce,
		WalletAddress:    in.WalletAddress,
		WalletShard:      in.WalletShard,
		LatencyMs:        latencyMs,
		CompletedAt:      now,
		SlippageGuardBps: slippageBps,
	}, nil
}

// signTx builds, signs, and returns the raw hex tx + predicted tx hash.
func (m *Module) signTx(
	nonce uint64,
	to string,
	value *big.Int,
	gasPrice *big.Int,
	gasLimit uint64,
	data []byte,
) (string, string, error) {
	toAddr := geth_common.HexToAddress(to)
	tx := geth_core.NewTx(&geth_core.LegacyTx{
		Nonce:    nonce,
		To:       &toAddr,
		Value:    value,
		Gas:      gasLimit,
		GasPrice: gasPrice,
		Data:     data,
	})

	signer := geth_core.NewEIP155Signer(m.chainID)
	signedTx, err := geth_core.SignTx(tx, signer, m.privKey)
	if err != nil {
		return "", "", fmt.Errorf("sign tx: %w", err)
	}

	rawBytes, err := signedTx.MarshalBinary()
	if err != nil {
		return "", "", fmt.Errorf("marshal tx: %w", err)
	}

	rawHex := "0x" + hex.EncodeToString(rawBytes)
	txHash := signedTx.Hash().Hex()
	return rawHex, txHash, nil
}

// pollReceipt polls for a transaction receipt with a simple loop.
func (m *Module) pollReceipt(ctx context.Context, txHash string) (*TxReceipt, error) {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		receipt, err := m.client.GetTransactionReceipt(ctx, txHash)
		if err != nil {
			return nil, err
		}
		if receipt != nil {
			return receipt, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return nil, nil // not mined within deadline
}

// failResult creates a failed ExecutionResultDTO.
func (m *Module) failResult(in contracts.AllocationDTO, now, errorCode string) (contracts.ExecutionResultDTO, error) {
	eventID := contracts.ContentIDFromString(fmt.Sprintf("exec-fail:%s:%s", in.EventID, errorCode))
	return contracts.ExecutionResultDTO{
		EventID:       eventID,
		TraceID:       in.TraceID,
		CorrelationID: in.CorrelationID,
		CausationID:   in.EventID,
		VersionID:     in.VersionID,

		TokenLifecycleID: in.TokenLifecycleID,
		ExecutionID:      in.ExecutionID,
		AllocationID:     in.EventID,

		Status:        "failed",
		Success:       false,
		Attempts:      1,
		MempoolRoute:  "public",
		WalletAddress: in.WalletAddress,
		ErrorCode:     errorCode,
		CompletedAt:   now,
	}, nil
}

// buildSwapCalldata encodes the swapExactETHForTokens(uint256,address[],address,uint256) call.
// Manual ABI encoding (no abigen required) for Phase 2 single-function use.
func buildSwapCalldata(
	amountOutMin *big.Int,
	path []geth_common.Address,
	to geth_common.Address,
	deadline *big.Int,
) ([]byte, error) {
	// Function selector: keccak256("swapExactETHForTokens(uint256,address[],address,uint256)")[:4]
	selector := []byte{0x7f, 0xf3, 0x6a, 0xb5}

	buf := make([]byte, 0, 4+32*10)
	buf = append(buf, selector...)

	// amountOutMin (uint256, slot 0)
	buf = append(buf, padLeft32(amountOutMin.Bytes())...)

	// path (dynamic: offset at slot 1 → 4 slots in = 0x80)
	pathOffset := big.NewInt(0x80)
	buf = append(buf, padLeft32(pathOffset.Bytes())...)

	// to (address, slot 2)
	addrBytes := make([]byte, 32)
	copy(addrBytes[12:], to.Bytes())
	buf = append(buf, addrBytes...)

	// deadline (uint256, slot 3)
	buf = append(buf, padLeft32(deadline.Bytes())...)

	// path array: length + elements
	pathLen := big.NewInt(int64(len(path)))
	buf = append(buf, padLeft32(pathLen.Bytes())...)
	for _, addr := range path {
		addrSlot := make([]byte, 32)
		copy(addrSlot[12:], addr.Bytes())
		buf = append(buf, addrSlot...)
	}

	return buf, nil
}

// padLeft32 pads b to 32 bytes on the left with zeros.
func padLeft32(b []byte) []byte {
	out := make([]byte, 32)
	if len(b) > 32 {
		b = b[len(b)-32:]
	}
	copy(out[32-len(b):], b)
	return out
}

// usdToWei converts a USD amount to wei using a 1 ETH = 2500 USD approximation.
// Phase 2 simplification: replaced by real price feed in Phase 3.
func usdToWei(usd float64) *big.Int {
	const ethPriceUsd = 2500.0
	ethAmount := usd / ethPriceUsd
	weiF := ethAmount * 1e18
	return new(big.Int).SetInt64(int64(weiF))
}
