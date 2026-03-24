package app

import (
	internallogging "github.com/jasperbigsum-commits/s3async/internal/logging"
	"github.com/jasperbigsum-commits/s3async/internal/task"
)

type taskEventRecorderAdapter struct {
	recorder *internallogging.FileAuditRecorder
}

func (a taskEventRecorderAdapter) Record(event task.TaskEvent) error {
	return a.recorder.RecordTaskEvent(event)
}
