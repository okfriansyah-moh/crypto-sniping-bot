package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// DashboardConfig holds operator-dashboard API settings (config/dashboard.yaml).
// Loaded via LoadDashboardConfig — not merged into sniper-bot Config.Load().
// API authentication uses DASHBOARD_API_KEY env var only (never YAML).
type DashboardConfig struct {
	ListenPort                   int      `yaml:"listen_port"`
	CorsAllowedOrigins           []string `yaml:"cors_allowed_origins"`
	PollIntervalSeconds          int      `yaml:"poll_interval_seconds"`
	MaxEventsPerRequest          int      `yaml:"max_events_per_request"`
	GateEvidenceDir              string   `yaml:"gate_evidence_dir"`
	ConfigManifestDir            string   `yaml:"config_manifest_dir"`
	DestructiveConfirmTTLSeconds int      `yaml:"destructive_confirm_ttl_seconds"`
}

type dashboardFile struct {
	Dashboard DashboardConfig `yaml:"dashboard"`
}

const (
	defaultDashboardListenPort          = 8090
	defaultDashboardPollIntervalSeconds = 30
	defaultDashboardMaxEventsPerRequest = 50
	defaultGateEvidenceDir              = "output/logs"
	defaultConfigManifestDir            = SharedConfigDirRel
	defaultDestructiveConfirmTTLSeconds = 60
	maxDashboardEventsPerRequest        = 200
)

// LoadDashboardConfig reads config/dashboard.yaml (or explicit paths) and applies defaults.
func LoadDashboardConfig(paths ...string) (*DashboardConfig, error) {
	if len(paths) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("dashboard config: get working directory: %w", err)
		}
		paths = []string{JoinConfigFile(cwd, "dashboard.yaml")}
	}

	var merged dashboardFile
	for _, path := range paths {
		if err := loadDashboardFile(path, &merged); err != nil {
			return nil, err
		}
	}

	applyDashboardDefaults(&merged.Dashboard)
	if err := merged.Dashboard.Validate(); err != nil {
		return nil, err
	}
	return &merged.Dashboard, nil
}

func loadDashboardFile(path string, out *dashboardFile) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("dashboard config: read %s: %w", path, err)
	}
	data := []byte(os.ExpandEnv(string(raw)))
	if err := yaml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("dashboard config: parse %s: %w", path, err)
	}
	return nil
}

func applyDashboardDefaults(d *DashboardConfig) {
	if d.ListenPort == 0 {
		d.ListenPort = defaultDashboardListenPort
	}
	if d.PollIntervalSeconds == 0 {
		d.PollIntervalSeconds = defaultDashboardPollIntervalSeconds
	}
	if d.MaxEventsPerRequest == 0 {
		d.MaxEventsPerRequest = defaultDashboardMaxEventsPerRequest
	}
	if d.MaxEventsPerRequest > maxDashboardEventsPerRequest {
		d.MaxEventsPerRequest = maxDashboardEventsPerRequest
	}
	if strings.TrimSpace(d.GateEvidenceDir) == "" {
		d.GateEvidenceDir = defaultGateEvidenceDir
	}
	if strings.TrimSpace(d.ConfigManifestDir) == "" {
		d.ConfigManifestDir = defaultConfigManifestDir
	}
	if d.DestructiveConfirmTTLSeconds == 0 {
		d.DestructiveConfirmTTLSeconds = defaultDestructiveConfirmTTLSeconds
	}
	if len(d.CorsAllowedOrigins) == 0 {
		d.CorsAllowedOrigins = []string{"http://localhost:5173"}
	}
}

// Validate checks dashboard YAML values after defaults are applied.
func (d *DashboardConfig) Validate() error {
	if d == nil {
		return fmt.Errorf("dashboard config: nil")
	}
	if d.ListenPort < 1 || d.ListenPort > 65535 {
		return fmt.Errorf("dashboard config: listen_port %d out of range [1,65535]", d.ListenPort)
	}
	if d.PollIntervalSeconds < 1 {
		return fmt.Errorf("dashboard config: poll_interval_seconds must be >= 1")
	}
	if d.MaxEventsPerRequest < 1 || d.MaxEventsPerRequest > maxDashboardEventsPerRequest {
		return fmt.Errorf("dashboard config: max_events_per_request must be in [1,%d]", maxDashboardEventsPerRequest)
	}
	if strings.TrimSpace(d.GateEvidenceDir) == "" {
		return fmt.Errorf("dashboard config: gate_evidence_dir required")
	}
	if strings.TrimSpace(d.ConfigManifestDir) == "" {
		return fmt.Errorf("dashboard config: config_manifest_dir required")
	}
	if d.DestructiveConfirmTTLSeconds < 1 {
		return fmt.Errorf("dashboard config: destructive_confirm_ttl_seconds must be >= 1")
	}
	return nil
}

// DashboardAPIKey returns the API key from DASHBOARD_API_KEY env (never YAML).
func DashboardAPIKey() string {
	return strings.TrimSpace(os.Getenv("DASHBOARD_API_KEY"))
}

// DashboardAuthFailClosed reports whether the API must reject unauthenticated
// requests because no DASHBOARD_API_KEY is configured (production-safe default).
func DashboardAuthFailClosed() bool {
	return DashboardAPIKey() == ""
}

// DashboardAllowedOperators returns operator IDs permitted to issue dashboard commands.
// Env-only: DASHBOARD_ALLOWED_OPERATORS (comma-separated). Empty means fail-closed for writes.
func DashboardAllowedOperators() []string {
	raw := strings.TrimSpace(os.Getenv("DASHBOARD_ALLOWED_OPERATORS"))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
