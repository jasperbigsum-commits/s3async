package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jasperbigsum-commits/s3async/internal/app"
	internaldaemon "github.com/jasperbigsum-commits/s3async/internal/daemon"
	internallogging "github.com/jasperbigsum-commits/s3async/internal/logging"
	"github.com/jasperbigsum-commits/s3async/internal/store"
	taskpkg "github.com/jasperbigsum-commits/s3async/internal/task"
)
func TestTaskWorkerOnceWithoutQueuedTasks(t *testing.T) {
	configPath, _, _ := writeTestConfig(t)

	stdout, stderr, err := executeCommand(NewRootCmd(), "task", "worker", "--once", "--config", configPath)
	if err != nil {
		t.Fatalf("executeCommand() error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "worker found no queued tasks") {
		t.Fatalf("stdout = %q, want worker empty-queue message", stdout)
	}
}

func TestDaemonStopCreatesStopFile(t *testing.T) {
	configPath, stateDir, _ := writeTestConfig(t)

	stdout, stderr, err := executeCommand(NewRootCmd(), "daemon", "stop", "--config", configPath)
	if err != nil {
		t.Fatalf("executeCommand() error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "daemon stop requested") {
		t.Fatalf("stdout = %q, want daemon stop confirmation", stdout)
	}

	manager := internaldaemon.NewManager(stateDir)
	if !manager.StopRequested() {
		t.Fatal("StopRequested() = false, want true")
	}
	if _, err := os.Stat(manager.StopFile()); err != nil {
		t.Fatalf("os.Stat(stopFile) error = %v, want existing stop file", err)
	}
}

func TestTaskStatusRespectsFailedLimit(t *testing.T) {
	configPath, _, dbPath := writeTestConfig(t)
	repo, err := store.NewSQLiteTaskRepository(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteTaskRepository() error = %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	started := now.Add(time.Second)
	completed := now.Add(2 * time.Second)
	taskID := "task_status_limit"
	taskRecord := taskpkg.Task{
		ID:           taskID,
		Source:       "/tmp/source",
		Bucket:       "bucket",
		Prefix:       "prefix",
		Mode:         "update",
		Status:       taskpkg.StatusPartialFailed,
		TotalItems:   3,
		PendingItems: 0,
		SuccessItems: 1,
		FailedItems:  2,
		LastError:    "second failure",
		CreatedAt:    now,
		UpdatedAt:    now,
		StartedAt:    &started,
		CompletedAt:  &completed,
	}
	items := []taskpkg.Item{
		{
			TaskID:       taskID,
			Path:         "/tmp/source/a.txt",
			RelativePath: "a.txt",
			Size:         1,
			Status:       taskpkg.ItemStatusSuccess,
			AttemptCount: 1,
			CreatedAt:    now,
			UpdatedAt:    now,
			StartedAt:    &started,
			CompletedAt:  &completed,
		},
		{
			TaskID:       taskID,
			Path:         "/tmp/source/b.txt",
			RelativePath: "b.txt",
			Size:         2,
			Status:       taskpkg.ItemStatusFailed,
			Error:        "first failure",
			AttemptCount: 2,
			CreatedAt:    now,
			UpdatedAt:    now,
			StartedAt:    &started,
			CompletedAt:  &completed,
		},
		{
			TaskID:       taskID,
			Path:         "/tmp/source/c.txt",
			RelativePath: "c.txt",
			Size:         3,
			Status:       taskpkg.ItemStatusFailed,
			Error:        "second failure",
			AttemptCount: 3,
			CreatedAt:    now,
			UpdatedAt:    now,
			StartedAt:    &started,
			CompletedAt:  &completed,
		},
	}
	if err := repo.Create(taskRecord, items); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	stdout, stderr, err := executeCommand(NewRootCmd(), "task", "status", taskID, "--failed-limit", "1", "--config", configPath)
	if err != nil {
		t.Fatalf("executeCommand() error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "id: "+taskID) {
		t.Fatalf("stdout = %q, want task id", stdout)
	}
	if !strings.Contains(stdout, "items_failed: 2") {
		t.Fatalf("stdout = %q, want failed item count", stdout)
	}
	if !strings.Contains(stdout, "failed_item[1]:") {
		t.Fatalf("stdout = %q, want first failed item details", stdout)
	}
	if strings.Contains(stdout, "failed_item[2]:") {
		t.Fatalf("stdout = %q, want only one failed item detail", stdout)
	}
	if !strings.Contains(stdout, "failed_items_remaining: 1") {
		t.Fatalf("stdout = %q, want remaining failed items count", stdout)
	}
}

func TestTaskEventsFiltersPersistedLog(t *testing.T) {
	configPath, stateDir, _ := writeTestConfig(t)
	logPath := filepath.Join(stateDir, "task-events.jsonl")
	recorder, err := internallogging.NewFileAuditRecorder(logPath)
	if err != nil {
		t.Fatalf("NewFileAuditRecorder() error = %v", err)
	}
	t.Cleanup(func() {
		if err := recorder.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})

	recordEvents(t, recorder,
		internallogging.AuditEvent{Time: time.Now().UTC(), Event: "task_event", TaskID: "task-1", TaskStatus: "running", ItemPath: "a.txt", ItemStatus: "uploading", Attempt: 1, Bytes: 10, Message: "upload started", Error: ""},
		internallogging.AuditEvent{Time: time.Now().UTC(), Event: "task_event", TaskID: "task-2", TaskStatus: "running", ItemPath: "b.txt", ItemStatus: "uploading", Attempt: 1, Bytes: 20, Message: "upload started", Error: ""},
		internallogging.AuditEvent{Time: time.Now().UTC(), Event: "daemon_started", DaemonPID: 123, DaemonState: "running", Message: "daemon loop started"},
	)

	stdout, stderr, err := executeCommand(NewRootCmd(), "task", "events", "--config", configPath, "--task-id", "task-1", "--match", "upload")
	if err != nil {
		t.Fatalf("executeCommand() error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "task=task-1") {
		t.Fatalf("stdout = %q, want filtered task event", stdout)
	}
	if !strings.Contains(stdout, "message=upload started") {
		t.Fatalf("stdout = %q, want matching message", stdout)
	}
	if strings.Contains(stdout, "task=task-2") {
		t.Fatalf("stdout = %q, want other task filtered out", stdout)
	}
	if strings.Contains(stdout, "daemon loop started") {
		t.Fatalf("stdout = %q, want non-task events filtered out", stdout)
	}
}

func TestTaskEventsWithoutLogPrintsNoMatchingEvents(t *testing.T) {
	configPath, _, _ := writeTestConfig(t)

	stdout, stderr, err := executeCommand(NewRootCmd(), "task", "events", "--config", configPath)
	if err != nil {
		t.Fatalf("executeCommand() error = %v", err)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, "no matching task events") {
		t.Fatalf("stdout = %q, want empty-log message", stdout)
	}
}

func TestTaskEventsReturnsBootstrapError(t *testing.T) {
	originalBootstrap := newBootstrapWithConfig
	newBootstrapWithConfig = func(string) (*app.Bootstrap, error) {
		return nil, fmt.Errorf("boom")
	}
	t.Cleanup(func() {
		newBootstrapWithConfig = originalBootstrap
	})

	_, _, err := executeCommand(NewRootCmd(), "task", "events", "--config", "ignored")
	if err == nil {
		t.Fatal("executeCommand() error = nil, want bootstrap error")
	}
	if !strings.Contains(err.Error(), "create bootstrap: boom") {
		t.Fatalf("executeCommand() error = %v, want wrapped bootstrap error", err)
	}
}

func TestTaskListReturnsBootstrapError(t *testing.T) {
	originalBootstrap := newBootstrapWithConfig
	newBootstrapWithConfig = func(string) (*app.Bootstrap, error) {
		return nil, fmt.Errorf("boom")
	}
	t.Cleanup(func() {
		newBootstrapWithConfig = originalBootstrap
	})

	_, _, err := executeCommand(NewRootCmd(), "task", "list", "--config", "ignored")
	if err == nil {
		t.Fatal("executeCommand() error = nil, want bootstrap error")
	}
	if !strings.Contains(err.Error(), "create bootstrap: boom") {
		t.Fatalf("executeCommand() error = %v, want wrapped bootstrap error", err)
	}
}

func TestTaskStatusReturnsBootstrapError(t *testing.T) {
	originalBootstrap := newBootstrapWithConfig
	newBootstrapWithConfig = func(string) (*app.Bootstrap, error) {
		return nil, fmt.Errorf("boom")
	}
	t.Cleanup(func() {
		newBootstrapWithConfig = originalBootstrap
	})

	_, _, err := executeCommand(NewRootCmd(), "task", "status", "task-1", "--config", "ignored")
	if err == nil {
		t.Fatal("executeCommand() error = nil, want bootstrap error")
	}
	if !strings.Contains(err.Error(), "create bootstrap: boom") {
		t.Fatalf("executeCommand() error = %v, want wrapped bootstrap error", err)
	}
}

func TestTaskRetryReturnsBootstrapError(t *testing.T) {
	originalBootstrap := newBootstrapWithConfig
	newBootstrapWithConfig = func(string) (*app.Bootstrap, error) {
		return nil, fmt.Errorf("boom")
	}
	t.Cleanup(func() {
		newBootstrapWithConfig = originalBootstrap
	})

	_, _, err := executeCommand(NewRootCmd(), "task", "retry", "task-1", "--config", "ignored")
	if err == nil {
		t.Fatal("executeCommand() error = nil, want bootstrap error")
	}
	if !strings.Contains(err.Error(), "create bootstrap: boom") {
		t.Fatalf("executeCommand() error = %v, want wrapped bootstrap error", err)
	}
}

func writeTestConfig(t *testing.T) (string, string, string) {
	t.Helper()
	baseDir := t.TempDir()
	stateDir := filepath.Join(baseDir, "state")
	dbPath := filepath.Join(baseDir, "state", "tasks.db")
	configPath := filepath.Join(baseDir, "config.yaml")
	content := fmt.Sprintf("s3:\n  bucket: test-bucket\nworkers: 1\ndatabase_path: %q\nstate_dir: %q\nretry:\n  max_attempts: 1\n  backoff_ms: 0\nsecurity:\n  dry_run: true\n", dbPath, stateDir)
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}
	return configPath, stateDir, dbPath
}

func recordEvents(t *testing.T, recorder *internallogging.FileAuditRecorder, events ...internallogging.AuditEvent) {
	t.Helper()
	for _, event := range events {
		if err := recorder.Record(event); err != nil {
			t.Fatalf("Record() error = %v", err)
		}
	}
}
