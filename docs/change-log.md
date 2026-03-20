# Change Log

## Unreleased

### Added
- Config loader with file + env support.
- Task item persistence in SQLite.
- Source scanning with relative path capture.
- Include/exclude filter integration in sync flow.
- Validate command with actionable diagnostics.
- Basic uploader structure for AWS SDK v2 integration.
- Unit tests for config, scanner, filter, and task service.
- `task run` command for explicit queued-task execution.
- File-level status persistence updates during upload execution.
- Retry reset path for failed task items.
- `task worker` supervisor command for one-shot or polling queue execution.
- SQLite-backed queued task claiming for supervisor / worker flows.
- SQLite tests covering richer task/item metrics persistence and queue claiming.
- Focused task service tests covering repository failure propagation during per-item execution updates.

### Changed
- Sync command now plans file items before creating tasks.
- Bootstrap now loads config and initializes persistence from resolved settings.
- README and design docs updated to reflect the expanded MVP.
- Foreground sync path now uses the same task executor as async execution.
- Async sync submission now spawns a detached background task runner.
- Task status output now includes item-level summary counts.
- Task/task-item persistence now stores summary counters, last error, attempt counts, and execution timestamps.
- `task list` and `task status` now expose richer observability, timestamps, and failed item details.
- Async background execution now launches queue-aware worker mode instead of only a raw `task run` subprocess.
- Task execution now fails fast when repository updates for item/task progress cannot be persisted.

### Pending
- Dedicated installable background daemon / service mode.
- Richer per-item byte progress metrics and transfer rate visibility.
- Retry backoff strategy improvements with jitter.
- Multipart upload and resume support.
- Self-audit checklist to be applied on every PR.
