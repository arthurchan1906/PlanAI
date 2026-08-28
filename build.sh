#!/bin/bash
set -euo pipefail

SKIP_FRONTEND=false
for arg in "$@"; do
  case $arg in
    -f|--skip-frontend) SKIP_FRONTEND=true ;;
    *) echo "用法: ./build.sh [-f]  (-f 跳过前端编译)"; exit 1 ;;
  esac
done

echo "=== Building aipmc ==="
OUTDIR="./dist"
mkdir -p "$OUTDIR"

# Build frontend
if [ "$SKIP_FRONTEND" = false ] && [ -f "./frontend/package.json" ]; then
  echo ""
  echo "Building frontend..."
  cd ./frontend && npm install --silent && npm run build && cd ..
  echo "Frontend built to frontend/dist/"
fi

# 当前平台
CURRENT_OS=$(go env GOOS)
CURRENT_OUTPUT="aipmc"
if [ "$CURRENT_OS" == "windows" ]; then
  CURRENT_OUTPUT="aipmc.exe"
fi

# 注入构建版本（git short sha）到日志 BOOT banner，用于把日志段映射回具体提交
LDFLAGS="-s -w -X aipmc/u.BuildVersion=$(git rev-parse --short HEAD 2>/dev/null || echo dev)"

# ── 当前平台编译（纯 Go）──────────────────────────────────────────
echo ""
echo "Building for current platform ($CURRENT_OS)..."

CGO_ENABLED=0 go build -ldflags="$LDFLAGS" -o "$OUTDIR/$CURRENT_OUTPUT" .
echo "  → pure-Go build (credentials: AES-256-GCM, no CGO/gmssl)"

echo ""
echo "=== Build complete ==="
ls -lh "$OUTDIR/$CURRENT_OUTPUT"
