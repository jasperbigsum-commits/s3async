package store

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jasperbigsum-commits/s3async/internal/task"
)

func TestSQLiteRepositoryPersistsTaskAndItemMetrics(t *testing.T) {
	repo, dbPath := newTestSQLiteRepo(t)
	now := time.Now().UTC().Truncate(time.Second)
	started := now.Add(2 * time.Second)
	completed := now.Add(4 * time.Second)

	itemStarted := now.Add(time.Second)
	itemCompleted := now.Add(3 * time.Second)
	created := task.Task{
		ID:             "task_metrics",
		Source:         "/tmp/source",
		Bucket:         "bucket",
		Prefix:         "prefix",
		Mode:           "update",
		Status:         task.StatusPartialFailed,
		TotalItems:     2,
		PendingItems:   0,
		UploadingItems: 0,
		SuccessItems:   1,
		FailedItems:    1,
		SkippedItems:   0,
		LastError:      "boom",
		CreatedAt:      now,
		UpdatedAt:      now,
		StartedAt:      &started,
		CompletedAt:    &completed,
	}
	items := []task.Item{
		{
			TaskID:       created.ID,
			Path:         filepath.Join(filepath.Dir(dbPath), "a.txt"),
			RelativePath: "a.txt",
			Size:         1,
			Status:       task.ItemStatusSuccess,
			AttemptCount: 1,
			CreatedAt:    now,
			UpdatedAt:    now,
			StartedAt:    &itemStarted,
			CompletedAt:  &itemCompleted,
		},
		{
			TaskID:       created.ID,
			Path:         filepath.Join(filepath.Dir(dbPath), "b.txt"),
			RelativePath: "b.txt",
			Size:         2,
			Status:       task.ItemStatusFailed,
			Error:        "boom",
			AttemptCount: 2,
			CreatedAt:    now,
			UpdatedAt:    now,
			StartedAt:    &itemStarted,
			CompletedAt:  &itemCompleted,
		},
	}

	if err := repo.Create(created, items); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	gotTask, err := repo.Get(created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if gotTask.TotalItems != 2 || gotTask.SuccessItems != 1 || gotTask.FailedItems != 1 {
		t.Fatalf("Get() summary = %+v, want total=2 success=1 failed=1", gotTask)
	}
	if gotTask.StartedAt == nil || gotTask.CompletedAt == nil {
		t.Fatalf("Get() timestamps = started:%v completed:%v, want both set", gotTask.StartedAt, gotTask.CompletedAt)
	}
	if gotTask.LastError != "boom" {
		t.Fatalf("Get() last error = %q, want boom", gotTask.LastError)
	}

	gotItems, err := repo.ListItems(created.ID)
	if err != nil {
		t.Fatalf("ListItems() error = %v", err)
	}
	if len(gotItems) != 2 {
		t.Fatalf("ListItems() len = %d, want 2", len(gotItems))
	}
	if gotItems[1].AttemptCount != 2 {
		t.Fatalf("ListItems()[1].AttemptCount = %d, want 2", gotItems[1].AttemptCount)
	}
	if gotItems[1].CompletedAt == nil {
		t.Fatalf("ListItems()[1].CompletedAt = nil, want set")
	}
}

func TestSQLiteRepositoryClaimNextQueued(t *testing.T) {
	repo, _ := newTestSQLiteRepo(t)
	now := time.Now().UTC()
	older := task.Task{
		ID:           "older",
		Source:       "/older",
		Bucket:       "bucket",
		Mode:         "update",
		Status:       task.StatusQueued,
		TotalItems:   1,
		PendingItems: 1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	newer := task.Task{
		ID:           "newer",
		Source:       "/newer",
		Bucket:       "bucket",
		Mode:         "update",
		Status:       task.StatusQueued,
		TotalItems:   1,
		PendingItems: 1,
		CreatedAt:    now.Add(time.Second),
		UpdatedAt:    now.Add(time.Second),
	}

	if err := repo.Create(older, []task.Item{{TaskID: older.ID, Path: "/older/a.txt", RelativePath: "a.txt", Size: 1, Status: task.ItemStatusPending, CreatedAt: now, UpdatedAt: now}}); err != nil {
		t.Fatalf("Create(older) error = %v", err)
	}
	if err := repo.Create(newer, []task.Item{{TaskID: newer.ID, Path: "/newer/b.txt", RelativePath: "b.txt", Size: 1, Status: task.ItemStatusPending, CreatedAt: now, UpdatedAt: now}}); err != nil {
		t.Fatalf("Create(newer) error = %v", err)
	}

	claimed, ok, err := repo.ClaimNextQueued()
	if err != nil {
		t.Fatalf("ClaimNextQueued() error = %v", err)
	}
	if !ok {
		t.Fatal("ClaimNextQueued() ok = false, want true")
	}
	if claimed.ID != older.ID {
		t.Fatalf("ClaimNextQueued() claimed %s, want %s", claimed.ID, older.ID)
	}
	if claimed.Status != task.StatusRunning {
		t.Fatalf("ClaimNextQueued() status = %s, want %s", claimed.Status, task.StatusRunning)
	}
	if claimed.StartedAt == nil {
		t.Fatal("ClaimNextQueued() started_at = nil, want set")
	}

	reloaded, err := repo.Get(older.ID)
	if err != nil {
		t.Fatalf("Get(older) error = %v", err)
	}
	if reloaded.Status != task.StatusRunning {
		t.Fatalf("Get(older) status = %s, want %s", reloaded.Status, task.StatusRunning)
	}
}

func newTestSQLiteRepo(t *testing.T) (*SQLiteTaskRepository, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	repo, err := NewSQLiteTaskRepository(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteTaskRepository() error = %v", err)
	}
	t.Cleanup(func() {
		if err := repo.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return repo, dbPath
}

func TestSQLiteRepositoryClaimNextQueuedIsAtomicAcrossRepositories(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	first, err := NewSQLiteTaskRepository(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteTaskRepository(first) error = %v", err)
	}
	defer first.Close()
	second, err := NewSQLiteTaskRepository(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteTaskRepository(second) error = %v", err)
	}
	defer second.Close()

	now := time.Now().UTC()
	queued := task.Task{
		ID:           "atomic-claim",
		Source:       "/source",
		Bucket:       "bucket",
		Mode:         "update",
		Status:       task.StatusQueued,
		TotalItems:   1,
		PendingItems: 1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := first.Create(queued, []task.Item{{
		TaskID:       queued.ID,
		Path:         "/source/a.txt",
		RelativePath: "a.txt",
		Size:         1,
		Status:       task.ItemStatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
	}}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	claims := 0
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			repo := first
			if i%2 == 1 {
				repo = second
			}
			claimed, ok, err := repo.ClaimNextQueued()
			if err != nil {
				errs <- err
				return
			}
			if ok {
				if claimed.ID != queued.ID || claimed.Status != task.StatusRunning {
					errs <- fmt.Errorf("unexpected claim: %+v", claimed)
					return
				}
				mu.Lock()
				claims++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if claims != 1 {
		t.Fatalf("claims = %d, want exactly 1", claims)
	}
}
