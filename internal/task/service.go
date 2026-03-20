package task

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

type Repository interface {
	Create(task Task, items []Item) error
	List() ([]Task, error)
	Get(id string) (Task, error)
	ListItems(taskID string) ([]Item, error)
	UpdateStatus(id string, status Status) error
	UpdateTask(task Task) error
	UpdateItemStatus(taskID string, relativePath string, status ItemStatus, errMsg string) error
	ResetItemsForRetry(taskID string) error
	ClaimNextQueued() (Task, bool, error)
}

type Uploader interface {
	UploadFile(bucket string, key string, localPath string) error
}

type ExecutionConfig struct {
	Workers     int
	MaxAttempts int
	Backoff     time.Duration
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateTask(source string, bucket string, prefix string, async bool, items []Item) (Task, error) {
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

	for i := range items {
		items[i].TaskID = t.ID
		items[i].CreatedAt = now
		items[i].UpdatedAt = now
		if items[i].Status == "" {
			items[i].Status = ItemStatusPending
		}
	}
	ApplySummary(&t, BuildSummary(items))

	if err := s.repo.Create(t, items); err != nil {
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

func (s *Service) ListTaskItems(id string) ([]Item, error) {
	items, err := s.repo.ListItems(id)
	if err != nil {
		return nil, fmt.Errorf("list task items from repo: %w", err)
	}

	return items, nil
}

func (s *Service) CompleteTaskIfEmpty(id string) error {
	t, err := s.repo.Get(id)
	if err != nil {
		return fmt.Errorf("get task: %w", err)
	}
	if t.TotalItems != 0 {
		return nil
	}

	now := time.Now().UTC()
	t.Status = StatusCompleted
	t.CompletedAt = &now
	t.UpdatedAt = now
	if err := s.repo.UpdateTask(t); err != nil {
		return fmt.Errorf("complete empty task: %w", err)
	}
	return nil
}

func (s *Service) RetryTask(id string) error {
	if err := s.repo.ResetItemsForRetry(id); err != nil {
		return fmt.Errorf("reset task items for retry: %w", err)
	}

	t, err := s.repo.Get(id)
	if err != nil {
		return fmt.Errorf("get task after retry reset: %w", err)
	}
	items, err := s.repo.ListItems(id)
	if err != nil {
		return fmt.Errorf("list task items after retry reset: %w", err)
	}

	now := time.Now().UTC()
	t.Status = StatusQueued
	t.LastError = ""
	t.StartedAt = nil
	t.CompletedAt = nil
	t.UpdatedAt = now
	ApplySummary(&t, BuildSummary(items))
	if err := s.repo.UpdateTask(t); err != nil {
		return fmt.Errorf("update task after retry: %w", err)
	}

	return nil
}

func (s *Service) ExecuteTask(id string, uploader Uploader, cfg ExecutionConfig) error {
	t, err := s.repo.Get(id)
	if err != nil {
		return fmt.Errorf("get task from repo: %w", err)
	}

	return s.executeLoadedTask(t, uploader, cfg, false)
}

func (s *Service) ExecuteNextQueuedTask(uploader Uploader, cfg ExecutionConfig) (Task, bool, error) {
	claimedTask, ok, err := s.repo.ClaimNextQueued()
	if err != nil {
		return Task{}, false, fmt.Errorf("claim next queued task: %w", err)
	}
	if !ok {
		return Task{}, false, nil
	}

	if err := s.executeLoadedTask(claimedTask, uploader, cfg, true); err != nil {
		return claimedTask, true, err
	}
	updatedTask, err := s.repo.Get(claimedTask.ID)
	if err != nil {
		return claimedTask, true, fmt.Errorf("reload claimed task: %w", err)
	}
	return updatedTask, true, nil
}

func (s *Service) executeLoadedTask(t Task, uploader Uploader, cfg ExecutionConfig, alreadyRunning bool) error {
	if uploader == nil {
		return fmt.Errorf("uploader is required")
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}
	if cfg.Backoff < 0 {
		cfg.Backoff = 0
	}

	items, err := s.repo.ListItems(t.ID)
	if err != nil {
		return fmt.Errorf("list task items from repo: %w", err)
	}

	if len(items) == 0 {
		now := time.Now().UTC()
		t.Status = StatusCompleted
		t.LastError = ""
		t.UpdatedAt = now
		if t.StartedAt == nil {
			t.StartedAt = &now
		}
		t.CompletedAt = &now
		ApplySummary(&t, BuildSummary(items))
		if err := s.repo.UpdateTask(t); err != nil {
			return fmt.Errorf("complete empty task: %w", err)
		}
		return nil
	}

	if !alreadyRunning {
		now := time.Now().UTC()
		t.Status = StatusRunning
		t.LastError = ""
		t.UpdatedAt = now
		if t.StartedAt == nil {
			t.StartedAt = &now
		}
		t.CompletedAt = nil
		ApplySummary(&t, BuildSummary(items))
		if err := s.repo.UpdateTask(t); err != nil {
			return fmt.Errorf("mark task running: %w", err)
		}
	}

	var mu sync.Mutex
	workCh := make(chan Item)
	var wg sync.WaitGroup

	persistProgress := func() {
		t.UpdatedAt = time.Now().UTC()
		ApplySummary(&t, BuildSummary(items))
		t.LastError = latestError(items)
		_ = s.repo.UpdateTask(t)
	}

	updateLocalItem := func(relativePath string, status ItemStatus, errMsg string) {
		mu.Lock()
		defer mu.Unlock()

		now := time.Now().UTC()
		for i := range items {
			if items[i].RelativePath != relativePath {
				continue
			}
			items[i].Status = status
			items[i].Error = errMsg
			items[i].UpdatedAt = now
			switch status {
			case ItemStatusUploading:
				items[i].AttemptCount++
				items[i].StartedAt = &now
				items[i].CompletedAt = nil
			case ItemStatusSuccess, ItemStatusFailed, ItemStatusSkipped:
				items[i].CompletedAt = &now
			}
			break
		}
		persistProgress()
	}

	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range workCh {
				var uploadErr error
				for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
					_ = s.repo.UpdateItemStatus(t.ID, item.RelativePath, ItemStatusUploading, "")
					updateLocalItem(item.RelativePath, ItemStatusUploading, "")

					key := item.RelativePath
					if t.Prefix != "" {
						key = filepath.ToSlash(filepath.Join(t.Prefix, item.RelativePath))
					}

					uploadErr = uploader.UploadFile(t.Bucket, key, item.Path)
					if uploadErr == nil {
						_ = s.repo.UpdateItemStatus(t.ID, item.RelativePath, ItemStatusSuccess, "")
						updateLocalItem(item.RelativePath, ItemStatusSuccess, "")
						break
					}

					if attempt < cfg.MaxAttempts && cfg.Backoff > 0 {
						time.Sleep(cfg.Backoff)
					}
				}

				if uploadErr != nil {
					_ = s.repo.UpdateItemStatus(t.ID, item.RelativePath, ItemStatusFailed, uploadErr.Error())
					updateLocalItem(item.RelativePath, ItemStatusFailed, uploadErr.Error())
				}
			}
		}()
	}

	for _, item := range items {
		if item.Status == ItemStatusSuccess || item.Status == ItemStatusSkipped {
			continue
		}
		workCh <- item
	}
	close(workCh)
	wg.Wait()

	finalItems, err := s.repo.ListItems(t.ID)
	if err != nil {
		return fmt.Errorf("list final task items from repo: %w", err)
	}

	finalStatus := summarizeTaskStatus(finalItems)
	now := time.Now().UTC()
	t.Status = finalStatus
	t.LastError = latestError(finalItems)
	t.UpdatedAt = now
	t.CompletedAt = &now
	ApplySummary(&t, BuildSummary(finalItems))
	if err := s.repo.UpdateTask(t); err != nil {
		return fmt.Errorf("update final task state: %w", err)
	}

	return nil
}

func summarizeTaskStatus(items []Item) Status {
	if len(items) == 0 {
		return StatusCompleted
	}

	var successCount int
	var failedCount int
	for _, item := range items {
		switch item.Status {
		case ItemStatusSuccess, ItemStatusSkipped:
			successCount++
		case ItemStatusFailed:
			failedCount++
		}
	}

	switch {
	case failedCount == 0:
		return StatusCompleted
	case successCount == 0:
		return StatusFailed
	default:
		return StatusPartialFailed
	}
}

func latestError(items []Item) string {
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Error != "" {
			return items[i].Error
		}
	}
	return ""
}
