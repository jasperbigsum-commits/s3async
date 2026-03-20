package task

import (
	"fmt"
	"time"
)

type Repository interface {
	Create(task Task) error
	List() ([]Task, error)
	Get(id string) (Task, error)
	UpdateStatus(id string, status Status) error
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateTask(source string, bucket string, prefix string, async bool) (Task, error) {
	status := StatusQueued
	if !async {
		status = StatusPending
	}

	now := time.Now().UTC()
	t := Task{
		ID:        fmt.Sprintf("task_%d", now.UnixNano()),
		Source:    source,
		Bucket:    bucket,
		Prefix:    prefix,
		Mode:      "update",
		Status:    status,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.repo.Create(t); err != nil {
		return Task{}, fmt.Errorf("persist task: %w", err)
	}

	return t, nil
}

func (s *Service) ListTasks() ([]Task, error) {
	tasks, err := s.repo.List()
	if err != nil {
		return nil, fmt.Errorf("list tasks from repo: %w", err)
	}

	return tasks, nil
}

func (s *Service) GetTask(id string) (Task, error) {
	t, err := s.repo.Get(id)
	if err != nil {
		return Task{}, fmt.Errorf("get task from repo: %w", err)
	}

	return t, nil
}

func (s *Service) RetryTask(id string) error {
	if err := s.repo.UpdateStatus(id, StatusQueued); err != nil {
		return fmt.Errorf("update task status: %w", err)
	}

	return nil
}
