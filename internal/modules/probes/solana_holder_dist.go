package probes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"crypto-sniping-bot/contracts"
)

// SolanaHolderDistConfig configures the solana_holder_dist probe.
type SolanaHolderDistConfig struct {
	Enabled    bool   `yaml:"enabled"`
	TimeoutMs  int    `yaml:"timeout_ms"`
	Commitment string `yaml:"commitment"`
	// TopK is the number of largest holders summed for Top5HolderPct.
	// MUST be 5 — the DTO field is named Top5HolderPct and the downstream
	// data-quality layer interprets it as such. Any value other than 5 is
	// clamped back to 5 by NewSolanaHolderDistProbe to avoid semantic
	// corruption. To support a different K, a new DTO field is required.
	TopK int `yaml:"top_k"`
}

// SolanaHolderDistProbe populates HolderCount, Top5HolderPct and
// HolderDistKnown from getTokenLargestAccounts. The Solana RPC returns
// up to 20 entries — large pump.fun pools typically saturate this.
type SolanaHolderDistProbe struct {
	rpc    SolanaProbeRPCClient
	cfg    SolanaHolderDistConfig
	logger *slog.Logger
}

func NewSolanaHolderDistProbe(rpc SolanaProbeRPCClient, cfg SolanaHolderDistConfig, logger *slog.Logger) *SolanaHolderDistProbe {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Commitment == "" {
		cfg.Commitment = "confirmed"
	}
	// Clamp TopK to exactly 5: the DTO field is Top5HolderPct and any
	// other value produces a semantically incorrect result.
	if cfg.TopK != 5 {
		cfg.TopK = 5
	}
	return &SolanaHolderDistProbe{rpc: rpc, cfg: cfg, logger: logger}
}

func (p *SolanaHolderDistProbe) Name() string { return "solana_holder_dist" }

// Probe sums the top-K holder amounts and divides by total supply when known.
// Skips non-Solana inputs. If total supply is unknown, it falls back to using
// the sum of the returned largest-holder balances as the denominator, so the
// resulting percentage is relative to the returned holder set rather than the
// full mint supply.
func (p *SolanaHolderDistProbe) Probe(ctx context.Context, in contracts.MarketDataDTO) (contracts.MarketDataDTO, error) {
	if !strings.EqualFold(in.Chain, "solana") {
		return in, nil
	}
	mint := strings.TrimSpace(in.TokenAddress)
	if mint == "" {
		return in, errors.New("probes/solana_holder_dist: empty token address")
	}
	if p.rpc == nil {
		return in, errors.New("probes/solana_holder_dist: nil rpc client")
	}

	timeout := time.Duration(p.cfg.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 300 * time.Millisecond
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	holders, err := p.rpc.GetTokenLargestAccounts(cctx, mint, p.cfg.Commitment)
	if err != nil {
		return in, fmt.Errorf("probes/solana_holder_dist: get_token_largest_accounts: %w", err)
	}
	if len(holders) == 0 {
		// No holders returned: possible for a freshly minted SPL token.
		// Leave HolderDistKnown=false so DQ profile drives the decision.
		return in, nil
	}

	topK := p.cfg.TopK
	if topK > len(holders) {
		topK = len(holders)
	}
	topSum := new(big.Int)
	for i := 0; i < topK; i++ {
		amt, ok := new(big.Int).SetString(holders[i].Amount, 10)
		if !ok || amt.Sign() < 0 {
			return in, fmt.Errorf("probes/solana_holder_dist: invalid amount %q", holders[i].Amount)
		}
		topSum.Add(topSum, amt)
	}

	out := in
	out.HolderCount = int32(len(holders))

	// Compute pct only when we have a reliable denominator. Prefer the
	// raw token-account decimals (holders[0].Decimals) and the DTO's
	// TotalSupply (already decimal-adjusted for some chains; for SPL it
	// is the raw u64). To stay portable, sum ALL returned holders as
	// the denominator floor when TotalSupply is unknown.
	denom := new(big.Float)
	if in.TotalSupplyKnown && in.TotalSupply > 0 {
		denom.SetFloat64(in.TotalSupply)
	} else {
		// Fall back to "total returned" — biased high (pct biased low),
		// but never produces > 1.0.
		fullSum := new(big.Int)
		for _, h := range holders {
			amt, ok := new(big.Int).SetString(h.Amount, 10)
			if !ok || amt.Sign() < 0 {
				continue
			}
			fullSum.Add(fullSum, amt)
		}
		if fullSum.Sign() == 0 {
			return in, nil
		}
		denom.SetInt(fullSum)
	}

	num := new(big.Float).SetInt(topSum)
	pct := new(big.Float).Quo(num, denom)
	pctF, _ := pct.Float64()
	if pctF < 0 {
		pctF = 0
	}
	if pctF > 1 {
		pctF = 1
	}
	out.Top5HolderPct = pctF
	out.HolderDistKnown = true
	return out, nil
}
