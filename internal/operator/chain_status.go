package operator

import (
	"context"
	"fmt"
	"strings"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
)

// BuildChainStatuses assembles per-chain overview strip cards.
func BuildChainStatuses(
	ctx context.Context,
	db database.Adapter,
	cfg *config.Config,
	evidenceDir string,
) ([]contracts.ChainStatusDTO, error) {
	verdict, err := DeriveThroughputVerdict(ctx, db, evidenceDir)
	if err != nil {
		return nil, fmt.Errorf("throughput verdict: %w", err)
	}

	open, err := db.GetOpenPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("get open positions: %w", err)
	}
	openByChain := map[string]int32{}
	for _, p := range open {
		chain := strings.ToLower(strings.TrimSpace(p.Chain))
		if chain == "" {
			chain = "solana"
		}
		openByChain[chain]++
	}

	stats1h, err := db.GetPipelineStats(ctx, 1)
	if err != nil {
		return nil, fmt.Errorf("get pipeline stats 1h: %w", err)
	}
	ingestionPerHour := int64(0)
	if stats1h != nil {
		ingestionPerHour = stats1h.Detected
	}

	out := make([]contracts.ChainStatusDTO, 0, 4)
	primaryChain := "solana"
	if cfg != nil && strings.TrimSpace(cfg.Solana.ChainID) != "" {
		primaryChain = strings.ToLower(cfg.Solana.ChainID)
	}
	out = append(out, contracts.ChainStatusDTO{
		Chain:             primaryChain,
		Label:             chainLabel(primaryChain),
		IngestionPerHour:  ingestionPerHour,
		OpenPositions:     openByChain[primaryChain],
		ThroughputVerdict: verdict,
		Status:            chainStatusLevel(verdict, ingestionPerHour),
	})

	if cfg != nil {
		for name := range cfg.Chains {
			chainID := strings.ToLower(strings.TrimSpace(name))
			if chainID == "" || chainID == primaryChain {
				continue
			}
			out = append(out, contracts.ChainStatusDTO{
				Chain:             chainID,
				Label:             chainLabel(chainID),
				IngestionPerHour:  0,
				OpenPositions:     openByChain[chainID],
				ThroughputVerdict: verdict,
				Status:            chainStatusLevel(verdict, 0),
			})
		}
	}
	return out, nil
}

func chainLabel(chain string) string {
	switch strings.ToLower(chain) {
	case "solana", "sol":
		return "Solana"
	case "eth", "ethereum":
		return "Ethereum"
	case "bsc", "bnb":
		return "BSC"
	case "base":
		return "Base"
	default:
		if chain == "" {
			return "Unknown"
		}
		return strings.ToUpper(chain[:1]) + chain[1:]
	}
}

func chainStatusLevel(verdict string, ingestionPerHour int64) string {
	switch verdict {
	case "CODE_DEFECT":
		return "bad"
	case "MARKET_QUIET":
		if ingestionPerHour == 0 {
			return "warn"
		}
		return "ok"
	case "GUARDRAILS_ACTIVE":
		return "warn"
	default:
		if ingestionPerHour == 0 {
			return "warn"
		}
		return "ok"
	}
}
