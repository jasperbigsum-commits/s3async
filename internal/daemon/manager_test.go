package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerAcquireWriteStatusAndStopRequest(t *testing.T) {
	manager := NewManager(filepath.Join(t.TempDir(), "state"))
	pid, release, err := manager.Acquire()
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("Acquire() pid = %d, want %d", pid, os.Getpid())
	}
	defer func() {
		if err := release(); err != nil {
			t.Fatalf("release() error = %v", err)
		}
	}()

	running, gotPID, err := manager.IsRunning()
	if err != nil {
		t.Fatalf("IsRunning() error = %v", err)
	}
	if !running || gotPID != pid {
		t.Fatalf("IsRunning() = (%v, %d), want (true, %d)", running, gotPID, pid)
	}

	now := time.Now().UTC().Truncate(time.Second)
	status := Status{
		PID:            pid,
		State:          "running",
		StartedAt:      now,
		HeartbeatAt:    now,
		CurrentTaskID:  "task_1",
		TasksExecuted:  1,
		QueuePolls:     2,
		PollInterval:   "2s",
		IdleTimeout:    "30s",
		AuditLogPath:   manager.AuditLogPath(),
		LastTaskID:     "task_0",
		LastTaskStatus: "completed",
	}
	if err := manager.WriteStatus(status); err != nil {
		t.Fatalf("WriteStatus() error = %v", err)
	}

	readStatus, err := manager.ReadStatus()
	if err != nil {
		t.Fatalf("ReadStatus() error = %v", err)
	}
	if readStatus.CurrentTaskID != "task_1" || readStatus.TasksExecuted != 1 {
		t.Fatalf("ReadStatus() = %+v, want current_task_id=task_1 tasks_executed=1", readStatus)
	}

	if manager.StopRequested() {
		t.Fatal("StopRequested() = true before RequestStop(), want false")
	}
	if err := manager.RequestStop(); err != nil {
		t.Fatalf("RequestStop() error = %v", err)
	}
	if !manager.StopRequested() {
		t.Fatal("StopRequested() = false after RequestStop(), want true")
	}
	if err := manager.ClearStopRequest(); err != nil {
		t.Fatalf("ClearStopRequest() error = %v", err)
	}
	if manager.StopRequested() {
		t.Fatal("StopRequested() = true after ClearStopRequest(), want false")
	}
}

func TestManagerAcquireRejectsExistingLivePID(t *testing.T) {
	manager := NewManager(t.TempDir())
	if err := manager.EnsureStateDir(); err != nil {
		t.Fatalf("EnsureStateDir() error = %v", err)
	}
	if err := os.WriteFile(manager.PIDFile(), []byte("1\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, _, err := manager.Acquire()
	if err == nil {
		t.Fatal("Acquire() error = nil, want error")
	}
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("Acquire() error = %v, want ErrAlreadyRunning", err)
	}
}
