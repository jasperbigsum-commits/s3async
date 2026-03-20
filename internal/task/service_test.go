package task

import (
	"fmt"
	"testing"
)

type memoryRepo struct {
	items     map[string]Task
	itemIndex map[string][]Item
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{items: map[string]Task{}, itemIndex: map[string][]Item{}}
}

func (m *memoryRepo) Create(task Task, items []Item) error {
	m.items[task.ID] = task
	m.itemIndex[task.ID] = append([]Item(nil), items...)
	return nil
}

func (m *memoryRepo) List() ([]Task, error) {
	result := make([]Task, 0, len(m.items))
	for _, item := range m.items {
		result = append(result, item)
	}
	return result, nil
}

func (m *memoryRepo) Get(id string) (Task, error) {
	item, ok := m.items[id]
	if !ok {
		return Task{}, fmt.Errorf("task not found: %s", id)
	}
	return item, nil
}

func (m *memoryRepo) ListItems(taskID string) ([]Item, error) {
	return append([]Item(nil), m.itemIndex[taskID]...), nil
}

func (m *memoryRepo) UpdateStatus(id string, status Status) error {
	item, ok := m.items[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	item.Status = status
	m.items[id] = item
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
			service := NewService(repo)

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
	service := NewService(repo)

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
}

func TestRetryTask(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo)
	createdTask, err := service.CreateTask("./data", "bucket", "prefix", false, nil)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
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
}
