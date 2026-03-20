package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUsesDefaultsWithoutConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("S3ASYNC_PROFILE", "")
	t.Setenv("S3ASYNC_REGION", "")
	t.Setenv("S3ASYNC_BUCKET", "")
	t.Setenv("S3ASYNC_PREFIX", "")
	t.Setenv("S3ASYNC_WORKERS", "")
	t.Setenv("S3ASYNC_DATABASE_PATH", "")
	t.Setenv("S3ASYNC_RETRY_MAX_ATTEMPTS", "")
	t.Setenv("S3ASYNC_RETRY_BACKOFF_MS", "")
	t.Setenv("S3ASYNC_FILTERS_INCLUDE", "")
	t.Setenv("S3ASYNC_FILTERS_EXCLUDE", "")
	t.Setenv("S3ASYNC_SECURITY_REDACT_LOGS", "")
	t.Setenv("S3ASYNC_SECURITY_DRY_RUN", "")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Workers != 4 {
		t.Fatalf("Load() workers = %d, want 4", cfg.Workers)
	}
	wantDBPath := filepath.Join(os.Getenv("HOME"), ".s3async", "tasks.db")
	if cfg.DatabasePath != wantDBPath {
		t.Fatalf("Load() database path = %q, want %q", cfg.DatabasePath, wantDBPath)
	}
}

func TestLoadReadsConfigFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("bucket: from-file\nprefix: backups/\nsecurity:\n  dry_run: true\nworkers: 9\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Bucket != "from-file" {
		t.Fatalf("Load() bucket = %q, want %q", cfg.Bucket, "from-file")
	}
	if cfg.Prefix != "backups/" {
		t.Fatalf("Load() prefix = %q, want %q", cfg.Prefix, "backups/")
	}
	if !cfg.Security.DryRun {
		t.Fatalf("Load() dry run = %v, want true", cfg.Security.DryRun)
	}
	if cfg.Workers != 9 {
		t.Fatalf("Load() workers = %d, want 9", cfg.Workers)
	}
}

func TestLoadEnvOverridesConfigFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("bucket: from-file\nworkers: 9\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("S3ASYNC_BUCKET", "from-env")
	t.Setenv("S3ASYNC_WORKERS", "6")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Bucket != "from-env" {
		t.Fatalf("Load() bucket = %q, want %q", cfg.Bucket, "from-env")
	}
	if cfg.Workers != 6 {
		t.Fatalf("Load() workers = %d, want 6", cfg.Workers)
	}
}

func TestLoadReturnsErrorForExplicitMissingConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	_, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
}
