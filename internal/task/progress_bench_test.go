package task

import (
	"fmt"
	"testing"
)

type noopProgressRecorder struct{}

func (noopProgressRecorder) Record(TaskEvent) error { return nil }

func BenchmarkExecuteTaskProgress(b *testing.B) {
	const n = 2000
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		repo := newMemoryRepo()
		service := NewService(repo, noopProgressRecorder{})
		items := make([]Item, 0, n)
		for j := 0; j < n; j++ {
			name := fmt.Sprintf("f%05d.txt", j)
			items = append(items, Item{Path: name, RelativePath: name, Size: 1})
		}
		created, err := service.CreateTask("./data", "bucket", "", false, items)
		if err != nil {
			b.Fatal(err)
		}
		uploader := &fakeUploader{failures: map[string]int{}}
		b.StartTimer()
		if err := service.ExecuteTask(created.ID, uploader, ExecutionConfig{Workers: 4, MaxAttempts: 1}); err != nil {
			b.Fatal(err)
		}
	}
}
