package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crypto-sniping-bot/internal/app/config"
)

func repoRootFromTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func TestLoadDashboardConfig_RealYAML(t *testing.T) {
	t.Parallel()
	root := repoRootFromTest(t)
	got, err := config.LoadDashboardConfig(filepath.Join(root, "shared", "config", "dashboard.yaml"))
	if err != nil {
		t.Fatalf("LoadDashboardConfig: %v", err)
	}
	if got.ListenPort != 8090 {
		t.Errorf("ListenPort = %d, want 8090", got.ListenPort)
	}
	if got.PollIntervalSeconds != 30 {
		t.Errorf("PollIntervalSeconds = %d, want 30", got.PollIntervalSeconds)
	}
	if got.MaxEventsPerRequest != 50 {
		t.Errorf("MaxEventsPerRequest = %d, want 50", got.MaxEventsPerRequest)
	}
	if got.GateEvidenceDir != "output/logs" {
		t.Errorf("GateEvidenceDir = %q", got.GateEvidenceDir)
	}
	if got.ConfigManifestDir != "shared/config" {
		t.Errorf("ConfigManifestDir = %q", got.ConfigManifestDir)
	}
	if got.DestructiveConfirmTTLSeconds != 60 {
		t.Errorf("DestructiveConfirmTTLSeconds = %d, want 60", got.DestructiveConfirmTTLSeconds)
	}
	if len(got.CorsAllowedOrigins) < 2 {
		t.Fatalf("CorsAllowedOrigins = %+v, want >=2 origins", got.CorsAllowedOrigins)
	}
}

func TestDashboardConfig_ApplyDefaults(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "dashboard.yaml")
	if err := os.WriteFile(path, []byte("dashboard: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := config.LoadDashboardConfig(path)
	if err != nil {
		t.Fatalf("LoadDashboardConfig: %v", err)
	}
	if got.ListenPort != 8090 || got.PollIntervalSeconds != 30 || got.MaxEventsPerRequest != 50 {
		t.Fatalf("defaults not applied: %+v", got)
	}
	if got.GateEvidenceDir != "output/logs" || got.ConfigManifestDir != "shared/config" {
		t.Fatalf("path defaults wrong: %+v", got)
	}
	if len(got.CorsAllowedOrigins) != 1 || got.CorsAllowedOrigins[0] != "http://localhost:5173" {
		t.Fatalf("cors default: %+v", got.CorsAllowedOrigins)
	}
}

func TestDashboardConfig_ValidateRejectsInvalidPort(t *testing.T) {
	t.Parallel()
	cfg := &config.DashboardConfig{ListenPort: 0}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for zero port after missing defaults")
	}
}

func TestDashboardConfig_MaxEventsCappedAt200(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "dashboard.yaml")
	content := "dashboard:\n  max_events_per_request: 500\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := config.LoadDashboardConfig(path)
	if err != nil {
		t.Fatalf("LoadDashboardConfig: %v", err)
	}
	if got.MaxEventsPerRequest != 200 {
		t.Fatalf("MaxEventsPerRequest = %d, want cap 200", got.MaxEventsPerRequest)
	}
}

func TestDashboardAuth_NoKeyInYAML(t *testing.T) {
	t.Parallel()
	root := repoRootFromTest(t)
	raw, err := os.ReadFile(filepath.Join(root, "shared", "config", "dashboard.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lower := strings.ToLower(trimmed)
		for _, forbidden := range []string{"api_key:", "password:", "secret:", "private_key:"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("dashboard.yaml must not set %q in YAML values: %q", forbidden, trimmed)
			}
		}
	}
}

func TestDashboardAuth_FailClosedWhenKeyUnset(t *testing.T) {
	t.Setenv("DASHBOARD_API_KEY", "")
	if !config.DashboardAuthFailClosed() {
		t.Fatal("expected fail-closed when DASHBOARD_API_KEY unset")
	}
	t.Setenv("DASHBOARD_API_KEY", "test-key")
	if config.DashboardAuthFailClosed() {
		t.Fatal("expected auth enabled when DASHBOARD_API_KEY set")
	}
	if config.DashboardAPIKey() != "test-key" {
		t.Fatalf("DashboardAPIKey = %q", config.DashboardAPIKey())
	}
}

func TestDashboardAllowedOperators_ParseEnv(t *testing.T) {
	t.Setenv("DASHBOARD_ALLOWED_OPERATORS", " alice , bob ")
	got := config.DashboardAllowedOperators()
	if len(got) != 2 || got[0] != "alice" || got[1] != "bob" {
		t.Fatalf("got %+v", got)
	}
	t.Setenv("DASHBOARD_ALLOWED_OPERATORS", "")
	if len(config.DashboardAllowedOperators()) != 0 {
		t.Fatal("expected nil/empty when unset")
	}
}
