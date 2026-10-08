#!/bin/bash
set -euo pipefail

SKIP_FRONTEND=false
RELEASE=false
for arg in "$@"; do
  case $arg in
    -f|--skip-frontend) SKIP_FRONTEND=true ;;
    -r|--release) RELEASE=true ;;
    *) echo "用法: ./build.sh [-f] [-r]  (-f 跳过前端编译, -r 同时构建全平台发布产物)"; exit 1 ;;
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

# ── 全平台发布产物（反馈 #48）────────────────────────────────────
# 跨平台产物此前靠手工构建，最后一次是 7/1，导致 9/2 的 schema 修复一直
# 没进 Windows 包——用户侧持续复现「expected 14 destination arguments in
# Scan」。这里把交叉编译固化进脚本，避免再出现「源码修了但分发没跟上」。
if [ "$RELEASE" = true ]; then
  echo ""
  echo "Building release artifacts (all platforms)..."
  build_target() {
    GOOS="$1" GOARCH="$2" CGO_ENABLED=0 go build -ldflags="$LDFLAGS" -o "$OUTDIR/$3" .
    echo "  → $3 ($1/$2)"
  }
  build_target darwin  arm64 aipmc-darwin-arm64
  build_target darwin  amd64 aipmc-darwin-amd64
  build_target linux   amd64 aipmc-linux-amd64
  build_target windows amd64 aipmc-windows-amd64.exe
fi

echo ""
echo "=== Build complete ==="
ls -lh "$OUTDIR/$CURRENT_OUTPUT"
