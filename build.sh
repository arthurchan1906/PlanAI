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

# ── 当前平台编译（CGO + GmSSL）───────────────────────────────────────
echo ""
echo "Building for current platform ($CURRENT_OS)..."

GMSSL_DIR="$PWD/gmssl"
if [ -f "$GMSSL_DIR/include/gmssl/sm4.h" ]; then
  export CGO_ENABLED=1
  export CGO_CFLAGS="-I$GMSSL_DIR/include"
  export CGO_LDFLAGS="$GMSSL_DIR/lib/libgmssl.a"
  go build -ldflags="-s -w" -o "$OUTDIR/$CURRENT_OUTPUT" .
  echo "  → credentials (SM4-GCM) enabled (static)"
else
  echo "  → gmssl/ not found, CGO disabled (no credentials support)"
  CGO_ENABLED=0 go build -ldflags="-s -w" -o "$OUTDIR/$CURRENT_OUTPUT" .
fi

echo ""
echo "=== Build complete ==="
ls -lh "$OUTDIR/$CURRENT_OUTPUT"
