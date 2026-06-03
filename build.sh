#!/bin/bash
set -euo pipefail

echo "=== Building aipmc ==="
OUTDIR="./dist"
mkdir -p "$OUTDIR"

# Build frontend
if [ -f "./frontend/package.json" ]; then
  echo ""
  echo "Building frontend..."
  cd ./frontend && npm install --silent && npm run build && cd ..
  echo "Frontend built to frontend/dist/"
fi

# Requires Go 1.21+
echo ""
echo "Building for current platform..."
go build -ldflags="-s -w" -o "$OUTDIR/aipmc" .

echo ""
echo "Cross-compiling..."
GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o "$OUTDIR/aipmc-darwin-amd64" .
GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o "$OUTDIR/aipmc-darwin-arm64" .
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o "$OUTDIR/aipmc-linux-amd64" .
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o "$OUTDIR/aipmc-windows-amd64.exe" .

echo ""
echo "=== Build complete ==="
ls -lh "$OUTDIR/"
