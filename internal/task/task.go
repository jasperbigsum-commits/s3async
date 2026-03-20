package task

import "time"

type Status string

type ItemStatus string

const (
	StatusPending       Status = "pending"
	StatusScanning      Status = "scanning"
	StatusQueued        Status = "queued"
	StatusRunning       Status = "running"
	StatusCompleted     Status = "completed"
	StatusPartialFailed Status = "partial_failed"
	StatusFailed        Status = "failed"
	StatusCanceled      Status = "canceled"
)

const (
	ItemStatusPending   ItemStatus = "pending"
	ItemStatusUploading ItemStatus = "uploading"
	ItemStatusSuccess   ItemStatus = "success"
	ItemStatusFailed    ItemStatus = "failed"
	ItemStatusSkipped   ItemStatus = "skipped"
)

type Task struct {
	ID        string
	Source    string
	Bucket    string
	Prefix    string
	Mode      string
	Status    Status
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Item struct {
	TaskID       string
	Path         string
	RelativePath string
	Size         int64
	Status       ItemStatus
	Error        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
