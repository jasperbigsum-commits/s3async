package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	pidFileName    = "daemon.pid"
	statusFileName = "daemon-status.json"
	stopFileName   = "daemon.stop"
	auditFileName  = "audit.jsonl"
)

var ErrAlreadyRunning = errors.New("daemon already running")

type Manager struct {
	stateDir string
}

type Status struct {
	PID             int       `json:"pid"`
	State           string    `json:"state"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	HeartbeatAt     time.Time `json:"heartbeat_at,omitempty"`
	StoppedAt       time.Time `json:"stopped_at,omitempty"`
	PollInterval    string    `json:"poll_interval,omitempty"`
	CurrentTaskID   string    `json:"current_task_id,omitempty"`
	LastTaskID      string    `json:"last_task_id,omitempty"`
	LastTaskStatus  string    `json:"last_task_status,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
	TasksExecuted   int       `json:"tasks_executed,omitempty"`
	QueuePolls      int       `json:"queue_polls,omitempty"`
	IdleTimeout     string    `json:"idle_timeout,omitempty"`
	AuditLogPath    string    `json:"audit_log_path,omitempty"`
	StateDir        string    `json:"state_dir,omitempty"`
}

func NewManager(stateDir string) *Manager {
	return &Manager{stateDir: stateDir}
}

func (m *Manager) StateDir() string {
	return m.stateDir
}

func (m *Manager) PIDFile() string {
	return filepath.Join(m.stateDir, pidFileName)
}

func (m *Manager) StatusFile() string {
	return filepath.Join(m.stateDir, statusFileName)
}

func (m *Manager) StopFile() string {
	return filepath.Join(m.stateDir, stopFileName)
}

func (m *Manager) AuditLogPath() string {
	return filepath.Join(m.stateDir, auditFileName)
}

func (m *Manager) EnsureStateDir() error {
	if m.stateDir == "" {
		return fmt.Errorf("state directory is required")
	}
	if err := os.MkdirAll(m.stateDir, 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	return nil
}

func (m *Manager) Acquire() (int, func() error, error) {
	if err := m.EnsureStateDir(); err != nil {
		return 0, nil, err
	}

	pidPath := m.PIDFile()
	currentPID := os.Getpid()
	if err := writePIDFileExclusive(pidPath, currentPID); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return 0, nil, fmt.Errorf("create pid file: %w", err)
		}

		existingPID, readErr := m.ReadPID()
		if readErr == nil && existingPID > 0 && processAlive(existingPID) {
			return 0, nil, fmt.Errorf("%w: pid=%d", ErrAlreadyRunning, existingPID)
		}

		if removeErr := os.Remove(pidPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return 0, nil, fmt.Errorf("remove stale pid file: %w", removeErr)
		}
		if err := writePIDFileExclusive(pidPath, currentPID); err != nil {
			return 0, nil, fmt.Errorf("recreate pid file: %w", err)
		}
	}

	release := func() error {
		if err := os.Remove(pidPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove pid file: %w", err)
		}
		return nil
	}
	return currentPID, release, nil
}

func writePIDFileExclusive(path string, pid int) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "%d\n", pid); err != nil {
		return err
	}
	return nil
}

func (m *Manager) ReadPID() (int, error) {
	content, err := os.ReadFile(m.PIDFile())
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		return 0, fmt.Errorf("parse pid file: %w", err)
	}
	return pid, nil
}

func (m *Manager) IsRunning() (bool, int, error) {
	pid, err := m.ReadPID()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, 0, nil
		}
		return false, 0, err
	}
	if pid <= 0 {
		return false, 0, nil
	}
	return processAlive(pid), pid, nil
}

func (m *Manager) WriteStatus(status Status) error {
	if err := m.EnsureStateDir(); err != nil {
		return err
	}
	status.StateDir = m.stateDir
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal daemon status: %w", err)
	}
	if err := os.WriteFile(m.StatusFile(), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write daemon status: %w", err)
	}
	return nil
}

func (m *Manager) ReadStatus() (Status, error) {
	data, err := os.ReadFile(m.StatusFile())
	if err != nil {
		return Status{}, err
	}
	var status Status
	if err := json.Unmarshal(data, &status); err != nil {
		return Status{}, fmt.Errorf("decode daemon status: %w", err)
	}
	if status.StateDir == "" {
		status.StateDir = m.stateDir
	}
	if status.AuditLogPath == "" {
		status.AuditLogPath = m.AuditLogPath()
	}
	return status, nil
}

func (m *Manager) RequestStop() error {
	if err := m.EnsureStateDir(); err != nil {
		return err
	}
	content := []byte(time.Now().UTC().Format(time.RFC3339Nano) + "\n")
	if err := os.WriteFile(m.StopFile(), content, 0o644); err != nil {
		return fmt.Errorf("write stop file: %w", err)
	}
	return nil
}

func (m *Manager) StopRequested() bool {
	_, err := os.Stat(m.StopFile())
	return err == nil
}

func (m *Manager) ClearStopRequest() error {
	if err := os.Remove(m.StopFile()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stop file: %w", err)
	}
	return nil
}
