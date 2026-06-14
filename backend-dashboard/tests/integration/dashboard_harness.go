package integration

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"crypto-sniping-bot/backend-dashboard/internal/api"
	"crypto-sniping-bot/backend-dashboard/internal/auth"
	"crypto-sniping-bot/internal/app/config"
)

const testDashboardAPIKey = "integration-test-dashboard-key"

func newDashboardTestServer(t *testing.T, db *dashboardFixtureDB) *httptest.Server {
	t.Helper()

	t.Setenv("DASHBOARD_API_KEY", testDashboardAPIKey)
	t.Setenv("DASHBOARD_ALLOWED_OPERATORS", "integration-operator")

	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "pipeline.yaml"), []byte("execution:\n  mode: shadow\n"), 0o644); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}
	gateDir := t.TempDir()

	dashCfg := &config.DashboardConfig{
		ListenPort:                   8090,
		PollIntervalSeconds:          30,
		MaxEventsPerRequest:          50,
		GateEvidenceDir:              gateDir,
		ConfigManifestDir:            configDir,
		DestructiveConfirmTTLSeconds: 60,
		CorsAllowedOrigins:           []string{"http://localhost:5174"},
	}

	pipelineCfg := &config.Config{
		Logging:   config.LoggingConfig{Level: "error", Format: "text"},
		Execution: config.ExecutionConfig{Mode: "shadow"},
		Capital:   config.CapitalConfig{MaxTotalExposureUsd: 50},
	}

	mux := http.NewServeMux()
	api.Register(mux, api.Deps{
		DB:           db,
		PipelineCfg:  pipelineCfg,
		DashboardCfg: dashCfg,
		StartTime:    time.Now().UTC().Add(-time.Hour),
	})

	handler := auth.WrapHandler(mux, dashCfg)
	return httptest.NewServer(handler)
}

func dashboardAuthRequest(t *testing.T, method, url string, body []byte) *http.Request {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Dashboard-Key", testDashboardAPIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}
