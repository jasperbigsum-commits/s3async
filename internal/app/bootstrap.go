package app

import (
	"fmt"

	cfgpkg "github.com/jasperbigsum-commits/s3async/internal/config"
	"github.com/jasperbigsum-commits/s3async/internal/store"
	"github.com/jasperbigsum-commits/s3async/internal/task"
)

type Bootstrap struct {
	Config      cfgpkg.Config
	TaskService *task.Service
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

	return &Bootstrap{Config: cfg, TaskService: task.NewService(repo)}, nil
}
