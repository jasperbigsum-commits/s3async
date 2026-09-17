#!/usr/bin/env python3
"""Generate markdown release notes for a s3async GitHub Release.

Usage:
    python3 scripts/generate-release-notes.py \
        --version v0.2.7 [--prev v0.2.6] \
        [--dist dist] [--output RELEASE_NOTES.md] [--repo owner/name]

The script collects:
  1. downloadable artifacts in --dist (*.tar.gz / *.zip + SHA256SUMS.txt)
  2. commit subjects between --prev and --version (or HEAD when testing locally)
  3. a compare link to GitHub

It is intentionally dependency-free (stdlib only) so it runs on CI runners
without extra setup. All output is Markdown.
"""

from __future__ import annotations

import argparse
import datetime
import os
import subprocess
import sys


def run(cmd: list[str]) -> str:
    proc = subprocess.run(cmd, capture_output=True, text=True)
    if proc.returncode != 0:
        raise RuntimeError(f"command failed: {' '.join(cmd)}\n{proc.stderr.strip()}")
    return proc.stdout.strip()


def detect_repo() -> str:
    env = os.environ.get("GITHUB_REPOSITORY") or os.environ.get("GH_REPO")
    if env:
        return env
    try:
        url = run(["git", "remote", "get-url", "origin"])
    except RuntimeError:
        return "jasperbigsum-commits/s3async"
    url = url.strip()
    if url.endswith(".git"):
        url = url[:-4]
    # Support git@github.com:owner/repo and https://github.com/owner/repo
    if ":" in url and "@" in url:
        url = url.split(":", 1)[1]
    if "github.com/" in url:
        url = url.split("github.com/", 1)[1]
    return url.strip("/") or "jasperbigsum-commits/s3async"


def detect_prev_tag(version: str) -> str | None:
    """Return the newest tag other than version, or None (first release)."""
    try:
        tags = run(["git", "tag", "--sort=-v:refname"]).splitlines()
    except RuntimeError:
        return None
    tags = [t.strip() for t in tags if t.strip() and t.strip() != version]
    return tags[0] if tags else None


def collect_commits(prev: str | None, version: str) -> list[str]:
    if prev:
        rev_range = f"{prev}..HEAD"
    else:
        rev_range = "HEAD"
    try:
        out = run(
            [
                "git",
                "log",
                rev_range,
                "--pretty=format:- %s (%h by %an)",
                "--no-merges",
            ]
        )
    except RuntimeError as exc:
        return [f"- （读取提交记录失败：{exc}）"]
    lines = [line for line in out.splitlines() if line.strip()]
    # Drop the tagged commit itself noise when range includes everything.
    return lines[:100]


def collect_artifacts(dist: str) -> tuple[list[str], str]:
    artifacts: list[str] = []
    if os.path.isdir(dist):
        for name in sorted(os.listdir(dist)):
            if name.endswith(".tar.gz") or name.endswith(".zip"):
                artifacts.append(name)
    sums = ""
    sums_path = os.path.join(dist, "SHA256SUMS.txt")
    if os.path.isfile(sums_path):
        with open(sums_path, encoding="utf-8") as fh:
            sums = fh.read().strip()
    return artifacts, sums


def platform_label(filename: str) -> str:
    name = filename.lower()
    if "linux" in name:
        return "Linux AMD64"
    if "darwin" in name:
        return "macOS ARM64"
    if "windows" in name:
        return "Windows AMD64"
    return "通用"


def build_notes(
    version: str,
    prev: str | None,
    repo: str,
    artifacts: list[str],
    sums: str,
    commits: list[str],
    date: str,
) -> str:
    lines: list[str] = []
    lines.append(f"# s3async {version}")
    lines.append("")
    lines.append(f"> 自动生成于 {date}，纯 Go 静态构建，无需担心 glibc 版本。")
    lines.append("")
    lines.append("## 下载")
    lines.append("")
    if artifacts:
        lines.append("| 平台 | 文件 |")
        lines.append("| --- | --- |")
        for artifact in artifacts:
            lines.append(f"| {platform_label(artifact)} | `{artifact}` |")
        lines.append("")
        lines.append(
            "解压后包含 `s3async`（Windows 为 `s3async.exe`）、"
            "`README.md` 和 `config.example.yaml`。"
        )
    else:
        lines.append("（本次未找到产物包，请检查构建步骤是否成功上传。）")
    lines.append("")
    lines.append("## 校验")
    lines.append("")
    lines.append("```bash")
    lines.append("sha256sum -c SHA256SUMS.txt")
    lines.append("```")
    if sums:
        lines.append("")
        lines.append("```text")
        lines.append(sums)
        lines.append("```")
    lines.append("")
    if prev:
        lines.append(f"## 变更（{prev}...{version}）")
    else:
        lines.append("## 变更")
    lines.append("")
    if commits:
        lines.extend(commits)
    elif prev:
        lines.append(f"- {prev} 之后无新增提交（多为重新打包或流水线修复）。")
    else:
        lines.append("- 首次发布。")
    lines.append("")
    if prev:
        lines.append(
            f"完整对比：https://github.com/{repo}/compare/{prev}...{version}"
        )
        lines.append("")
    lines.append("## 使用")
    lines.append("")
    lines.append("```bash")
    lines.append("./s3async version  # 应输出 " + version)
    lines.append("# Windows：.\\s3async.exe version")
    lines.append("```")
    lines.append("")
    return "\n".join(lines)


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="生成 GitHub Release 说明文档")
    parser.add_argument("--version", required=True, help="本次版本号，如 v0.2.7")
    parser.add_argument("--prev", default=None, help="上一个版本号，不传则自动探测")
    parser.add_argument("--dist", default="dist", help="产物目录")
    parser.add_argument("--output", default="RELEASE_NOTES.md", help="输出的 markdown 路径")
    parser.add_argument("--repo", default=None, help="owner/name，不传则自动探测")
    return parser.parse_args(argv)


def main(argv: list[str]) -> int:
    args = parse_args(argv)
    version = args.version.strip()
    prev = args.prev.strip() if args.prev else detect_prev_tag(version)
    repo = args.repo or detect_repo()
    artifacts, sums = collect_artifacts(args.dist)
    commits = collect_commits(prev, version)
    date = datetime.date.today().isoformat()
    notes = build_notes(version, prev, repo, artifacts, sums, commits, date)
    with open(args.output, "w", encoding="utf-8") as fh:
        fh.write(notes)
    print(f"Wrote {args.output} (version={version} prev={prev or '-'} "
          f"artifacts={len(artifacts)} commits={len(commits)})")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
