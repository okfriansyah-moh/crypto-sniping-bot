package api

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"

	"crypto-sniping-bot/backend-dashboard/internal/api/activity"
	"crypto-sniping-bot/backend-dashboard/internal/api/commands"
	"crypto-sniping-bot/backend-dashboard/internal/api/configs"
	"crypto-sniping-bot/backend-dashboard/internal/api/dq"
	"crypto-sniping-bot/backend-dashboard/internal/api/executions"
	"crypto-sniping-bot/backend-dashboard/internal/api/gate"
	"crypto-sniping-bot/backend-dashboard/internal/api/health"
	"crypto-sniping-bot/backend-dashboard/internal/api/ingestion"
	"crypto-sniping-bot/backend-dashboard/internal/api/overview"
	"crypto-sniping-bot/backend-dashboard/internal/api/pipeline"
	"crypto-sniping-bot/backend-dashboard/internal/api/pnl"
	"crypto-sniping-bot/backend-dashboard/internal/api/positions"
	"crypto-sniping-bot/backend-dashboard/internal/api/posture"
	"crypto-sniping-bot/backend-dashboard/internal/api/probepending"
	"crypto-sniping-bot/backend-dashboard/internal/api/rescan"
)

// Deps are shared dependencies for dashboard API route registration.
type Deps struct {
	DB           database.Adapter
	PipelineCfg  *config.Config
	DashboardCfg *config.DashboardConfig
	StartTime    time.Time
}

// Register mounts dashboard read routes on mux.
func Register(mux *http.ServeMux, deps Deps) {
	if mux == nil {
		return
	}
	evidenceDir := gateEvidenceDir(deps.DashboardCfg)
	mux.Handle("GET /api/v1/overview", overview.NewHandler(deps.DB, deps.PipelineCfg, deps.StartTime, evidenceDir))
	mux.Handle("GET /api/v1/health", health.NewHandler(deps.DB, deps.PipelineCfg))
	mux.Handle("GET /api/v1/posture", posture.NewHandler(deps.DB, deps.PipelineCfg, evidenceDir))
	mux.Handle("GET /api/v1/ingestion", ingestion.NewHandler(deps.PipelineCfg))
	mux.Handle("GET /api/v1/executions", executions.NewHandler(deps.DB, deps.PipelineCfg))
	mux.Handle("GET /api/v1/rescan", rescan.NewHandler(deps.DB, deps.PipelineCfg))
	mux.Handle("GET /api/v1/pipeline", pipeline.NewHandler(deps.DB))
	mux.Handle("GET /api/v1/probes/pending", probepending.NewHandler(deps.DB))
	mux.Handle("GET /api/v1/positions", positions.NewHandler(deps.DB))
	mux.Handle("GET /api/v1/pnl", pnl.NewHandler(deps.DB))
	mux.Handle("GET /api/v1/dq", dq.NewHandler(deps.DB))
	mux.Handle("GET /api/v1/activity", activity.NewHandler(deps.DB, maxEventsPerRequest(deps.DashboardCfg)))
	mux.Handle("GET /api/v1/gate/evidence", gate.NewHandler(deps.DB, evidenceDir))
	mux.Handle("GET /api/v1/gate/brief", gate.NewBriefHandler(evidenceDir))
	mux.Handle("GET /api/v1/configs", configs.NewHandler(configManifestDir(deps.DashboardCfg)))

	pending := commands.NewPendingStore(confirmTTL(deps.DashboardCfg))
	mux.Handle("POST /api/v1/commands", commands.NewHandler(deps.DB, deps.DashboardCfg, pending))
	mux.Handle("POST /api/v1/commands/confirm", commands.NewConfirmHandler(deps.DB, deps.DashboardCfg, pending))
}

func confirmTTL(dash *config.DashboardConfig) time.Duration {
	if dash == nil || dash.DestructiveConfirmTTLSeconds <= 0 {
		return 60 * time.Second
	}
	return time.Duration(dash.DestructiveConfirmTTLSeconds) * time.Second
}

func maxEventsPerRequest(dash *config.DashboardConfig) int {
	if dash == nil || dash.MaxEventsPerRequest <= 0 {
		return 50
	}
	return dash.MaxEventsPerRequest
}

func gateEvidenceDir(dash *config.DashboardConfig) string {
	if dash == nil {
		return "output/logs"
	}
	return dash.GateEvidenceDir
}

func configManifestDir(dash *config.DashboardConfig) string {
	cwd, _ := os.Getwd()
	if dash == nil || dash.ConfigManifestDir == "" {
		return config.ResolveConfigDir(cwd)
	}
	dir := dash.ConfigManifestDir
	if dir == "config" || dir == config.SharedConfigDirRel {
		return config.ResolveConfigDir(cwd)
	}
	if !filepath.IsAbs(dir) {
		return filepath.Join(cwd, dir)
	}
	return dir
}
