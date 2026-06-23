package operator

import (
	"context"
	"fmt"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
)

type rescanStatsQueryer interface {
	GetRescanStats(ctx context.Context, windowHours int) (*database.RescanStats, error)
}

// BuildRescanStats returns per-band 24h emission counts aligned with configured bands.
func BuildRescanStats(
	ctx context.Context,
	db database.Adapter,
	cfg *config.Config,
) (*contracts.RescanStatusResponseDTO, error) {
	out := &contracts.RescanStatusResponseDTO{
		Bands: []contracts.RescanBandStatsDTO{},
	}
	if cfg != nil {
		out.Enabled = cfg.Rescan.Enabled
		for _, band := range cfg.Rescan.Bands {
			out.Bands = append(out.Bands, contracts.RescanBandStatsDTO{
				Band:  band.Name,
				Phase: rescanBandPhase(band.Name),
			})
		}
	}

	rq, ok := db.(rescanStatsQueryer)
	if !ok {
		return out, nil
	}
	rs, err := rq.GetRescanStats(ctx, 24)
	if err != nil {
		return nil, fmt.Errorf("get rescan stats: %w", err)
	}
	if rs == nil {
		return out, nil
	}
	out.TotalEmitted24h = rs.TotalEmitted
	if len(out.Bands) == 0 && len(rs.ByBand) > 0 {
		for name := range rs.ByBand {
			out.Bands = append(out.Bands, contracts.RescanBandStatsDTO{
				Band:  name,
				Phase: rescanBandPhase(name),
			})
		}
	}
	for i := range out.Bands {
		if rs.ByBand != nil {
			out.Bands[i].Emitted24h = rs.ByBand[out.Bands[i].Band]
		}
	}
	return out, nil
}

// rescanBandPhase maps band names to fortress rescan goal lanes (A/B/C).
func rescanBandPhase(name string) string {
	switch name {
	case "12h", "24h":
		return "B"
	case "36h", "48h":
		return "C"
	default:
		return "A"
	}
}
