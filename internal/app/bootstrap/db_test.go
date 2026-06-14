package bootstrap_test

import (
	"os"
	"path/filepath"
	"testing"

	"crypto-sniping-bot/internal/app/bootstrap"
	"crypto-sniping-bot/internal/app/config"
)

func TestBuildDBConfig_MapsPipelineDatabase(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Engine:   "postgres",
			Host:     "db.local",
			Port:     5432,
			Database: "sniper",
			User:     "sniper",
			SSLMode:  "disable",
			Pool: config.PoolConfig{
				MaxOpenConns:        10,
				MaxIdleConns:        5,
				ConnMaxLifetimeSecs: 300,
			},
		},
	}
	got := bootstrap.BuildDBConfig(cfg)
	if got.Host != "db.local" || got.Database != "sniper" || got.Port != 5432 {
		t.Fatalf("BuildDBConfig: %+v", got)
	}
}

func TestFindPipelineConfigPath_FromRepoRoot(t *testing.T) {
	root := repoRoot(t)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_PATH", "")
	path, err := bootstrap.FindPipelineConfigPath()
	if err != nil {
		t.Fatalf("FindPipelineConfigPath: %v", err)
	}
	if filepath.Base(path) != "pipeline.yaml" {
		t.Fatalf("path = %q", path)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
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
