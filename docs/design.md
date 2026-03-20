# s3async Design

## Positioning
A Go-based asynchronous S3 sync CLI for Windows and Linux.

## Goals
- Submit sync jobs without blocking the terminal
- Persist task state locally
- Persist file-level task items for later execution and inspection
- Resume/retry failed work
- Support safe credential loading
- Scale with concurrent upload workers

## High-level architecture
1. CLI layer (`cmd/`)
2. App orchestration (`internal/app`)
3. Configuration loading (`internal/config`)
4. Task management (`internal/task`)
5. Local persistence (`internal/store`)
6. Scan/filter pipeline (`internal/scanner`, `internal/filter`)
7. Upload execution (`internal/uploader`)

## Task lifecycle
- pending
- scanning
- queued
- running
- completed
- partial_failed
- failed
- canceled

## File lifecycle
- pending
- uploading
- success
- failed
- skipped

## Async model
The current MVP uses a local async submission model with a queue-aware worker path:
- CLI creates a task record
- CLI scans the source and persists file-level task items
- `sync --async` submits the task and launches a detached worker process for that task
- `task worker` can run once or poll the queue as a lightweight supervisor loop
- worker mode atomically claims the oldest queued task from SQLite before executing it
- task metadata and item metadata are persisted to SQLite for later inspection and retry

## Persistence
SQLite database stores:
- tasks
- task_items

Persisted execution metadata now includes:
- task summary counters (pending/uploading/success/failed/skipped)
- task started/completed timestamps
- task last error string
- per-item attempt counts
- per-item started/completed timestamps

No AWS secret material is persisted.

## Validation model
`validate` reports:
- resolved database path
- region and bucket presence
- visible credential path hints
- dry-run and worker settings

## Security notes
- Resolve credentials from AWS SDK default chain
- Use context timeouts for network calls
- Avoid printing signed URLs, secret keys, or session tokens
- Keep mirror/delete semantics out of MVP

## Future extensions
- installable background daemon/service
- multipart upload and resume
- byte-level task item progress updates
- MinIO-compatible mode
- rate limiting
- checksums and manifests
