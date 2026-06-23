package config

import (
	"os"
	"path/filepath"
)

// SharedConfigDirRel is the repository-relative YAML directory (shared/config).
const SharedConfigDirRel = "shared/config"

// ResolveConfigDir returns the directory containing pipeline.yaml and sibling YAML files.
// Prefers shared/config (monorepo layout); falls back to config/ (Docker /app/config).
func ResolveConfigDir(cwd string) string {
	shared := filepath.Join(cwd, "shared", "config")
	if isDir(shared) {
		return shared
	}
	legacy := filepath.Join(cwd, "config")
	if isDir(legacy) {
		return legacy
	}
	return shared
}

// JoinConfigFile returns an absolute path to a YAML file under ResolveConfigDir(cwd).
func JoinConfigFile(cwd, filename string) string {
	return filepath.Join(ResolveConfigDir(cwd), filename)
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}
