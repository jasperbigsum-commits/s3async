package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jasperbigsum-commits/s3async/internal/logging"
)

func TestBootstrapCloseReleasesResources(t *testing.T) {
	base := t.TempDir()
	configPath := filepath.Join(base, "config.yaml")
	dbPath := filepath.Join(base, "tasks.db")
	config := fmt.Sprintf("database_path: %q\nstate_dir: %q\n", dbPath, base)
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	bootstrap, err := NewBootstrapWithConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bootstrap.Close() })
	if err := bootstrap.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrap.TaskService.ListTasks(); err == nil {
		t.Fatal("database is still usable after Close")
	}
	if err := bootstrap.recorder.Record(logging.AuditEvent{Event: "after_close"}); err == nil {
		t.Fatal("audit log is still writable after Close")
	}
	if err := bootstrap.Close(); err != nil {
		t.Fatalf("repeated Close: %v", err)
	}
	for _, path := range []string{dbPath, filepath.Join(base, "task-events.jsonl")} {
		if err := os.Remove(path); err != nil {
			t.Fatalf("resource remains locked: %v", err)
		}
	}
}

func TestBootstrapRecorderFailureClosesDatabase(t *testing.T) {
	base := t.TempDir()
	configPath := filepath.Join(base, "config.yaml")
	dbPath := filepath.Join(base, "tasks.db")
	// A directory in place of the log file forces recorder initialization to fail.
	if err := os.Mkdir(filepath.Join(base, "task-events.jsonl"), 0700); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf("database_path: %q\nstate_dir: %q\n", dbPath, base)
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewBootstrapWithConfig(configPath); err == nil {
		t.Fatal("expected recorder initialization failure")
	}
	if err := os.Remove(dbPath); err != nil {
		t.Fatalf("database remains locked after initialization failure: %v", err)
	}
}
