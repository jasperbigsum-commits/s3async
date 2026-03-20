package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromConfigFileAndEnv(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	content := []byte("bucket: file-bucket\nregion: ap-southeast-1\nworkers: 6\nfilters:\n  include:\n    - \"*.txt\"\n")
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	t.Setenv("S3ASYNC_BUCKET", "env-bucket")
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Bucket != "env-bucket" {
		t.Fatalf("Load() bucket = %s, want env-bucket", cfg.Bucket)
	}
	if cfg.Region != "ap-southeast-1" {
		t.Fatalf("Load() region = %s, want ap-southeast-1", cfg.Region)
	}
	if cfg.Workers != 6 {
		t.Fatalf("Load() workers = %d, want 6", cfg.Workers)
	}
	if len(cfg.Filters.Include) != 1 || cfg.Filters.Include[0] != "*.txt" {
		t.Fatalf("Load() includes = %#v, want [*.txt]", cfg.Filters.Include)
	}
}
