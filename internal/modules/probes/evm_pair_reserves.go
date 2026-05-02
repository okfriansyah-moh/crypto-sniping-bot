package probes

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"crypto-sniping-bot/contracts"
)

// EVMPairReservesRPCClient is the narrow eth_call surface this probe needs.
// Re-uses the same shape as HoneypotSimRPCClient, kept separate so each
// probe stays decoupled — production wiring can pass the same concrete
// client implementation.
type EVMPairReservesRPCClient interface {
	EthCall(ctx context.Context, to string, callData []byte, block string) ([]byte, error)
}

// EVMPairReservesConfig configures the evm_pair_reserves probe.
type EVMPairReservesConfig struct {
	Enabled   bool `yaml:"enabled"`
	TimeoutMs int  `yaml:"timeout_ms"`
}

// uniswapV2GetReservesSelector is keccak256("getReserves()")[:4]. The
// canonical Uniswap-V2 pair returns (uint112 reserve0, uint112 reserve1, uint32 blockTimestampLast).
var uniswapV2GetReservesSelector = mustHex("0x0902f1ac")

func mustHex(s string) []byte {
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil {
		panic(err)
	}
	return b
}

// EVMReserves is the decoded result of getReserves().
type EVMReserves struct {
	Reserve0       *big.Int
	Reserve1       *big.Int
	BlockTimestamp uint32
}

// DecodeUniswapV2Reserves parses the 96-byte ABI-encoded return of
// getReserves(): three 32-byte words (uint112 reserve0, uint112 reserve1,
// uint32 blockTimestampLast — each left-padded into 32 bytes).
func DecodeUniswapV2Reserves(b []byte) (*EVMReserves, error) {
	if len(b) < 96 {
		return nil, fmt.Errorf("probes/evm_reserves: short response: %d bytes", len(b))
	}
	r0 := new(big.Int).SetBytes(b[0:32])
	r1 := new(big.Int).SetBytes(b[32:64])
	ts := new(big.Int).SetBytes(b[64:96])
	if !ts.IsUint64() || ts.Uint64() > 1<<32-1 {
		return nil, fmt.Errorf("probes/evm_reserves: timestamp out of uint32 range")
	}
	return &EVMReserves{Reserve0: r0, Reserve1: r1, BlockTimestamp: uint32(ts.Uint64())}, nil
}

// EVMPairReservesProbe replaces the ingestion-time per-swap reserve flow
// with the live pool depth via getReserves(). Skips non-EVM inputs and
// inputs whose Token0Address / Token1Address / BaseAddress aren't set
// (we cannot tell which of (reserve0, reserve1) is the base side).
type EVMPairReservesProbe struct {
	rpc    EVMPairReservesRPCClient
	cfg    EVMPairReservesConfig
	logger *slog.Logger
}

func NewEVMPairReservesProbe(rpc EVMPairReservesRPCClient, cfg EVMPairReservesConfig, logger *slog.Logger) *EVMPairReservesProbe {
	if logger == nil {
		logger = slog.Default()
	}
	return &EVMPairReservesProbe{rpc: rpc, cfg: cfg, logger: logger}
}

func (p *EVMPairReservesProbe) Name() string { return "evm_pair_reserves" }

// Probe calls getReserves() on the pair and populates ReserveBaseRaw /
// ReserveTokenRaw with live pool depth. LiquidityUsd is left to a
// downstream probe (an EVM USD price source is required and not all
// deployments configure one).
func (p *EVMPairReservesProbe) Probe(ctx context.Context, in contracts.MarketDataDTO) (contracts.MarketDataDTO, error) {
	chain := strings.ToLower(in.Chain)
	if chain == "" || chain == "solana" {
		return in, nil
	}
	pool := strings.TrimSpace(in.PoolAddress)
	if pool == "" {
		return in, nil
	}
	if in.Token0Address == "" || in.Token1Address == "" || in.BaseAddress == "" {
		return in, nil
	}
	if p.rpc == nil {
		// Mirrors honeypot_sim: dormant when no RPC is wired.
		return in, errors.New("probes/evm_pair_reserves: nil rpc client")
	}

	timeout := time.Duration(p.cfg.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 300 * time.Millisecond
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out, err := p.rpc.EthCall(cctx, pool, uniswapV2GetReservesSelector, "latest")
	if err != nil {
		return in, fmt.Errorf("probes/evm_pair_reserves: eth_call: %w", err)
	}
	res, err := DecodeUniswapV2Reserves(out)
	if err != nil {
		return in, err
	}

	// Resolve which reserve is the base (WETH/WBNB/USDx) side.
	baseIs0 := strings.EqualFold(in.Token0Address, in.BaseAddress)
	baseIs1 := strings.EqualFold(in.Token1Address, in.BaseAddress)
	if !baseIs0 && !baseIs1 {
		return in, fmt.Errorf("probes/evm_pair_reserves: base %s not in pair (%s, %s)",
			in.BaseAddress, in.Token0Address, in.Token1Address)
	}

	enriched := in
	if baseIs0 {
		enriched.ReserveBaseRaw = res.Reserve0.String()
		enriched.ReserveTokenRaw = res.Reserve1.String()
	} else {
		enriched.ReserveBaseRaw = res.Reserve1.String()
		enriched.ReserveTokenRaw = res.Reserve0.String()
	}
	return enriched, nil
}
