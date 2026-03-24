package logging

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	taskpkg "github.com/jasperbigsum-commits/s3async/internal/task"
)

type AuditEvent struct {
	Time        time.Time        `json:"time"`
	Event       string           `json:"event"`
	TaskID      string           `json:"task_id,omitempty"`
	ItemPath    string           `json:"item_path,omitempty"`
	TaskStatus  string           `json:"task_status,omitempty"`
	ItemStatus  string           `json:"item_status,omitempty"`
	Attempt     int              `json:"attempt,omitempty"`
	Message     string           `json:"message,omitempty"`
	Error       string           `json:"error,omitempty"`
	WorkerCount int              `json:"worker_count,omitempty"`
	MaxAttempts int              `json:"max_attempts,omitempty"`
	Bytes       int64            `json:"bytes,omitempty"`
	Bucket      string           `json:"bucket,omitempty"`
	Prefix      string           `json:"prefix,omitempty"`
	Source      string           `json:"source,omitempty"`
	DaemonPID   int              `json:"daemon_pid,omitempty"`
	DaemonState string           `json:"daemon_state,omitempty"`
	Summary     *taskpkg.Summary `json:"summary,omitempty"`
}

type AuditRecorder interface {
	Record(AuditEvent) error
}

type FileAuditRecorder struct {
	mu   sync.Mutex
	file *os.File
}

func NewFileAuditRecorder(path string) (*FileAuditRecorder, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create audit log directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open audit log file: %w", err)
	}
	return &FileAuditRecorder{file: f}, nil
}

func (r *FileAuditRecorder) Record(event AuditEvent) error {
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal audit event: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.file.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("write audit event: %w", err)
	}
	return nil
}

func (r *FileAuditRecorder) RecordTaskEvent(event taskpkg.TaskEvent) error {
	return r.Record(AuditEvent{
		Time:        event.Time,
		Event:       "task_event",
		TaskID:      event.TaskID,
		ItemPath:    event.ItemPath,
		TaskStatus:  string(event.TaskStatus),
		ItemStatus:  string(event.ItemStatus),
		Attempt:     event.Attempt,
		Message:     event.Message,
		Error:       event.Error,
		WorkerCount: event.Workers,
		MaxAttempts: event.MaxAttempts,
		Bytes:       event.Size,
		Bucket:      event.Bucket,
		Prefix:      event.Prefix,
		Source:      event.Source,
		Summary:     &event.Summary,
	})
}

func (r *FileAuditRecorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	return err
}
