#!/usr/bin/env bash
set -euo pipefail

# build-linux.sh
# Usage:
#   ./build-linux.sh [--out name] [--target linux|windows]
# Examples:
#   ./build-linux.sh                    # builds s3async (linux)
#   ./build-linux.sh --out myapp        # builds myapp (linux)
#   ./build-linux.sh --target windows --out s3async  # cross-builds s3async.exe (Windows)

TARGET=linux
OUT=s3async

while [[ $# -gt 0 ]]; do
  case "$1" in
    --target|-t)
      TARGET=$2
      shift 2
      ;;
    --out|-o)
      OUT=$2
      shift 2
      ;;
    *)
      echo "Unknown option: $1"
      echo "Usage: $0 [--out name] [--target linux|windows]"
      exit 1
      ;;
  esac
done

if [[ "$TARGET" != "linux" && "$TARGET" != "windows" ]]; then
  echo "Invalid target: $TARGET"
  exit 1
fi

DIST_DIR="dist"
mkdir -p "$DIST_DIR"

if [[ "$TARGET" == "linux" ]]; then
  export CC=${CC:-gcc}
  export CGO_ENABLED=1
  export GOOS=linux
  export GOARCH=amd64
  EXE_OUTPUT="${DIST_DIR}/$OUT"
else
  if ! command -v x86_64-w64-mingw32-gcc >/dev/null 2>&1; then
    echo "Error: x86_64-w64-mingw32-gcc is required for Windows target cross-build"
    exit 1
  fi
  export CC=x86_64-w64-mingw32-gcc
  export CGO_ENABLED=1
  export GOOS=windows
  export GOARCH=amd64
  EXE_OUTPUT="${DIST_DIR}/${OUT%.exe}.exe"
fi

echo "Building target=$TARGET to $EXE_OUTPUT"
go build -ldflags "-s -w" -o "$EXE_OUTPUT" .
if [[ $? -ne 0 ]]; then
  echo "go build failed"
  exit 1
fi

echo "Built: $EXE_OUTPUT"
echo "Done."
