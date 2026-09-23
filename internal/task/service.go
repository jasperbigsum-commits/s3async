package task

import (
	"crypto/rand"
	"fmt"
	"path/filepath"
	"strings"
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

type taskClaimer interface {
	ClaimTask(id string) (Task, bool, error)
}

type Uploader interface {
	UploadFile(bucket string, key string, localPath string) error
}

type Downloader interface {
	DownloadFile(bucket string, key string, root string, relativePath string, size int64) error
}

type ExecutionConfig struct {
	Workers     int
	MaxAttempts int
	Backoff     time.Duration
	// PathStyle must match the style used when the task was planned: faithful
	// trees decode local names back to true S3 keys on upload.
	PathStyle PathStyle
}

type Service struct {
	repo     Repository
	recorder EventRecorder

	// running prevents two execution entry points in the same process from
	// transferring the same task at the same time. The repository claim is the
	// cross-process guard; this is the fast in-process half for repositories
	// that do not implement taskClaimer (and avoids duplicate work before a
	// second database round trip).
	runningMu    sync.Mutex
	runningTasks map[string]struct{}
}

func NewService(repo Repository, recorder EventRecorder) *Service {
	return &Service{
		repo:         repo,
		recorder:     recorder,
		runningTasks: make(map[string]struct{}),
	}
}

func (s *Service) acquireTaskExecution(id string) bool {
	s.runningMu.Lock()
	defer s.runningMu.Unlock()
	if s.runningTasks == nil {
		s.runningTasks = make(map[string]struct{})
	}
	if _, exists := s.runningTasks[id]; exists {
		return false
	}
	s.runningTasks[id] = struct{}{}
	return true
}

func (s *Service) releaseTaskExecution(id string) {
	s.runningMu.Lock()
	delete(s.runningTasks, id)
	s.runningMu.Unlock()
}

func (s *Service) emitEvent(eventType string, t Task, item Item, items []Item, cfg ExecutionConfig, message string, errMsg string) {
	s.emitEventWithSummary(eventType, t, item, BuildSummary(items), cfg, message, errMsg)
}

// emitEventWithSummary is the hot-path variant: the caller passes the
// already-maintained summary so per-item events stay O(1) instead of
// rescanning the whole item list on every status change.
func (s *Service) emitEventWithSummary(eventType string, t Task, item Item, summary Summary, cfg ExecutionConfig, message string, errMsg string) {
	if s.recorder == nil {
		return
	}
	event := NewTaskEvent(eventType, t, item, cfg, summary, message, errMsg)
	_ = s.recorder.Record(event)
}

func (s *Service) CreateTask(source string, bucket string, prefix string, async bool, items []Item) (Task, error) {
	return s.createTask(source, bucket, prefix, "update", async, items)
}

func (s *Service) CreateDownloadTask(destination, bucket, prefix string, async bool, items []Item) (Task, error) {
	return s.createTask(destination, bucket, prefix, "download", async, items)
}

func (s *Service) createTask(source, bucket, prefix, mode string, async bool, items []Item) (Task, error) {
	status := StatusQueued
	if !async {
		status = StatusPending
	}

	now := time.Now().UTC()
	id, err := newTaskID(now)
	if err != nil {
		return Task{}, err
	}
	t := Task{
		ID:        id,
		Source:    source,
		Bucket:    bucket,
		Prefix:    prefix,
		Mode:      mode,
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

	s.emitEvent("task_created", t, Item{}, items, ExecutionConfig{}, "task persisted", "")
	return t, nil
}

// Wall-clock resolution varies by platform; time alone is not a unique ID.
// Randomness also distinguishes tasks created simultaneously in other processes.
func newTaskID(now time.Time) (string, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", fmt.Errorf("generate task ID: %w", err)
	}
	return fmt.Sprintf("task_%d_%x", now.UnixNano(), entropy), nil
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

	s.emitEvent("task_retried", t, Item{}, items, ExecutionConfig{}, "task moved back to queue", "")
	return nil
}

func (s *Service) ExecuteTask(id string, uploader Uploader, cfg ExecutionConfig) error {
	if !s.acquireTaskExecution(id) {
		return fmt.Errorf("task %s is already running", id)
	}
	defer s.releaseTaskExecution(id)

	if claimer, ok := s.repo.(taskClaimer); ok {
		claimed, claimedOK, claimErr := claimer.ClaimTask(id)
		if claimErr != nil {
			return fmt.Errorf("claim task from repo: %w", claimErr)
		}
		if !claimedOK {
			return fmt.Errorf("task %s is already running or unavailable", id)
		}
		return s.executeLoadedTask(claimed, uploader, cfg, true)
	}
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
	if !s.acquireTaskExecution(claimedTask.ID) {
		return claimedTask, true, fmt.Errorf("task %s is already running", claimedTask.ID)
	}
	defer s.releaseTaskExecution(claimedTask.ID)

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
	activeStatus := ItemStatusUploading
	transfer := func(item Item, key string) error { return uploader.UploadFile(t.Bucket, key, item.Path) }
	if t.Mode == "download" {
		downloader, ok := uploader.(Downloader)
		if !ok {
			return fmt.Errorf("client does not support downloads")
		}
		activeStatus = ItemStatusDownloading
		transfer = func(item Item, key string) error {
			return downloader.DownloadFile(t.Bucket, key, t.Source, item.RelativePath, item.Size)
		}
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
		s.emitEvent("task_completed_empty", t, Item{}, nil, ExecutionConfig{}, "task completed with no items", "")
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
		s.emitEvent("task_started", t, Item{}, items, cfg, "task execution started", "")
	}

	var mu sync.Mutex
	var executionErrMu sync.Mutex
	var executionErr error
	// Keep the dispatcher bounded. AWS CLI's s3 transfer manager separates
	// the producer from a bounded executor; using a small queue here avoids
	// retaining a second copy of a very large task's item list and ensures the
	// number of active transfers is controlled solely by cfg.Workers.
	queueSize := cfg.Workers * 2
	if queueSize < 1 {
		queueSize = 1
	}
	workCh := make(chan Item, queueSize)
	var wg sync.WaitGroup

	setExecutionErr := func(err error) {
		if err == nil {
			return
		}
		executionErrMu.Lock()
		defer executionErrMu.Unlock()
		if executionErr == nil {
			executionErr = err
		}
	}

	getExecutionErr := func() error {
		executionErrMu.Lock()
		defer executionErrMu.Unlock()
		return executionErr
	}

	currentSummary := BuildSummary(items)
	t.LastError = latestError(items)
	lastPersistedAt := time.Now().UTC()
	itemIndex := make(map[string]int, len(items))
	for i := range items {
		if _, exists := itemIndex[items[i].RelativePath]; !exists {
			itemIndex[items[i].RelativePath] = i
		}
	}

	persistProgressLocked := func(force bool) error {
		now := time.Now().UTC()
		if !force && now.Sub(lastPersistedAt) < 500*time.Millisecond {
			return nil
		}
		t.UpdatedAt = now
		ApplySummary(&t, currentSummary)
		if err := s.repo.UpdateTask(t); err != nil {
			return fmt.Errorf("update task progress: %w", err)
		}
		lastPersistedAt = now
		return nil
	}

	updateLocalItem := func(relativePath string, status ItemStatus, errMsg string) error {
		mu.Lock()
		defer mu.Unlock()

		now := time.Now().UTC()
		idx, ok := itemIndex[relativePath]
		if ok {
			oldStatus := items[idx].Status
			currentSummary = MoveSummaryItem(currentSummary, oldStatus, status, items[idx].Size)
			items[idx].Status = status
			items[idx].Error = errMsg
			items[idx].UpdatedAt = now
			if errMsg != "" {
				t.LastError = errMsg
			}
			switch status {
			case ItemStatusUploading, ItemStatusDownloading:
				items[idx].AttemptCount++
				items[idx].StartedAt = &now
				items[idx].CompletedAt = nil
			case ItemStatusSuccess, ItemStatusFailed, ItemStatusSkipped:
				items[idx].CompletedAt = &now
			}
		}

		// Task-level counters are advisory progress: throttle every write and
		// let the flush after wg.Wait() persist the authoritative final state.
		if err := persistProgressLocked(false); err != nil {
			return err
		}
		if ok {
			s.emitEventWithSummary("item_status_changed", t, items[idx], currentSummary, cfg, fmt.Sprintf("item moved to %s", status), errMsg)
		}
		return nil
	}

	normalizedPrefix := normalizeObjectPrefix(t.Prefix)

	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range workCh {
				if getExecutionErr() != nil {
					continue
				}

				key := joinObjectKey(normalizedPrefix, item.RelativePath)
				if cfg.PathStyle == PathFaithful {
					key = joinObjectKey(normalizedPrefix, DecodeLocalRelative(item.RelativePath))
				}
				if t.Mode == "download" {
					// RelativePath is the exact suffix returned by S3, including leading slashes.
					key = item.RelativePath
					if normalizedPrefix != "" {
						key = normalizedPrefix + "/" + item.RelativePath
					}
				}
				var transferErr error
				for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
					if err := s.repo.UpdateItemStatus(t.ID, item.RelativePath, activeStatus, ""); err != nil {
						setExecutionErr(fmt.Errorf("mark item transferring %s: %w", item.RelativePath, err))
						break
					}
					if err := updateLocalItem(item.RelativePath, activeStatus, ""); err != nil {
						setExecutionErr(fmt.Errorf("persist local transfer progress %s: %w", item.RelativePath, err))
						break
					}

					transferErr = transfer(item, key)
					if transferErr == nil {
						if err := s.repo.UpdateItemStatus(t.ID, item.RelativePath, ItemStatusSuccess, ""); err != nil {
							setExecutionErr(fmt.Errorf("mark item success %s: %w", item.RelativePath, err))
							break
						}
						if err := updateLocalItem(item.RelativePath, ItemStatusSuccess, ""); err != nil {
							setExecutionErr(fmt.Errorf("persist local success progress %s: %w", item.RelativePath, err))
						}
						break
					}

					if attempt < cfg.MaxAttempts && cfg.Backoff > 0 {
						time.Sleep(cfg.Backoff)
					}
				}

				if getExecutionErr() != nil {
					continue
				}
				if transferErr != nil {
					if err := s.repo.UpdateItemStatus(t.ID, item.RelativePath, ItemStatusFailed, transferErr.Error()); err != nil {
						setExecutionErr(fmt.Errorf("mark item failed %s: %w", item.RelativePath, err))
						continue
					}
					if err := updateLocalItem(item.RelativePath, ItemStatusFailed, transferErr.Error()); err != nil {
						setExecutionErr(fmt.Errorf("persist local failed progress %s: %w", item.RelativePath, err))
					}
				}
			}
		}()
	}

	// Dispatch only after workers are live. This is the bounded-executor
	// pattern used by the AWS CLI: the producer applies backpressure instead
	// of enqueueing the entire task before any transfer can start.
	for _, item := range items {
		if item.Status == ItemStatusSuccess || item.Status == ItemStatusSkipped {
			continue
		}
		workCh <- item
	}
	close(workCh)
	wg.Wait()

	if err := getExecutionErr(); err != nil {
		finalItems, listErr := s.repo.ListItems(t.ID)
		if listErr == nil {
			now := time.Now().UTC()
			t.Status = StatusFailed
			t.LastError = err.Error()
			t.UpdatedAt = now
			t.CompletedAt = &now
			ApplySummary(&t, BuildSummary(finalItems))
			_ = s.repo.UpdateTask(t)
			s.emitEvent("task_execution_error", t, Item{}, finalItems, cfg, "task execution aborted by persistence error", err.Error())
		}
		return err
	}

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
	s.emitEvent("task_finished", t, Item{}, finalItems, cfg, fmt.Sprintf("task finished with status %s", finalStatus), t.LastError)

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

func normalizeObjectPrefix(prefix string) string {
	return strings.Trim(slashifyPath(prefix), "/")
}

func joinObjectKey(prefix string, relativePath string) string {
	normalizedPrefix := normalizeObjectPrefix(prefix)
	normalizedPath := strings.TrimLeft(slashifyPath(relativePath), "/")
	if normalizedPrefix == "" {
		return normalizedPath
	}
	if normalizedPath == "" {
		return normalizedPrefix
	}
	return normalizedPrefix + "/" + normalizedPath
}

func slashifyPath(value string) string {
	// filepath.ToSlash converts OS-specific separators, but it does not convert
	// backslashes on non-Windows platforms. Convert remaining backslashes to make
	// S3 object keys consistent.
	return strings.ReplaceAll(filepath.ToSlash(value), "\\", "/")
}
