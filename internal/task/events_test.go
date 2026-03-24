package task

import (
	"strings"
	"testing"
)

type recordingEventRecorder struct {
	events []TaskEvent
}

func (r *recordingEventRecorder) Record(event TaskEvent) error {
	r.events = append(r.events, event)
	return nil
}

func TestExecuteTaskEmitsEvents(t *testing.T) {
	repo := newMemoryRepo()
	recorder := &recordingEventRecorder{}
	service := NewService(repo, recorder)

	createdTask, err := service.CreateTask("./data", "bucket", "prefix", true, []Item{{Path: "a.txt", RelativePath: "a.txt", Size: 1}})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if err := service.ExecuteTask(createdTask.ID, &fakeUploader{failures: map[string]int{}}, ExecutionConfig{Workers: 1, MaxAttempts: 1}); err != nil {
		t.Fatalf("ExecuteTask() error = %v", err)
	}

	if len(recorder.events) < 4 {
		t.Fatalf("Record() events = %d, want at least 4", len(recorder.events))
	}

	var sawCreated bool
	var sawStarted bool
	var sawUploading bool
	var sawFinished bool
	for _, event := range recorder.events {
		switch {
		case event.Message == "task persisted":
			sawCreated = true
		case event.Message == "task execution started":
			sawStarted = true
		case strings.Contains(event.Message, "item moved to uploading"):
			sawUploading = true
		case strings.Contains(event.Message, "task finished with status completed"):
			sawFinished = true
		}
	}

	if !sawCreated || !sawStarted || !sawUploading || !sawFinished {
		t.Fatalf("missing expected events: created=%v started=%v uploading=%v finished=%v", sawCreated, sawStarted, sawUploading, sawFinished)
	}
}
