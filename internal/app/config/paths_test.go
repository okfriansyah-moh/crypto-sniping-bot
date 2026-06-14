package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveConfigDir_prefersSharedConfig(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(cwd, "..", "..", "..")
	root, err = filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	got := ResolveConfigDir(root)
	want := filepath.Join(root, "shared", "config")
	if got != want {
		t.Fatalf("ResolveConfigDir: got %q want %q", got, want)
	}
}
