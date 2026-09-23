# Change Log

## Unreleased

### Added
- `path_style: faithful` config: reversible `%2F`/`%2E`/`%2E%2E` escaping so S3 keys with empty or dot segments (e.g. `backup//a.txt`) restore to distinct local paths; uploads decode back to the true keys. `s3async validate` prints the active style.
- `--on-collision skip` flag for `sync --download`: colliding objects become skipped items with reasons instead of aborting the whole plan; collision errors now name both full S3 keys plus the shared local file.
- Download planning dedupes exactly repeated S3 keys across LIST pages (concurrent bucket writes can repeat keys), keeping the latest listing state.
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
- Persistent `task-events.jsonl` execution event log and `task events` CLI for recent task/item history inspection.
- Command-package tests covering worker empty-queue messaging, daemon stop requests, task status failed-item limits, and task-event filtering/bootstrap error paths.

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
- Runtime observability is now split between daemon lifecycle audit logs and per-task execution event logs.
- Task execution now uses a bounded worker queue and process-local duplicate-execution guard; SQLite queue claims use one atomic conditional update so concurrent workers cannot claim the same task.

### Pending
- Dedicated installable background daemon / service mode.
- Richer per-item byte progress metrics and transfer rate visibility.
- Retry backoff strategy improvements with jitter.
- Multipart upload and resume support.
- Self-audit checklist to be applied on every PR.
- GitHub code search automation once `gh` auth is available in the execution environment.
