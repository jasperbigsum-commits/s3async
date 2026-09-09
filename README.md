# s3async

`s3async` 是一个面向 macOS、Windows 和 Linux 的安全、高效、跨平台 S3 异步同步命令行工具。
它支持本地目录上传到 S3、从 S3 下载到本地、增量同步、任务队列、失败重试和后台守护进程。

`s3async` is a secure, efficient, cross-platform asynchronous S3 sync CLI for macOS, Windows and Linux.

## 功能概览 / Features

- 本地文件上传到 S3，或将 S3 前缀下载到本地目录 / Upload and download
- 增量上传：跳过目标端大小和修改时间均未变化的文件 / Incremental upload
- 异步任务提交、SQLite 持久化、队列 worker 和后台 daemon / Async jobs and workers
- include/exclude 文件过滤、文件级状态、失败重试和任务事件记录 / Filtering and retries
- 配置文件、环境变量、AWS Profile、静态凭证和 S3 兼容端点 / Flexible configuration

## 命令速查 / Commands

命令默认使用配置文件中的数据库和 S3 设置。需要查询某个任务时，请使用创建任务时相同的 `--config` 参数。

| 命令 | 说明 |
| --- | --- |
| `s3async sync <local-path>` | 上传本地目录到 S3 |
| `s3async sync <local-path> --download` | 下载 S3 前缀到本地目录 |
| `s3async sync <local-path> --incremental` | 增量同步，跳过未变化文件（上传和下载均支持） |
| `s3async task list` | 列出任务及汇总状态 |
| `s3async task status <task-id>` | 查看任务和文件级状态 |
| `s3async task events` | 查看任务执行事件 |
| `s3async task retry <task-id>` | 重置失败项并重新排队 |
| `s3async task run <task-id>` | 前台执行指定任务 |
| `s3async task worker` | 执行队列中的任务；可使用 `--once` 只执行一次 |
| `s3async daemon run` | 启动后台任务守护进程 |
| `s3async daemon status` | 查看守护进程状态 |
| `s3async daemon stop` | 停止守护进程 |
| `s3async validate` | 检查配置、数据库和凭证环境 |
| `s3async version` | 输出版本号 |

常用命令也可以直接复制使用：
```bash
# 上传目录（异步提交，立即返回任务 ID）
s3async sync ./data --bucket my-bucket --prefix backup/ --async

# 上传目录并在当前终端等待完成
s3async sync ./data --config examples/config.yaml --async=false

# 增量上传：目标端未变化的文件会标记为 skipped
s3async sync ./data --bucket my-bucket --prefix backup/ --incremental --async=false

# 查看任务、事件和失败项
s3async task list
s3async task status <task-id> --failed-limit 20
s3async task events --task-id <task-id> --limit 100
```

增量规划需要列举 S3 对象，因此账号需要 `s3:ListBucket` 权限。源端删除的文件不会从 S3 删除。

## 快速开始 / Quick start
```bash
# 1. 前台上传
go run . sync ./data --bucket my-bucket --prefix backup/ --async

# 2. 使用配置文件前台执行
go run . sync ./data --config examples/config.yaml --async=false

# 3. 启动一次 worker，处理一个排队任务
go run . task worker --once

# 4. worker 持续轮询，空闲 30 秒后退出
go run . task worker --poll-interval 2s --idle-timeout 30s

# 5. 检查配置和运行环境
go run . task status <task-id> --failed-limit 20
go run . task events --task-id <task-id> --limit 100
go run . daemon status
go run . validate --config examples/config.yaml
```

## 从 S3 下载到本地 / Download from S3

```bash
# 前台下载：backup/reports/a.txt -> ./restore/reports/a.txt
s3async sync ./restore --download --bucket my-bucket --prefix backup/ --async=false

# 异步下载：复用任务队列、并发数和重试配置
s3async sync ./restore --download --config examples/config.yaml --async

# 只下载匹配文件；空前缀表示整个桶，并覆盖配置中的 prefix
s3async sync ./restore --download --bucket my-bucket --prefix "" --include "*.txt" --exclude "private/*"
```

`--download` 将本地路径解释为目标目录，自动创建所需子目录。`--bucket`、`--prefix`、配置文件和环境变量的解析方式与上传一致。前缀按目录处理（`backup` 与 `backup/` 等价），过滤规则作用于去掉前缀后的相对路径；跳过 S3 目录标记。

默认每次任务会下载所有匹配对象并覆盖本地同名文件；使用 `--incremental` 时，大小和修改时间均未变化的文件会标记为 `skipped`。不删除本地多余文件，也不执行删除同步。下载先写入同目录临时文件，完整接收后再替换目标文件；失败时保留原文件并清理临时文件。保留 S3 返回的最后修改时间。拒绝路径穿越、目标路径中的符号链接、仅大小写不同的重名对象以及文件/目录冲突。

查询任务时应使用创建任务时的同一份配置，否则可能查询到另一个数据库：

```bash
go run . task list --config examples/config-xc.yaml
go run . task status <task-id> --config examples/config-xc.yaml
```

`task list` 显示 `direction=upload/download`、来源、目标、文件数与已成功传输/跳过文件的字节数；空列表会提示当前数据库路径。字节数按文件完成后累计，并非实时网络传输速率。

下载方向存储在任务中，`task run`、`task retry` 和后台 worker 会继续执行下载。`task status` 显示 `mode: download` 和 `items_downloading`。为兼容现有数据库，传输中的计数沿用数据库的 `uploading_items` / `uploading_bytes` 字段。

`security.dry_run: true` 仍需连接 S3 列举对象，但不会创建或覆盖本地文件。下载需要 `s3:ListBucket` 和 `s3:GetObject` 权限。当前每次列举请求及单个文件下载超时为 30 秒；失败重试从文件开头重新下载，不支持断点续传。

## 配置 / Configuration

完整示例见 `examples/config.yaml`。配置文件未指定时，程序会依次查找当前目录和 `~/.s3async/config.yaml`。

### 推荐配置：`s3.*`

新的 `s3.*` 配置优先于旧版顶层字段：

```yaml
s3:
  profile: default              # AWS Profile 名称
  region: ap-southeast-1        # S3 区域
  bucket: my-bucket              # S3 存储桶
  prefix: backups/               # 上传使用的 S3 前缀
  endpoint: http://127.0.0.1:9000 # S3 兼容端点（如 MinIO）
  force_path_style: true         # 使用路径式访问（MinIO 通常需要）
  skip_tls_verify: false         # 跳过 TLS 校验，仅建议开发环境使用
  ca_cert_file: ""               # HTTPS 自定义 CA 证书文件
  static_credentials:            # 静态凭证，优先于 Profile
    access_key_id: AKIAIOSFODNN7EXAMPLE
    secret_access_key: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
```

### 旧版字段（兼容）

顶层 `profile`、`region`、`bucket`、`prefix` 仍可使用，但已不推荐新增配置继续采用。

### 环境变量 / Environment variables
- 推荐变量：`S3ASYNC_S3_PROFILE`、`S3ASYNC_S3_REGION`、`S3ASYNC_S3_BUCKET`
- 兼容旧变量：`S3ASYNC_PROFILE`、`S3ASYNC_REGION`、`S3ASYNC_BUCKET`

### MinIO 本地测试
```bash
# 启动本地 MinIO
docker run -p 9000:9000 -p 9001:9001 --name minio -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin minio/minio server /data --console-address ":9001"

# 使用 MinIO 端点验证并执行同步
go run . validate --config examples/config.yaml
go run . sync ./data --config examples/config.yaml --async=false
```

### 安全说明

- 不要将密钥提交到版本库。
- 生产环境优先使用环境变量、AWS Profile 或 IAM Role。
- `skip_tls_verify: true` 仅用于本地开发测试。
- 使用 `dry_run: true` 安全检查扫描和规划逻辑。

## 文档 / Documentation
- `docs/design.md`
- `docs/mvp-plan.md`
- `docs/work-log.md`
- `docs/change-log.md`

## 运行状态与日志 / Operational visibility

- 任务执行事件写入 `<state_dir>/task-events.jsonl`。
- daemon 生命周期和队列事件写入 `<state_dir>/audit.jsonl`。
- 使用 `s3async task events --task-id <task-id>` 查看文件状态变化和任务结果。
- 使用 `s3async daemon status` 查看 daemon PID、心跳和状态目录。

## 安全 / Security

- 任务数据库不会保存 AWS 密钥。
- 生产环境优先使用环境变量、AWS Profile 或 IAM Role。
- 为 S3 配置最小权限策略；上传需要 `s3:PutObject`，增量规划需要 `s3:ListBucket`。
- `dry_run` 可用于安全检查扫描、规划和任务逻辑。

## 开发与验证 / Development
```bash
go mod tidy
go test ./...
go test -race ./...
go test -cover ./...
go build ./...
```

## GitHub 自动发布

推送符合语义化版本规范的 `v` 前缀 tag 后，`.github/workflows/release.yml` 自动测试、构建并发布 GitHub Release。所有分支推送（包括 `main/master`）和 PR 仅运行测试检查，不打包、不上传可执行程序、不发布版本；以 `v` 开头但格式非法的 tag 会被校验拒绝。

```bash
# 先将代码及流水线提交并推送到 GitHub，再创建版本 tag
git tag -a v1.2.3 -m "Release v1.2.3"
git push origin v1.2.3

# 预发布示例（自动标记为 GitHub Pre-release）
git tag -a v1.3.0-rc.1 -m "Release v1.3.0-rc.1"
git push origin v1.3.0-rc.1
```

正式版本使用 `v主版本.次版本.修订版本`，例如 `v1.2.3`；不使用 `v1.2` 或 `v01.2.3`。已发布版本不覆盖或移动 tag，修复后递增版本号。

每个 Release 包含以下文件（以 `v1.2.3` 为例）：

| 平台 | 发布文件 | 包内可执行程序 |
| --- | --- | --- |
| macOS Apple Silicon / ARM64 | `s3async_v1.2.3_darwin_arm64.tar.gz` | `s3async` |
| Linux AMD64 | `s3async_v1.2.3_linux_amd64.tar.gz` | `s3async` |
| Windows AMD64 | `s3async_v1.2.3_windows_amd64.zip` | `s3async.exe` |
| SHA256 校验和 | `SHA256SUMS.txt` | — |

压缩包同时附带 README 和 `config.example.yaml`。解压后通过 `./s3async version`（Windows：`.\s3async.exe version`）检查版本，输出与 tag 一致。Linux 下可将三个压缩包与校验文件放在同一目录执行 `sha256sum -c SHA256SUMS.txt` 验证完整性。

三个平台分别使用原生 runner 并启用 CGO，所有平台测试及 Linux race 检查通过后才发布。Linux 产物在 Ubuntu 22.04 上构建，面向 glibc 2.35+ 环境，不是适用于 Alpine/musl 的静态程序。macOS 产物未做 Apple 签名和公证。

仓库需启用 GitHub Actions，并允许发布 job 使用 `GITHUB_TOKEN` 的 `contents: write` 权限；无需额外配置 PAT 或云服务密钥。下载地址为仓库的 [Releases 页面](https://github.com/jasperbigsum-commits/s3async/releases)。上传失败时可重跑流水线完成草稿发布，已公开发布的版本则拒绝覆盖。

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



## 后续计划 / TODOs
- convert detached worker launch into a first-class long-running daemon/service install mode
- multipart upload and resume
- retry jitter and selective retry policies
- richer CLI integration tests for daemon/task observability flows
