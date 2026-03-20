package task

import (
	"testing"
)

type memoryRepo struct {
	items map[string]Task
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{items: map[string]Task{}}
}

func (m *memoryRepo) Create(task Task) error {
	m.items[task.ID] = task
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
	return m.items[id], nil
}

func (m *memoryRepo) UpdateStatus(id string, status Status) error {
	item := m.items[id]
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

			task, err := service.CreateTask("./data", "bucket", "prefix", tt.async)
			if err != nil {
				t.Fatalf("CreateTask() error = %v", err)
			}

			if task.Status != tt.wantStatus {
				t.Fatalf("CreateTask() status = %s, want %s", task.Status, tt.wantStatus)
			}
		})
	}
}

func TestRetryTask(t *testing.T) {
	repo := newMemoryRepo()
	service := NewService(repo)
	created, err := service.CreateTask("./data", "bucket", "prefix", false)
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if err := service.RetryTask(created.ID); err != nil {
		t.Fatalf("RetryTask() error = %v", err)
	}

	got, err := service.GetTask(created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}

	if got.Status != StatusQueued {
		t.Fatalf("GetTask() status after retry = %s, want %s", got.Status, StatusQueued)
	}
}
