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

func BuildSummary(items []Item) Summary {
	summary := Summary{TotalItems: len(items)}
	for _, item := range items {
		incrementSummaryStatus(&summary, item.Status)
	}
	return summary
}

func ApplySummary(t *Task, summary Summary) {
	t.TotalItems = summary.TotalItems
	t.PendingItems = summary.PendingItems
	t.UploadingItems = summary.UploadingItems
	t.SuccessItems = summary.SuccessItems
	t.FailedItems = summary.FailedItems
	t.SkippedItems = summary.SkippedItems
}

func TaskSummary(t Task) Summary {
	return Summary{
		TotalItems:     t.TotalItems,
		PendingItems:   t.PendingItems,
		UploadingItems: t.UploadingItems,
		SuccessItems:   t.SuccessItems,
		FailedItems:    t.FailedItems,
		SkippedItems:   t.SkippedItems,
	}
}

func MoveSummaryItem(summary Summary, from ItemStatus, to ItemStatus) Summary {
	decrementSummaryStatus(&summary, from)
	incrementSummaryStatus(&summary, to)
	return summary
}

func incrementSummaryStatus(summary *Summary, status ItemStatus) {
	switch status {
	case ItemStatusPending:
		summary.PendingItems++
	case ItemStatusUploading:
		summary.UploadingItems++
	case ItemStatusSuccess:
		summary.SuccessItems++
	case ItemStatusFailed:
		summary.FailedItems++
	case ItemStatusSkipped:
		summary.SkippedItems++
	}
}

func decrementSummaryStatus(summary *Summary, status ItemStatus) {
	switch status {
	case ItemStatusPending:
		if summary.PendingItems > 0 {
			summary.PendingItems--
		}
	case ItemStatusUploading:
		if summary.UploadingItems > 0 {
			summary.UploadingItems--
		}
	case ItemStatusSuccess:
		if summary.SuccessItems > 0 {
			summary.SuccessItems--
		}
	case ItemStatusFailed:
		if summary.FailedItems > 0 {
			summary.FailedItems--
		}
	case ItemStatusSkipped:
		if summary.SkippedItems > 0 {
			summary.SkippedItems--
		}
	}
}
