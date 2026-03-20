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
The first MVP uses a local async submission model:
- CLI creates a task record
- CLI scans the source and persists file-level task items
- CLI can execute in foreground or async-submission mode
- Task metadata is persisted to SQLite
- Later daemonization can reuse the same task store and state machine

## Persistence
SQLite database stores:
- tasks
- task_items

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
- background daemon
- multipart upload and resume
- task item progress updates
- MinIO-compatible mode
- rate limiting
- checksums and manifests
