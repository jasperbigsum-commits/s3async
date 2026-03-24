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

type Summary struct {
	TotalItems     int
	PendingItems   int
	UploadingItems int
	SuccessItems   int
	FailedItems    int
	SkippedItems   int
	TotalBytes     int64
	PendingBytes   int64
	UploadingBytes int64
	SuccessBytes   int64
	FailedBytes    int64
	SkippedBytes   int64
	CompletedItems int
	CompletedBytes int64
}

type Task struct {
	ID             string
	Source         string
	Bucket         string
	Prefix         string
	Mode           string
	Status         Status
	TotalItems     int
	PendingItems   int
	UploadingItems int
	SuccessItems   int
	FailedItems    int
	SkippedItems   int
	TotalBytes     int64
	PendingBytes   int64
	UploadingBytes int64
	SuccessBytes   int64
	FailedBytes    int64
	SkippedBytes   int64
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	StartedAt      *time.Time
	CompletedAt    *time.Time
}

type Item struct {
	TaskID       string
	Path         string
	RelativePath string
	Size         int64
	Status       ItemStatus
	Error        string
	AttemptCount int
	CreatedAt    time.Time
	UpdatedAt    time.Time
	StartedAt    *time.Time
	CompletedAt  *time.Time
}

type TaskEvent struct {
	Time        time.Time
	TaskID      string
	TaskStatus  Status
	ItemPath    string
	ItemStatus  ItemStatus
	Attempt     int
	Size        int64
	Message     string
	Error       string
	Bucket      string
	Prefix      string
	Source      string
	Summary     Summary
	Workers     int
	MaxAttempts int
}

func BuildSummary(items []Item) Summary {
	summary := Summary{TotalItems: len(items)}
	for _, item := range items {
		summary.TotalBytes += item.Size
		incrementSummaryStatus(&summary, item.Status, item.Size)
	}
	summary.CompletedItems = summary.SuccessItems + summary.FailedItems + summary.SkippedItems
	summary.CompletedBytes = summary.SuccessBytes + summary.FailedBytes + summary.SkippedBytes
	return summary
}

func ApplySummary(t *Task, summary Summary) {
	t.TotalItems = summary.TotalItems
	t.PendingItems = summary.PendingItems
	t.UploadingItems = summary.UploadingItems
	t.SuccessItems = summary.SuccessItems
	t.FailedItems = summary.FailedItems
	t.SkippedItems = summary.SkippedItems
	t.TotalBytes = summary.TotalBytes
	t.PendingBytes = summary.PendingBytes
	t.UploadingBytes = summary.UploadingBytes
	t.SuccessBytes = summary.SuccessBytes
	t.FailedBytes = summary.FailedBytes
	t.SkippedBytes = summary.SkippedBytes
}

func TaskSummary(t Task) Summary {
	summary := Summary{
		TotalItems:     t.TotalItems,
		PendingItems:   t.PendingItems,
		UploadingItems: t.UploadingItems,
		SuccessItems:   t.SuccessItems,
		FailedItems:    t.FailedItems,
		SkippedItems:   t.SkippedItems,
		TotalBytes:     t.TotalBytes,
		PendingBytes:   t.PendingBytes,
		UploadingBytes: t.UploadingBytes,
		SuccessBytes:   t.SuccessBytes,
		FailedBytes:    t.FailedBytes,
		SkippedBytes:   t.SkippedBytes,
	}
	summary.CompletedItems = summary.SuccessItems + summary.FailedItems + summary.SkippedItems
	summary.CompletedBytes = summary.SuccessBytes + summary.FailedBytes + summary.SkippedBytes
	return summary
}

func MoveSummaryItem(summary Summary, from ItemStatus, to ItemStatus, size int64) Summary {
	decrementSummaryStatus(&summary, from, size)
	incrementSummaryStatus(&summary, to, size)
	summary.CompletedItems = summary.SuccessItems + summary.FailedItems + summary.SkippedItems
	summary.CompletedBytes = summary.SuccessBytes + summary.FailedBytes + summary.SkippedBytes
	return summary
}

func incrementSummaryStatus(summary *Summary, status ItemStatus, size int64) {
	switch status {
	case ItemStatusPending:
		summary.PendingItems++
		summary.PendingBytes += size
	case ItemStatusUploading:
		summary.UploadingItems++
		summary.UploadingBytes += size
	case ItemStatusSuccess:
		summary.SuccessItems++
		summary.SuccessBytes += size
	case ItemStatusFailed:
		summary.FailedItems++
		summary.FailedBytes += size
	case ItemStatusSkipped:
		summary.SkippedItems++
		summary.SkippedBytes += size
	}
}

func decrementSummaryStatus(summary *Summary, status ItemStatus, size int64) {
	switch status {
	case ItemStatusPending:
		if summary.PendingItems > 0 {
			summary.PendingItems--
		}
		if summary.PendingBytes >= size {
			summary.PendingBytes -= size
		}
	case ItemStatusUploading:
		if summary.UploadingItems > 0 {
			summary.UploadingItems--
		}
		if summary.UploadingBytes >= size {
			summary.UploadingBytes -= size
		}
	case ItemStatusSuccess:
		if summary.SuccessItems > 0 {
			summary.SuccessItems--
		}
		if summary.SuccessBytes >= size {
			summary.SuccessBytes -= size
		}
	case ItemStatusFailed:
		if summary.FailedItems > 0 {
			summary.FailedItems--
		}
		if summary.FailedBytes >= size {
			summary.FailedBytes -= size
		}
	case ItemStatusSkipped:
		if summary.SkippedItems > 0 {
			summary.SkippedItems--
		}
		if summary.SkippedBytes >= size {
			summary.SkippedBytes -= size
		}
	}
}
