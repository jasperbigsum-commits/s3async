package task

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryRepo struct {
	mu                 sync.Mutex
	items              map[string]Task
	itemIndex          map[string][]Item
	failUpdateTask     bool
	failUpdateItemPath string
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{items: map[string]Task{}, itemIndex: map[string][]Item{}}
}

func (m *memoryRepo) Create(task Task, items []Item) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[task.ID] = task
	m.itemIndex[task.ID] = append([]Item(nil), items...)
	return nil
}

func (m *memoryRepo) List() ([]Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]Task, 0, len(m.items))
	for _, item := range m.items {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

func (m *memoryRepo) Get(id string) (Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.items[id]
	if !ok {
		return Task{}, fmt.Errorf("task not found: %s", id)
	}
	return item, nil
}

func (m *memoryRepo) ListItems(taskID string) ([]Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Item(nil), m.itemIndex[taskID]...), nil
}

func (m *memoryRepo) UpdateStatus(id string, status Status) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.items[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	item.Status = status
	item.UpdatedAt = time.Now().UTC()
	m.items[id] = item
	return nil
}

func (m *memoryRepo) UpdateTask(task Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failUpdateTask {
		return fmt.Errorf("forced UpdateTask failure")
	}
	if _, ok := m.items[task.ID]; !ok {
		return fmt.Errorf("task not found: %s", task.ID)
	}
	m.items[task.ID] = task
	return nil
}

func (m *memoryRepo) UpdateItemStatus(taskID string, relativePath string, status ItemStatus, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failUpdateItemPath != "" && m.failUpdateItemPath == relativePath {
		return fmt.Errorf("forced UpdateItemStatus failure for %s", relativePath)
	}
	items := m.itemIndex[taskID]
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
			if items[i].StartedAt == nil {
				items[i].StartedAt = &now
			}
			items[i].CompletedAt = nil
		case ItemStatusSuccess, ItemStatusFailed, ItemStatusSkipped:
			items[i].CompletedAt = &now
		}
		m.itemIndex[taskID] = items
		return nil
	}
	return fmt.Errorf("task item not found: %s/%s", taskID, relativePath)
}

func (m *memoryRepo) ResetItemsForRetry(taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := m.itemIndex[taskID]
	for i := range items {
		if items[i].Status == ItemStatusFailed {
			items[i].Status = ItemStatusPending
			items[i].Error = ""
			items[i].AttemptCount = 0
			items[i].StartedAt = nil
			items[i].CompletedAt = nil
		}
	}
	m.itemIndex[taskID] = items
	return nil
}

func (m *memoryRepo) ClaimNextQueued() (Task, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var best *Task
	for _, item := range m.items {
		candidate := item
		if candidate.Status != StatusQueued {
			continue
		}
		if best == nil || candidate.CreatedAt.Before(best.CreatedAt) {
			best = &candidate
		}
	}
	if best == nil {
		return Task{}, false, nil
	}
	now := time.Now().UTC()
	best.Status = StatusRunning
	best.UpdatedAt = now
	if best.StartedAt == nil {
		best.StartedAt = &now
	}
	best.CompletedAt = nil
	best.LastError = ""
	m.items[best.ID] = *best
	return *best, true, nil
}

type fakeUploader struct {
	mu       sync.Mutex
	failures map[string]int
	calls    []string
}

func (f *fakeUploader) UploadFile(bucket string, key string, localPath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, key)
	if remaining := f.failures[key]; remaining > 0 {
		f.failures[key] = remaining - 1
		return fmt.Errorf("forced upload failure for %s", key)
	}
	return nil
}

func TestCreateTaskStatus(t *testing.T) {
	tests := []struct {
		name       string
		async      bool
		wantStatus Status
	}{
		{name: "async uses queued", async: true, wantStatus: StatusQueued},
		{name: "foreground uses pending", async: false, wantStatus: StatusPending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMemoryRepo()
			service := NewService(repo, nil)

			createdTask, err := service.CreateTask("./data", "bucket", "prefix", tt.async, []Item{{Path: "a.txt", RelativePath: "a.txt", Size: 1}})
			if err != nil {
				t.Fatalf("CreateTask() error = %v", err)
			}

			if createdTask.Status != tt.wantStatus {
				t.Fatalf("CreateTask() status = %s, want %s", createdTask.Status, tt.wantStatus)
			}
		})
	}
}

func TestCreateTaskPersistsItems(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, nil)

	createdTask, err := service.CreateTask("./data", "bucket", "prefix", true, []Item{{Path: "a.txt", RelativePath: "a.txt", Size: 1}})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	items, err := service.ListTaskItems(createdTask.ID)
	if err != nil {
		t.Fatalf("ListTaskItems() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ListTaskItems() len = %d, want 1", len(items))
	}
	if items[0].TaskID != createdTask.ID {
		t.Fatalf("ListTaskItems() task_id = %s, want %s", items[0].TaskID, createdTask.ID)
	}
	if items[0].Status != ItemStatusPending {
		t.Fatalf("ListTaskItems() status = %s, want %s", items[0].Status, ItemStatusPending)
	}
	if createdTask.TotalItems != 1 || createdTask.PendingItems != 1 {
		t.Fatalf("CreateTask() summary = %+v, want total=1 pending=1", createdTask)
	}
}

func TestRetryTask(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, nil)
	createdTask, err := service.CreateTask("./data", "bucket", "prefix", false, []Item{{Path: "a.txt", RelativePath: "a.txt", Size: 1, Status: ItemStatusFailed, Error: "boom"}})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	now := time.Now().UTC()
	createdTask.Status = StatusFailed
	createdTask.LastError = "boom"
	createdTask.StartedAt = &now
	createdTask.CompletedAt = &now
	createdTask.PendingItems = 0
	createdTask.FailedItems = 1
	if err := repo.UpdateTask(createdTask); err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}
	if err := repo.UpdateItemStatus(createdTask.ID, "a.txt", ItemStatusFailed, "boom"); err != nil {
		t.Fatalf("UpdateItemStatus() error = %v", err)
	}

	if err := service.RetryTask(createdTask.ID); err != nil {
		t.Fatalf("RetryTask() error = %v", err)
	}

	got, err := service.GetTask(createdTask.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}

	if got.Status != StatusQueued {
		t.Fatalf("GetTask() status after retry = %s, want %s", got.Status, StatusQueued)
	}
	if got.LastError != "" {
		t.Fatalf("GetTask() last error after retry = %q, want empty", got.LastError)
	}
	if got.StartedAt != nil || got.CompletedAt != nil {
		t.Fatalf("GetTask() timestamps after retry = started:%v completed:%v, want nil", got.StartedAt, got.CompletedAt)
	}

	items, err := service.ListTaskItems(createdTask.ID)
	if err != nil {
		t.Fatalf("ListTaskItems() error = %v", err)
	}
	if items[0].Status != ItemStatusPending {
		t.Fatalf("item status after retry = %s, want %s", items[0].Status, ItemStatusPending)
	}
	if items[0].Error != "" {
		t.Fatalf("item error after retry = %q, want empty", items[0].Error)
	}
	if items[0].AttemptCount != 0 {
		t.Fatalf("item attempts after retry = %d, want 0", items[0].AttemptCount)
	}
}

func TestExecuteTaskSuccess(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, nil)
	createdTask, err := service.CreateTask("./data", "bucket", "backup", true, []Item{
		{Path: "a.txt", RelativePath: "a.txt", Size: 1},
		{Path: "b.txt", RelativePath: "sub/b.txt", Size: 2},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	uploader := &fakeUploader{failures: map[string]int{}}
	if err := service.ExecuteTask(createdTask.ID, uploader, ExecutionConfig{Workers: 2, MaxAttempts: 1}); err != nil {
		t.Fatalf("ExecuteTask() error = %v", err)
	}

	got, err := service.GetTask(createdTask.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.Status != StatusCompleted {
		t.Fatalf("task status = %s, want %s", got.Status, StatusCompleted)
	}
	if got.StartedAt == nil || got.CompletedAt == nil {
		t.Fatalf("task timestamps = started:%v completed:%v, want both set", got.StartedAt, got.CompletedAt)
	}
	if got.SuccessItems != 2 || got.FailedItems != 0 || got.PendingItems != 0 {
		t.Fatalf("task summary = %+v, want success=2 failed=0 pending=0", got)
	}

	items, err := service.ListTaskItems(createdTask.ID)
	if err != nil {
		t.Fatalf("ListTaskItems() error = %v", err)
	}
	for _, item := range items {
		if item.Status != ItemStatusSuccess {
			t.Fatalf("item %s status = %s, want %s", item.RelativePath, item.Status, ItemStatusSuccess)
		}
		if item.AttemptCount != 1 {
			t.Fatalf("item %s attempts = %d, want 1", item.RelativePath, item.AttemptCount)
		}
		if item.StartedAt == nil || item.CompletedAt == nil {
			t.Fatalf("item %s timestamps = started:%v completed:%v, want both set", item.RelativePath, item.StartedAt, item.CompletedAt)
		}
	}
	if len(uploader.calls) != 2 {
		t.Fatalf("upload calls = %d, want 2", len(uploader.calls))
	}
}

func TestExecuteTaskPartialFailure(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, nil)
	createdTask, err := service.CreateTask("./data", "bucket", "", true, []Item{
		{Path: "a.txt", RelativePath: "a.txt", Size: 1},
		{Path: "b.txt", RelativePath: "b.txt", Size: 2},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	uploader := &fakeUploader{failures: map[string]int{"b.txt": 1}}
	if err := service.ExecuteTask(createdTask.ID, uploader, ExecutionConfig{Workers: 2, MaxAttempts: 1}); err != nil {
		t.Fatalf("ExecuteTask() error = %v", err)
	}

	got, err := service.GetTask(createdTask.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.Status != StatusPartialFailed {
		t.Fatalf("task status = %s, want %s", got.Status, StatusPartialFailed)
	}
	if !strings.Contains(got.LastError, "forced upload failure") {
		t.Fatalf("task last error = %q, want forced upload failure", got.LastError)
	}
	if got.SuccessItems != 1 || got.FailedItems != 1 {
		t.Fatalf("task summary = %+v, want success=1 failed=1", got)
	}

	items, err := service.ListTaskItems(createdTask.ID)
	if err != nil {
		t.Fatalf("ListTaskItems() error = %v", err)
	}

	var sawFailure bool
	for _, item := range items {
		if item.RelativePath == "b.txt" {
			sawFailure = true
			if item.Status != ItemStatusFailed {
				t.Fatalf("failed item status = %s, want %s", item.Status, ItemStatusFailed)
			}
			if item.AttemptCount != 1 {
				t.Fatalf("failed item attempts = %d, want 1", item.AttemptCount)
			}
			if !strings.Contains(item.Error, "forced upload failure") {
				t.Fatalf("failed item error = %q, want forced upload failure", item.Error)
			}
		}
	}
	if !sawFailure {
		t.Fatal("expected to see failed item b.txt")
	}
}

func TestExecuteTaskRetriesThenSucceeds(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, nil)
	createdTask, err := service.CreateTask("./data", "bucket", "prefix", true, []Item{{Path: "a.txt", RelativePath: "a.txt", Size: 1}})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	uploader := &fakeUploader{failures: map[string]int{"prefix/a.txt": 1}}
	if err := service.ExecuteTask(createdTask.ID, uploader, ExecutionConfig{Workers: 1, MaxAttempts: 2}); err != nil {
		t.Fatalf("ExecuteTask() error = %v", err)
	}

	got, err := service.GetTask(createdTask.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.Status != StatusCompleted {
		t.Fatalf("task status = %s, want %s", got.Status, StatusCompleted)
	}

	items, err := service.ListTaskItems(createdTask.ID)
	if err != nil {
		t.Fatalf("ListTaskItems() error = %v", err)
	}
	if items[0].Status != ItemStatusSuccess {
		t.Fatalf("item status = %s, want %s", items[0].Status, ItemStatusSuccess)
	}
	if items[0].AttemptCount != 2 {
		t.Fatalf("item attempts = %d, want 2", items[0].AttemptCount)
	}
	if len(uploader.calls) != 2 {
		t.Fatalf("upload calls = %d, want 2", len(uploader.calls))
	}
}

func TestExecuteTaskStripsLeadingSlashFromRootPrefix(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, nil)
	createdTask, err := service.CreateTask("./data", "bucket", "/", true, []Item{{Path: "a.txt", RelativePath: "nested/a.txt", Size: 1}})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	uploader := &fakeUploader{failures: map[string]int{}}
	if err := service.ExecuteTask(createdTask.ID, uploader, ExecutionConfig{Workers: 1, MaxAttempts: 1}); err != nil {
		t.Fatalf("ExecuteTask() error = %v", err)
	}

	if len(uploader.calls) != 1 {
		t.Fatalf("upload calls = %d, want 1", len(uploader.calls))
	}
	if uploader.calls[0] != "nested/a.txt" {
		t.Fatalf("upload key = %q, want %q", uploader.calls[0], "nested/a.txt")
	}
}

func TestNormalizeObjectPrefix(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		want   string
	}{
		{name: "empty prefix", prefix: "", want: ""},
		{name: "root prefix", prefix: "/", want: ""},
		{name: "trim edge slashes", prefix: "/backup/", want: "backup"},
		{name: "preserve spaces", prefix: " backup ", want: " backup "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeObjectPrefix(tt.prefix)
			if got != tt.want {
				t.Fatalf("normalizeObjectPrefix(%q) = %q, want %q", tt.prefix, got, tt.want)
			}
		})
	}
}

func TestJoinObjectKey(t *testing.T) {
	tests := []struct {
		name         string
		prefix       string
		relativePath string
		want         string
	}{
		{name: "empty prefix", prefix: "", relativePath: "nested/a.txt", want: "nested/a.txt"},
		{name: "root prefix", prefix: "/", relativePath: "nested/a.txt", want: "nested/a.txt"},
		{name: "trim edge slashes", prefix: "/backup/", relativePath: "nested/a.txt", want: "backup/nested/a.txt"},
		{name: "trim relative leading slash", prefix: "backup", relativePath: "/nested/a.txt", want: "backup/nested/a.txt"},
		{name: "empty relative path returns prefix", prefix: "backup", relativePath: "", want: "backup"},
		{name: "convert single windows separator", prefix: "backup", relativePath: "nested\\a.txt", want: "backup/nested/a.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joinObjectKey(tt.prefix, tt.relativePath)
			if got != tt.want {
				t.Fatalf("joinObjectKey(%q, %q) = %q, want %q", tt.prefix, tt.relativePath, got, tt.want)
			}
		})
	}
}

func TestExecuteTaskReturnsErrorWhenUpdateItemStatusFails(t *testing.T) {
	repo := newMemoryRepo()
	repo.failUpdateItemPath = "a.txt"
	service := NewService(repo, nil)
	createdTask, err := service.CreateTask("./data", "bucket", "", true, []Item{{Path: "a.txt", RelativePath: "a.txt", Size: 1}})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	err = service.ExecuteTask(createdTask.ID, &fakeUploader{failures: map[string]int{}}, ExecutionConfig{Workers: 1, MaxAttempts: 1})
	if err == nil {
		t.Fatal("ExecuteTask() error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "forced UpdateItemStatus failure") {
		t.Fatalf("ExecuteTask() error = %v, want UpdateItemStatus failure", err)
	}
}

func TestExecuteTaskReturnsErrorWhenProgressPersistenceFails(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, nil)
	createdTask, err := service.CreateTask("./data", "bucket", "", true, []Item{{Path: "a.txt", RelativePath: "a.txt", Size: 1}})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	repo.failUpdateTask = true
	err = service.ExecuteTask(createdTask.ID, &fakeUploader{failures: map[string]int{}}, ExecutionConfig{Workers: 1, MaxAttempts: 1})
	if err == nil {
		t.Fatal("ExecuteTask() error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "forced UpdateTask failure") {
		t.Fatalf("ExecuteTask() error = %v, want UpdateTask failure", err)
	}
}

func TestExecuteNextQueuedTaskClaimsOldestQueued(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo, nil)
	first, err := service.CreateTask("./first", "bucket", "", true, []Item{{Path: "a.txt", RelativePath: "a.txt", Size: 1}})
	if err != nil {
		t.Fatalf("CreateTask(first) error = %v", err)
	}
	time.Sleep(time.Millisecond)
	second, err := service.CreateTask("./second", "bucket", "", true, []Item{{Path: "b.txt", RelativePath: "b.txt", Size: 1}})
	if err != nil {
		t.Fatalf("CreateTask(second) error = %v", err)
	}

	uploader := &fakeUploader{failures: map[string]int{}}
	executed, ok, err := service.ExecuteNextQueuedTask(uploader, ExecutionConfig{Workers: 1, MaxAttempts: 1})
	if err != nil {
		t.Fatalf("ExecuteNextQueuedTask() error = %v", err)
	}
	if !ok {
		t.Fatal("ExecuteNextQueuedTask() ok = false, want true")
	}
	if executed.ID != first.ID {
		t.Fatalf("ExecuteNextQueuedTask() claimed %s, want %s", executed.ID, first.ID)
	}

	firstTask, err := service.GetTask(first.ID)
	if err != nil {
		t.Fatalf("GetTask(first) error = %v", err)
	}
	if firstTask.Status != StatusCompleted {
		t.Fatalf("first task status = %s, want %s", firstTask.Status, StatusCompleted)
	}
	secondTask, err := service.GetTask(second.ID)
	if err != nil {
		t.Fatalf("GetTask(second) error = %v", err)
	}
	if secondTask.Status != StatusQueued {
		t.Fatalf("second task status = %s, want %s", secondTask.Status, StatusQueued)
	}
}
