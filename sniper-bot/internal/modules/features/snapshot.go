package features

import (
	"math"
	"strconv"
	"strings"
	"time"

	"crypto-sniping-bot/shared/contracts"
)

// MarketSnapshot is the immutable view of upstream MarketDataDTO fields the
// Layer-2 feature extractor needs. The worker constructs it from the
// originating MarketDataDTO and passes it into the pure module.
//
// Every numeric field is paired with a *Known boolean so the zero value is
// not silently treated as "0 measured". The module derives FeatureConfidence
// from the populated subset.
type MarketSnapshot struct {
	Market         string
	Chain          string
	BlockTimestamp time.Time

	// Liquidity
	LiquidityUsd        float64
	LpStatsKnown        bool
	SingleLpProviderPct float64
	LpChurnDetected     bool
	PoolAgeSeconds      int32

	// LP lock
	LpLockKnown    bool
	LpLockStrength float64
	LpLockDays     int32

	// Wash / activity
	WashStatsKnown  bool
	TxCount1m       int32
	UniqueWallets1m int32
	WalletEntropy   float64
	RepeatRatio1m   float64

	// Holder distribution
	HolderDistKnown bool
	Top5HolderPct   float64
	HolderCount     int32

	// Price (best-effort raw amounts; module will derive log returns where possible)
	ReserveBaseRaw  string
	ReserveTokenRaw string

	// Solana-specific (additive)
	BondingCurveProgressBps int32

	// AI Narrative enrichment (additive) — threaded from MarketDataDTO.
	// NarrativeKnown=false when the ai_narrative probe has not run.
	NarrativeScore float64
	NarrativeKnown bool
}

// MarketSnapshotFromDTO extracts the relevant fields from a MarketDataDTO.
// Returns an empty snapshot when md is nil — callers must handle the
// "no market data" case via the *Known flags.
func MarketSnapshotFromDTO(md *contracts.MarketDataDTO) MarketSnapshot {
	if md == nil {
		return MarketSnapshot{}
	}
	bt, _ := time.Parse(time.RFC3339Nano, md.BlockTimestamp)
	return MarketSnapshot{
		Market:                  md.Market,
		Chain:                   md.Chain,
		BlockTimestamp:          bt,
		LiquidityUsd:            md.LiquidityUsd,
		LpStatsKnown:            md.LpStatsKnown,
		SingleLpProviderPct:     md.SingleLpProviderPct,
		LpChurnDetected:         md.LpChurnDetected,
		PoolAgeSeconds:          md.PoolAgeSeconds,
		LpLockKnown:             md.LpLockKnown,
		LpLockStrength:          md.LpLockStrength,
		LpLockDays:              md.LpLockDays,
		WashStatsKnown:          md.WashStatsKnown,
		TxCount1m:               md.TxCount1m,
		UniqueWallets1m:         md.UniqueWallets1m,
		WalletEntropy:           md.WalletEntropy,
		RepeatRatio1m:           md.RepeatRatio1m,
		HolderDistKnown:         md.HolderDistKnown,
		Top5HolderPct:           md.Top5HolderPct,
		HolderCount:             md.HolderCount,
		ReserveBaseRaw:          md.ReserveBaseRaw,
		ReserveTokenRaw:         md.ReserveTokenRaw,
		BondingCurveProgressBps: md.BondingCurveProgressBps,
		NarrativeScore:          md.NarrativeScore,
		NarrativeKnown:          md.NarrativeKnown,
	}
}

// PriceFromReserves returns the token-per-base price as a float, or 0
// when either reserve is unparseable or zero. Used as a deterministic
// raw input for the price-momentum signal.
func PriceFromReserves(reserveBaseRaw, reserveTokenRaw string) float64 {
	base, errB := parseDecimal(reserveBaseRaw)
	tok, errT := parseDecimal(reserveTokenRaw)
	if errB != nil || errT != nil || base == 0 || tok == 0 {
		return 0
	}
	return base / tok
}

func parseDecimal(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errEmptyDecimal
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, errInvalidDecimal
	}
	return v, nil
}

var (
	errEmptyDecimal   = stringErr("empty")
	errInvalidDecimal = stringErr("invalid")
)

type stringErr string

func (e stringErr) Error() string { return string(e) }
