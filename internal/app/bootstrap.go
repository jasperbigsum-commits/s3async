package app

import (
	"errors"
	"fmt"
	"path/filepath"

	cfgpkg "github.com/jasperbigsum-commits/s3async/internal/config"
	internallogging "github.com/jasperbigsum-commits/s3async/internal/logging"
	"github.com/jasperbigsum-commits/s3async/internal/store"
	"github.com/jasperbigsum-commits/s3async/internal/task"
)

type Bootstrap struct {
	Config      cfgpkg.Config
	TaskService *task.Service
	repo        *store.SQLiteTaskRepository
	recorder    *internallogging.FileAuditRecorder
}

func NewBootstrap() (*Bootstrap, error) {
	return NewBootstrapWithConfig("")
}

func NewBootstrapWithConfig(configPath string) (*Bootstrap, error) {
	cfg, err := cfgpkg.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	repo, err := store.NewSQLiteTaskRepository(cfg.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("create sqlite repository: %w", err)
	}

	auditRecorder, err := internallogging.NewFileAuditRecorder(filepath.Join(cfg.StateDir, "task-events.jsonl"))
	if err != nil {
		_ = repo.Close()
		return nil, fmt.Errorf("create task event recorder: %w", err)
	}

	return &Bootstrap{
		Config: cfg, TaskService: task.NewService(repo, taskEventRecorderAdapter{recorder: auditRecorder}),
		repo: repo, recorder: auditRecorder,
	}, nil
}

// Close releases owned resources after all task execution has finished.
func (b *Bootstrap) Close() error {
	var recorderErr, repoErr error
	if b.recorder != nil {
		recorderErr = b.recorder.Close()
	}
	if b.repo != nil {
		repoErr = b.repo.Close()
	}
	return errors.Join(recorderErr, repoErr)
}
