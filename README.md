# s3async

`s3async` is a secure, efficient, cross-platform asynchronous S3 sync CLI for Windows and Linux.

## Current scope
- Async sync task creation
- SQLite persistence for tasks and task items
- Source directory scan with include/exclude filtering
- Foreground execution and queue-aware background worker flow
- File-level status, attempt, and timestamp updates during uploads
- Retry-aware execution pipeline for queued tasks
- Configuration loading via config file and environment variables
- Validation command with actionable environment output

## Commands
```bash
s3async sync <source>
s3async task list
s3async task status <task-id>
s3async task retry <task-id>
s3async task run <task-id>
s3async task worker
s3async validate
s3async version
```

## Quick start
```bash
go run . sync ./data --bucket my-bucket --prefix backup/ --async
go run . sync ./data --config examples/config.yaml --async=false
go run . task worker --once
go run . task worker --poll-interval 2s --idle-timeout 30s
go run . task status <task-id> --failed-limit 20
go run . validate --config examples/config.yaml
```

## Configuration
See `examples/config.yaml`.

Supported configuration sources:
- `--config /path/to/config.yaml`
- `config.yaml` in current directory
- `~/.s3async/config.yaml`
- environment variables such as `S3ASYNC_BUCKET`, `S3ASYNC_REGION`, `S3ASYNC_DB_PATH`

## Documentation
- `docs/design.md`
- `docs/mvp-plan.md`
- `docs/work-log.md`
- `docs/change-log.md`

## Security
- Do not persist AWS secrets in task storage.
- Prefer environment variables, AWS profile, or IAM role.
- Add least-privilege IAM policies for S3 access.
- Dry-run mode can be used to verify scan, planning, and execution logic safely.

## Development
```bash
go mod tidy
go test ./...
go test -race ./...
go test -cover ./...
```

## Current TODOs
- convert detached worker launch into a first-class long-running daemon/service install mode
- multipart upload and resume
- retry jitter and selective retry policies
- MinIO and compatible S3 endpoints
- structured execution logs and self-audit checklist automation
