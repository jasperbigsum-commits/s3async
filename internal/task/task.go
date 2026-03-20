package task

import "time"

type Status string

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
