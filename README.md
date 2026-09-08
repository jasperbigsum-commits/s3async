# s3async

`s3async` is a secure, efficient, cross-platform asynchronous S3 sync CLI for Windows and Linux.

## Current scope
- Upload local files or download S3 prefixes into local directories
- Async sync task creation
- SQLite persistence for tasks and task items
- Source directory scan with include/exclude filtering
- Foreground execution and queue-aware background worker flow
- File-level status, attempt, and timestamp updates during uploads
- Retry-aware execution pipeline for queued tasks
- Persistent task event logging for execution history and troubleshooting
- Configuration loading via config file and environment variables
- Validation command with actionable environment output

## Commands
```bash
s3async sync <source>
s3async task list
s3async task status <task-id>
s3async task events
s3async task retry <task-id>
s3async task run <task-id>
s3async task worker
s3async daemon run
s3async daemon status
s3async daemon stop
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
go run . task events --task-id <task-id> --limit 100
go run . daemon status
go run . validate --config examples/config.yaml
```

## 从 S3 反向同步到本地

```bash
# 前台下载：backup/reports/a.txt → ./restore/reports/a.txt
s3async sync ./restore --download --bucket my-bucket --prefix backup/ --async=false

# 后台下载，复用任务队列、并发数和重试配置
s3async sync ./restore --download --config examples/config.yaml --async

# 只下载匹配文件；显式空前缀表示整个桶，覆盖配置中的 prefix
s3async sync ./restore --download --bucket my-bucket --prefix "" --include "*.txt" --exclude "private/*"
```

`--download` 将本地路径解释为目标目录，自动创建所需子目录。`--bucket`、`--prefix`、配置文件和环境变量的解析方式与上传一致。前缀按目录处理（`backup` 与 `backup/` 等价），过滤规则作用于去掉前缀后的相对路径；跳过 S3 目录标记。

每次任务会下载所有匹配对象并覆盖本地同名文件，不删除本地多余文件，也不按时间或校验和跳过文件。先写入同目录临时文件，完整接收后再替换目标文件；下载失败保留原文件并清理临时文件。保留 S3 返回的最后修改时间。拒绝路径穿越、目标路径中的符号链接、仅大小写不同的重名对象以及文件/目录冲突。

下载方向存储在任务中，`task run`、`task retry` 和后台 worker 会继续执行下载。`task status` 显示 `mode: download` 和 `items_downloading`。为兼容现有数据库，传输中的计数沿用数据库的 `uploading_items` / `uploading_bytes` 字段。

`security.dry_run: true` 仍需连接 S3 列举对象，但不会创建或覆盖本地文件。下载需要 `s3:ListBucket` 和 `s3:GetObject` 权限。当前每次列举请求及单个文件下载超时为 30 秒；失败重试从文件开头重新下载，不支持断点续传。

## Configuration
See `examples/config.yaml`.

### New S3 Configuration Structure (Recommended)
The new `s3.*` configuration takes precedence over legacy top-level fields:

```yaml
s3:
  profile: default              # AWS profile name
  region: ap-southeast-1         # S3 region
  bucket: my-bucket             # S3 bucket name
  prefix: backups/              # S3 prefix for uploads
  endpoint: http://127.0.0.1:9000  # S3-compatible endpoint (MinIO, etc.)
  force_path_style: true        # Use path-style addressing (required for MinIO)
  skip_tls_verify: false        # Skip TLS verification (dev only!)
  ca_cert_file: ""              # Custom CA certificate file for HTTPS
  static_credentials:           # Static credentials (takes precedence over profile)
    access_key_id: AKIAIOSFODNN7EXAMPLE
    secret_access_key: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
```

### Legacy Fields (Backward Compatible)
Top-level `profile`, `region`, `bucket`, `prefix` are still supported but deprecated.

### Environment Variables
- `S3ASYNC_S3_PROFILE`, `S3ASYNC_S3_REGION`, `S3ASYNC_S3_BUCKET`
- Or legacy: `S3ASYNC_PROFILE`, `S3ASYNC_REGION`, `S3ASYNC_BUCKET`

### MinIO Testing
```bash
# Start MinIO locally
docker run -p 9000:9000 -p 9001:9001 --name minio -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin minio/minio server /data --console-address ":9001"

# Run s3async with MinIO endpoint
go run . validate --config examples/config.yaml
go run . sync ./data --config examples/config.yaml --async=false
```

### Security Notes
- **Never commit secrets** to version control
- Use environment variables or IAM roles in production
- `skip_tls_verify: true` is for local development only
- Use `dry_run: true` to test configuration safely

## Documentation
- `docs/design.md`
- `docs/mvp-plan.md`
- `docs/work-log.md`
- `docs/change-log.md`

## Operational visibility
- Task execution events are appended to `<state_dir>/task-events.jsonl`.
- Daemon lifecycle and queue supervisor events are appended to `<state_dir>/audit.jsonl`.
- Use `s3async task events --task-id <task-id>` to inspect recent item transitions and terminal task outcomes.
- Use `s3async daemon status` to inspect the current daemon PID, heartbeat, and state directory.

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
go build ./...
```

## 构建（Windows 与 Linux）

项目提供了示例构建脚本，支持在本地或在 WSL/CI 中交叉编译为 Windows 可执行文件。

### 构建输出目录

所有构建产物都统一输出到 `dist/` 文件夹（脚本会自动创建）：
- `dist/s3async.exe` — Windows 可执行文件
- `dist/s3async` — Linux 可执行文件

### Windows 构建

使用 `scripts/build-windows.ps1` 构建 Windows 可执行文件：

```powershell
# 默认构建：生成 dist/s3async.exe
.\scripts\build-windows.ps1

# 指定输出名称
.\scripts\build-windows.ps1 -Out myapp
# 生成 dist/myapp.exe

# 创建 release 包
.\scripts\build-windows.ps1 -Out s3async -Release
# 生成 dist/s3async-windows-amd64.zip
```

**C 编译器要求：** Windows 下如果出现 `cgo: C compiler "gcc" not found`，说明没有可用的 C 编译器。推荐安装 MSYS2 并使用 mingw-w64：

```powershell
# 安装 MSYS2 后，在 MSYS2 shell 中运行
pacman -Syu
pacman -S mingw-w64-x86_64-gcc
```

然后将 MSYS2 的 `mingw64\bin` 添加到系统 `PATH`，或者在 PowerShell 中先设置：

```powershell
$env:PATH += ";C:\msys64\mingw64\bin"
```

重新运行脚本即可。

### Linux 构建

使用 `scripts/build-linux.sh` 构建 Linux 可执行文件，或交叉编译为 Windows：

```bash
chmod +x scripts/build-linux.sh

# 默认构建 Linux：生成 dist/s3async
./scripts/build-linux.sh

# 指定输出名称
./scripts/build-linux.sh --out myapp
# 生成 dist/myapp

# 交叉编译为 Windows（需要 mingw-w64）
./scripts/build-linux.sh --target windows --out s3async
# 生成 dist/s3async.exe
```

### 依赖说明

本项目使用 `github.com/mattn/go-sqlite3`，该驱动依赖 CGO 与系统 C 编译器。如果要在 CI 中交叉编译，请确保安装并配置了对应的 mingw 工具链，或考虑替换为纯 Go 驱动（例如 `modernc.org/sqlite`）以避免 CGO。

### 依赖说明

本项目使用 `github.com/mattn/go-sqlite3`，该驱动依赖 CGO 与系统 C 编译器。如果要在 CI 中交叉编译，请确保安装并配置了对应的 mingw 工具链，或考虑替换为纯 Go 驱动（例如 `modernc.org/sqlite`）以避免 CGO。



## Current TODOs
- convert detached worker launch into a first-class long-running daemon/service install mode
- multipart upload and resume
- retry jitter and selective retry policies
- richer CLI integration tests for daemon/task observability flows
