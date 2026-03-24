package task

import "time"

type EventRecorder interface {
	Record(TaskEvent) error
}

func NewTaskEvent(_ string, task Task, item Item, cfg ExecutionConfig, summary Summary, message string, errMsg string) TaskEvent {
	return TaskEvent{
		Time:        time.Now().UTC(),
		TaskID:      task.ID,
		TaskStatus:  task.Status,
		ItemPath:    item.RelativePath,
		ItemStatus:  item.Status,
		Attempt:     item.AttemptCount,
		Size:        item.Size,
		Message:     message,
		Error:       errMsg,
		Bucket:      task.Bucket,
		Prefix:      task.Prefix,
		Source:      task.Source,
		Summary:     summary,
		Workers:     cfg.Workers,
		MaxAttempts: cfg.MaxAttempts,
	}
}

func FindItemByRelativePath(items []Item, relativePath string) (Item, bool) {
	for _, item := range items {
		if item.RelativePath == relativePath {
			return item, true
		}
	}
	return Item{}, false
}
