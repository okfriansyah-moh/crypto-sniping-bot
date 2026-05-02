package probes

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strconv"
	"strings"
	"time"

	"crypto-sniping-bot/contracts"
)

// Pump.fun BondingCurve account layout (49 bytes):
//
//	[ 0..  8) anchor discriminator
//	[ 8.. 16) virtualTokenReserves (u64 LE)
//	[16.. 24) virtualSolReserves   (u64 LE)
//	[24.. 32) realTokenReserves    (u64 LE)
//	[32.. 40) realSolReserves      (u64 LE)
//	[40.. 48) tokenTotalSupply     (u64 LE)
//	[48.. 49) complete             (bool)
const (
	pumpfunBondingCurveSize = 49
	offsetVirtualTokenRes   = 8
	offsetVirtualSolRes     = 16
	offsetRealTokenRes      = 24
	offsetRealSolRes        = 32
	offsetTokenTotalSupply  = 40
	offsetComplete          = 48

	lamportsPerSol = 1_000_000_000.0
)

// PumpfunCurveState is the decoded view the LP probe consumes.
type PumpfunCurveState struct {
	VirtualTokenReserves uint64
	VirtualSolReserves   uint64
	RealTokenReserves    uint64
	RealSolReserves      uint64
	TokenTotalSupply     uint64
	Complete             bool
}

// DecodePumpfunBondingCurve decodes the on-chain bonding curve account.
func DecodePumpfunBondingCurve(b []byte) (*PumpfunCurveState, error) {
	if len(b) < pumpfunBondingCurveSize {
		return nil, fmt.Errorf("probes/pumpfun_lp: bonding curve account too short: %d bytes", len(b))
	}
	return &PumpfunCurveState{
		VirtualTokenReserves: binary.LittleEndian.Uint64(b[offsetVirtualTokenRes:]),
		VirtualSolReserves:   binary.LittleEndian.Uint64(b[offsetVirtualSolRes:]),
		RealTokenReserves:    binary.LittleEndian.Uint64(b[offsetRealTokenRes:]),
		RealSolReserves:      binary.LittleEndian.Uint64(b[offsetRealSolRes:]),
		TokenTotalSupply:     binary.LittleEndian.Uint64(b[offsetTokenTotalSupply:]),
		Complete:             b[offsetComplete] == 1,
	}, nil
}

// SolUsdSource returns a USD price per SOL (e.g. via the Pyth provider).
// The bool is false when no recent price is available; callers SHOULD
// then decline to populate LiquidityUsd rather than fabricate one.
type SolUsdSource interface {
	SolUsd(ctx context.Context) (float64, bool)
}

// SolanaPumpfunLpConfig configures the pumpfun_lp probe.
type SolanaPumpfunLpConfig struct {
	Enabled    bool   `yaml:"enabled"`
	TimeoutMs  int    `yaml:"timeout_ms"`
	Commitment string `yaml:"commitment"`
}

// SolanaPumpfunLpProbe replaces the virtual-reserve estimate produced
// by ingestion with live bonding-curve reserves and a USD liquidity
// figure derived from the live SOL/USD feed. Skips non-pumpfun markets.
type SolanaPumpfunLpProbe struct {
	rpc    SolanaProbeRPCClient
	solUsd SolUsdSource
	cfg    SolanaPumpfunLpConfig
	logger *slog.Logger
}

func NewSolanaPumpfunLpProbe(rpc SolanaProbeRPCClient, solUsd SolUsdSource, cfg SolanaPumpfunLpConfig, logger *slog.Logger) *SolanaPumpfunLpProbe {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Commitment == "" {
		cfg.Commitment = "confirmed"
	}
	return &SolanaPumpfunLpProbe{rpc: rpc, solUsd: solUsd, cfg: cfg, logger: logger}
}

func (p *SolanaPumpfunLpProbe) Name() string { return "solana_pumpfun_lp" }

// Probe applies only to pump.fun markets ("solana-pumpfun"). Other
// markets pass through unchanged.
func (p *SolanaPumpfunLpProbe) Probe(ctx context.Context, in contracts.MarketDataDTO) (contracts.MarketDataDTO, error) {
	if !strings.EqualFold(in.Chain, "solana") {
		return in, nil
	}
	if !strings.HasPrefix(strings.ToLower(in.Market), "solana-pumpfun") {
		return in, nil
	}
	pool := strings.TrimSpace(in.PoolAddress)
	if pool == "" {
		return in, errors.New("probes/pumpfun_lp: empty pool address")
	}
	if p.rpc == nil {
		return in, errors.New("probes/pumpfun_lp: nil rpc client")
	}

	timeout := time.Duration(p.cfg.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 300 * time.Millisecond
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	acc, err := p.rpc.GetAccountInfo(cctx, pool, p.cfg.Commitment)
	if err != nil {
		return in, fmt.Errorf("probes/pumpfun_lp: get_account_info: %w", err)
	}
	if acc == nil {
		return in, fmt.Errorf("probes/pumpfun_lp: bonding curve not found: %s", pool)
	}
	raw, err := base64.StdEncoding.DecodeString(acc.DataB64)
	if err != nil {
		return in, fmt.Errorf("probes/pumpfun_lp: base64 decode: %w", err)
	}
	state, err := DecodePumpfunBondingCurve(raw)
	if err != nil {
		return in, err
	}

	// Reserves: total = virtual + real for both legs. Stored as decimal
	// strings to match the rest of the DTO contract.
	solReserves := new(big.Int).SetUint64(state.VirtualSolReserves)
	solReserves.Add(solReserves, new(big.Int).SetUint64(state.RealSolReserves))
	tokenReserves := new(big.Int).SetUint64(state.VirtualTokenReserves)
	tokenReserves.Add(tokenReserves, new(big.Int).SetUint64(state.RealTokenReserves))

	out := in
	out.ReserveBaseRaw = solReserves.String()
	out.ReserveTokenRaw = tokenReserves.String()

	// LiquidityUsd requires a SOL/USD quote. Without one, leave LpStatsKnown
	// false so DQ degrades on missing liquidity rather than seeing a
	// fabricated zero/negative figure.
	if p.solUsd != nil {
		if px, ok := p.solUsd.SolUsd(cctx); ok && px > 0 {
			solFloat, _ := strconv.ParseFloat(solReserves.String(), 64)
			out.LiquidityUsd = (solFloat / lamportsPerSol) * px
			out.LpStatsKnown = true
		}
	}

	// Set TotalSupply when ingestion missed it.
	if !out.TotalSupplyKnown && state.TokenTotalSupply > 0 {
		out.TotalSupply = float64(state.TokenTotalSupply)
		out.TotalSupplyKnown = true
	}
	return out, nil
}
