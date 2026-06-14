package operator_test

import (
	"os"
	"path/filepath"
	"testing"

	"crypto-sniping-bot/internal/operator"
)

func TestBuildConfigManifest_TopLevelKeysOnly(t *testing.T) {
	t.Parallel()
	root := findRepoRoot(t)
	configDir := filepath.Join(root, "shared", "config")

	got, err := operator.BuildConfigManifest(configDir)
	if err != nil {
		t.Fatalf("BuildConfigManifest: %v", err)
	}
	if len(got) < 10 {
		t.Fatalf("expected >=10 yaml files, got %d", len(got))
	}

	names := make(map[string]struct{}, len(got))
	for _, entry := range got {
		names[entry.Filename] = struct{}{}
		if entry.SHA256Prefix == "" || len(entry.SHA256Prefix) != 8 {
			t.Fatalf("bad sha prefix for %s: %q", entry.Filename, entry.SHA256Prefix)
		}
		if entry.LastModified == "" {
			t.Fatalf("missing last_modified for %s", entry.Filename)
		}
		if len(entry.TopLevelKeys) == 0 {
			t.Fatalf("expected top-level keys for %s", entry.Filename)
		}
	}
	if _, ok := names["pipeline.yaml"]; !ok {
		t.Fatal("pipeline.yaml missing from manifest")
	}
}

func TestBuildConfigManifest_MissingDirGraceful(t *testing.T) {
	t.Parallel()
	got, err := operator.BuildConfigManifest(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("BuildConfigManifest: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("got %+v, want empty slice", got)
	}
}

func TestBuildConfigManifest_RejectsNonYaml(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("secret=value"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sample.yaml"), []byte("execution:\n  mode: shadow\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := operator.BuildConfigManifest(dir)
	if err != nil {
		t.Fatalf("BuildConfigManifest: %v", err)
	}
	if len(got) != 1 || got[0].Filename != "sample.yaml" {
		t.Fatalf("got %+v", got)
	}
	if len(got[0].TopLevelKeys) != 1 || got[0].TopLevelKeys[0] != "execution" {
		t.Fatalf("keys = %+v", got[0].TopLevelKeys)
	}
}
