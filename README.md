# s3async

`s3async` is a secure, efficient, cross-platform asynchronous S3 sync CLI for Windows and Linux.

## Current scope
- Async sync task creation
- Task status persistence via SQLite
- Concurrent uploads
- Include/exclude filters
- Dry-run mode
- AWS credential resolution from environment/profile/role
- Log redaction-ready structured logging

## Commands
```bash
s3async sync <source>
s3async task list
s3async task status <task-id>
s3async task retry <task-id>
s3async validate
s3async version
```

## Quick start
```bash
go run . sync ./data --bucket my-bucket --prefix backup/ --async
```

## Configuration
See `examples/config.yaml`.

## Documentation
- `docs/design.md`
- `docs/mvp-plan.md`

## Security
- Do not persist AWS secrets in task storage.
- Prefer environment variables, AWS profile, or IAM role.
- Add least-privilege IAM policies for S3 access.

## Development
```bash
go test ./...
go test -race ./...
go test -cover ./...
```
