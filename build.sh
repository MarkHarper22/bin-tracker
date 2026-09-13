#!/bin/bash
# Builds Bin Tracker for Mac (Intel + Apple Silicon) and Windows into dist/.
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v go >/dev/null 2>&1; then
  export PATH="$HOME/sdk/go/bin:$PATH"
fi

VERSION=$(sed -nE 's/^[[:space:]]*appVersion[[:space:]]*=[[:space:]]*"([^"]+)".*/\1/p' main.go)
echo "Building Bin Tracker $VERSION"

rm -rf dist
mkdir -p "dist/BinTracker-Mac" "dist/BinTracker-Windows"

export CGO_ENABLED=0
FLAGS=(-trimpath -ldflags "-s -w")

echo "  Mac (Intel)..."
GOOS=darwin GOARCH=amd64 go build "${FLAGS[@]}" -o dist/mac-amd64 .
echo "  Mac (Apple Silicon)..."
GOOS=darwin GOARCH=arm64 go build "${FLAGS[@]}" -o dist/mac-arm64 .
lipo -create -output "dist/BinTracker-Mac/BinTracker" dist/mac-amd64 dist/mac-arm64
rm dist/mac-amd64 dist/mac-arm64
codesign --force --sign - "dist/BinTracker-Mac/BinTracker" 2>/dev/null || true

echo "  Windows..."
GOOS=windows GOARCH=amd64 go build "${FLAGS[@]}" -o "dist/BinTracker-Windows/BinTracker.exe" .

cp "READ ME.txt" "dist/BinTracker-Mac/READ ME.txt"
cp "READ ME.txt" "dist/BinTracker-Windows/READ ME.txt"

(cd dist && zip -qr "BinTracker-$VERSION-Mac.zip" BinTracker-Mac && zip -qr "BinTracker-$VERSION-Windows.zip" BinTracker-Windows)

echo "  Docker source..."
mkdir -p dist/docker-src/bintracker
cp -R Dockerfile .dockerignore docker-compose.yml go.mod go.sum ./*.go web README.md dist/docker-src/bintracker/
(cd dist/docker-src && zip -qr "../BinTracker-$VERSION-Docker.zip" bintracker -x "*.DS_Store")
rm -rf dist/docker-src

echo "Done:"
ls -lh dist/*.zip
