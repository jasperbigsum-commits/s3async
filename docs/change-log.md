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

### Changed
- Sync command now plans file items before creating tasks.
- Bootstrap now loads config and initializes persistence from resolved settings.
- README and design docs updated to reflect the expanded MVP.

### Pending
- Background async worker execution.
- File-level state updates during upload.
- Retry and backoff execution.
- Multipart upload and resume support.
- Self-audit checklist to be applied on every PR.
