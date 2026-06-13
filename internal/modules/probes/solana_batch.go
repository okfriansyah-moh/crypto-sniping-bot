package probes

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"crypto-sniping-bot/contracts"
)

// BatchAccountRequest describes which on-chain accounts to fetch in one RPC.
type BatchAccountRequest struct {
	Mint       string
	Pool       string
	Commitment string
}

// BatchAccountResult holds fetched accounts keyed by logical role.
type BatchAccountResult struct {
	Mint *SolanaAccountData
	Pool *SolanaAccountData
}

// Pubkeys returns the ordered pubkey list and index map for decoding the
// getMultipleAccounts response. Empty when nothing is requested.
func (r BatchAccountRequest) Pubkeys() (pubkeys []string, mintIdx, poolIdx int) {
	mintIdx, poolIdx = -1, -1
	if m := strings.TrimSpace(r.Mint); m != "" && isValidSolanaMint(m) {
		mintIdx = len(pubkeys)
		pubkeys = append(pubkeys, m)
	}
	if p := strings.TrimSpace(r.Pool); p != "" && isValidSolanaMint(p) {
		poolIdx = len(pubkeys)
		pubkeys = append(pubkeys, p)
	}
	return pubkeys, mintIdx, poolIdx
}

// NeedsFetch reports whether the request would issue any RPC calls.
func (r BatchAccountRequest) NeedsFetch() bool {
	pubkeys, _, _ := r.Pubkeys()
	return len(pubkeys) > 0
}

// BatchAccountRequestFor builds a batch request for new-token Solana events.
// Skips accounts whose *Known flags are already true upstream.
func BatchAccountRequestFor(in contracts.MarketDataDTO, authoritiesEnabled, pumpfunLpEnabled bool, commitment string) BatchAccountRequest {
	if !strings.EqualFold(in.Chain, "solana") {
		return BatchAccountRequest{}
	}
	if commitment == "" {
		commitment = "confirmed"
	}
	req := BatchAccountRequest{Commitment: commitment}
	if authoritiesEnabled && !in.SolanaAuthoritiesKnown {
		req.Mint = strings.TrimSpace(in.TokenAddress)
	}
	if pumpfunLpEnabled &&
		strings.HasPrefix(strings.ToLower(in.Market), "solana-pumpfun") &&
		!in.LpStatsKnown {
		req.Pool = strings.TrimSpace(in.PoolAddress)
	}
	return req
}

// FetchBatchAccounts issues a single getMultipleAccounts for the requested
// pubkeys. Missing accounts are returned as nil entries (fail-closed: callers
// must not set *Known flags for nil accounts).
func FetchBatchAccounts(ctx context.Context, rpc SolanaProbeRPCClient, req BatchAccountRequest) (*BatchAccountResult, error) {
	if rpc == nil {
		return nil, errors.New("probes/batch: nil rpc client")
	}
	pubkeys, mintIdx, poolIdx := req.Pubkeys()
	if len(pubkeys) == 0 {
		return &BatchAccountResult{}, nil
	}
	commitment := req.Commitment
	if commitment == "" {
		commitment = "confirmed"
	}

	accounts, err := rpc.GetMultipleAccounts(ctx, pubkeys, commitment)
	if err != nil {
		return nil, fmt.Errorf("probes/batch: get_multiple_accounts: %w", err)
	}
	if len(accounts) != len(pubkeys) {
		return nil, fmt.Errorf("probes/batch: response length %d != request %d", len(accounts), len(pubkeys))
	}

	out := &BatchAccountResult{}
	if mintIdx >= 0 {
		out.Mint = accounts[mintIdx]
	}
	if poolIdx >= 0 {
		out.Pool = accounts[poolIdx]
	}
	return out, nil
}

// ApplyBatchAccounts enriches the DTO from batch-fetched accounts. Accounts
// that are nil or fail decode leave the corresponding *Known flags false
// (fail-closed). solUsd may be nil — LP USD liquidity is skipped without a
// price source, matching the individual probe behaviour.
func ApplyBatchAccounts(ctx context.Context, in contracts.MarketDataDTO, res *BatchAccountResult, solUsd SolUsdSource, authoritiesEnabled, pumpfunLpEnabled bool) contracts.MarketDataDTO {
	if res == nil {
		return in
	}
	out := in
	if authoritiesEnabled && res.Mint != nil {
		if enriched, ok := enrichAuthoritiesFromAccount(out, res.Mint); ok {
			out = enriched
		}
	}
	if pumpfunLpEnabled && res.Pool != nil {
		if enriched, ok := enrichPumpfunLpFromAccount(ctx, out, res.Pool, solUsd); ok {
			out = enriched
		}
	}
	return out
}

func enrichAuthoritiesFromAccount(in contracts.MarketDataDTO, acc *SolanaAccountData) (contracts.MarketDataDTO, bool) {
	if acc == nil {
		return in, false
	}
	if acc.Owner != "" {
		if _, ok := splTokenProgramOwners[acc.Owner]; !ok {
			return in, false
		}
	}
	raw, err := base64.StdEncoding.DecodeString(acc.DataB64)
	if err != nil {
		return in, false
	}
	state, err := DecodeSPLMint(raw)
	if err != nil {
		return in, false
	}
	out := in
	out.MintAuthorityRenounced = state.MintAuthorityRenounced
	out.FreezeAuthorityRenounced = state.FreezeAuthorityRenounced
	out.SolanaAuthoritiesKnown = true
	return out, true
}

func enrichPumpfunLpFromAccount(ctx context.Context, in contracts.MarketDataDTO, acc *SolanaAccountData, solUsd SolUsdSource) (contracts.MarketDataDTO, bool) {
	if acc == nil {
		return in, false
	}
	raw, err := base64.StdEncoding.DecodeString(acc.DataB64)
	if err != nil {
		return in, false
	}
	state, err := DecodePumpfunBondingCurve(raw)
	if err != nil {
		return in, false
	}

	solReserves := new(big.Int).SetUint64(state.VirtualSolReserves)
	solReserves.Add(solReserves, new(big.Int).SetUint64(state.RealSolReserves))
	tokenReserves := new(big.Int).SetUint64(state.VirtualTokenReserves)
	tokenReserves.Add(tokenReserves, new(big.Int).SetUint64(state.RealTokenReserves))

	out := in
	out.ReserveBaseRaw = solReserves.String()
	out.ReserveTokenRaw = tokenReserves.String()

	if solUsd != nil {
		if px, ok := solUsd.SolUsd(ctx); ok && px > 0 {
			solFloat, _ := strconv.ParseFloat(solReserves.String(), 64)
			if solFloat > 0 {
				out.LiquidityUsd = (solFloat / lamportsPerSol) * px
				out.LpStatsKnown = true
			}
		}
	}

	if !out.TotalSupplyKnown && state.TokenTotalSupply > 0 {
		out.TotalSupply = float64(state.TokenTotalSupply) / pumpfunTokenDecimals
		out.TotalSupplyKnown = true
	}
	return out, true
}

// BatchFetchTimeout returns the max deadline for a batched account fetch.
// Uses the larger of authorities and pumpfun_lp probe timeouts when both apply.
func BatchFetchTimeout(authoritiesMs, pumpfunLpMs int) time.Duration {
	ms := authoritiesMs
	if pumpfunLpMs > ms {
		ms = pumpfunLpMs
	}
	if ms <= 0 {
		ms = 1000
	}
	return time.Duration(ms) * time.Millisecond
}
