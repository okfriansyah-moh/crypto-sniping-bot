package operator

import (
	"context"
	"fmt"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
)

// rescanPipelineQueryer is implemented by postgres.DB for optional rescan funnel stats.
type rescanPipelineQueryer interface {
	GetRescanPipelineStats(ctx context.Context, windowHours int) (*database.RescanPipelineStats, error)
}

// BuildPipelineStats maps adapter funnel counts to the dashboard pipeline view.
// chain filters supplementary rescan layer only; core funnel is window-global (adapter).
func BuildPipelineStats(
	ctx context.Context,
	db database.Adapter,
	windowHours int,
	chain string,
) (*contracts.PipelineStatsResponseDTO, error) {
	windowHours = database.CapDQWindowHours(windowHours)
	chain = normalizeChainFilter(chain)

	stats, err := db.GetPipelineStats(ctx, windowHours)
	if err != nil {
		return nil, fmt.Errorf("get pipeline stats: %w", err)
	}

	out := &contracts.PipelineStatsResponseDTO{
		WindowHours:     windowHours,
		Chain:           chain,
		Funnel:          mapPipelineFunnel(stats),
		LayerHeartbeats: buildLayerHeartbeats(stats),
	}

	if chain == "" {
		if rq, ok := db.(rescanPipelineQueryer); ok {
			rs, rsErr := rq.GetRescanPipelineStats(ctx, windowHours)
			if rsErr == nil && rs != nil && rs.Detected > 0 {
				out.LayerHeartbeats = append([]contracts.LayerHeartbeatDTO{{
					Layer:      "L0.5",
					Stage:      "Rescan",
					Count24h:   rs.Detected,
					DropPct:    "—",
					Status:     "ok",
					LastSeenAt: "",
				}}, out.LayerHeartbeats...)
			}
		}
	}

	return out, nil
}

// FetchPipelineStats returns raw adapter funnel stats for Telegram /pipeline formatting.
func FetchPipelineStats(ctx context.Context, db database.Adapter, windowHours int) (*database.PipelineStats, error) {
	windowHours = database.CapDQWindowHours(windowHours)
	stats, err := db.GetPipelineStats(ctx, windowHours)
	if err != nil {
		return nil, fmt.Errorf("get pipeline stats: %w", err)
	}
	return stats, nil
}

func mapPipelineFunnel(stats *database.PipelineStats) contracts.PipelineFunnelDTO {
	if stats == nil {
		return contracts.PipelineFunnelDTO{}
	}
	return contracts.PipelineFunnelDTO{
		Detected:     stats.Detected,
		DQPassed:     stats.DQPassed,
		FeatureReady: stats.FeatureReady,
		EdgeDetected: stats.EdgeDetected,
		Validated:    stats.Validated,
		Selected:     stats.Selected,
		Executed:     stats.Executed,
		PositionOpen: stats.PositionOpen,
		Evaluated:    stats.Evaluated,
		Rejected:     stats.Rejected,
		Failed:       stats.Failed,
	}
}

type layerSpec struct {
	layer string
	stage string
	count int64
}

func buildLayerHeartbeats(stats *database.PipelineStats) []contracts.LayerHeartbeatDTO {
	if stats == nil {
		return []contracts.LayerHeartbeatDTO{}
	}

	specs := []layerSpec{
		{"L0", "Ingestion", stats.Detected},
		{"L1", "Data quality", stats.DQPassed},
		{"L2", "Features", stats.FeatureReady},
		{"L3", "Edge", stats.EdgeDetected},
		{"L5", "Validation", stats.Validated},
		{"L6", "Selection", stats.Selected},
		{"L8", "Execution", stats.Executed},
		{"L9", "Position", stats.PositionOpen},
		{"L10", "Learning", stats.Evaluated},
	}

	out := make([]contracts.LayerHeartbeatDTO, 0, len(specs))
	var prev int64
	for i, spec := range specs {
		drop := "—"
		if i > 0 {
			drop = funnelDropPct(prev, spec.count)
		}
		out = append(out, contracts.LayerHeartbeatDTO{
			Layer:      spec.layer,
			Stage:      spec.stage,
			Count24h:   spec.count,
			DropPct:    drop,
			Status:     layerHeartbeatStatus(spec.layer, spec.count),
			LastSeenAt: "",
		})
		prev = spec.count
	}
	return out
}

func funnelDropPct(prev, curr int64) string {
	if prev == 0 {
		return "—"
	}
	retained := float64(curr) / float64(prev) * 100
	dropped := 100.0 - retained
	if dropped < 0 {
		dropped = 0
	}
	return fmt.Sprintf("%.0f%%", dropped)
}

func layerHeartbeatStatus(layer string, count int64) string {
	if count > 0 {
		return "ok"
	}
	if layer == "L10" {
		return "stalled"
	}
	return "warn"
}
