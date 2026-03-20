package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jasperbigsum-commits/s3async/internal/store"
	"github.com/jasperbigsum-commits/s3async/internal/task"
)

type Bootstrap struct {
	TaskService *task.Service
}

func NewBootstrap() (*Bootstrap, error) {
	databasePath := os.Getenv("S3ASYNC_DB_PATH")
	if databasePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve user home dir: %w", err)
		}
		databasePath = filepath.Join(home, ".s3async", "tasks.db")
	}

	repo, err := store.NewSQLiteTaskRepository(databasePath)
	if err != nil {
		return nil, fmt.Errorf("create sqlite repository: %w", err)
	}

	return &Bootstrap{TaskService: task.NewService(repo)}, nil
}
