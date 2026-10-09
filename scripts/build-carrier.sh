#!/bin/sh
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$root/plugin/native/cmd/aii-meaning-t3/dist}
mkdir -p "$out"
cd "$root/plugin/native"
build() {
  CGO_ENABLED=0 GOOS=$1 GOARCH=$2 GOFLAGS=-mod=vendor GOPROXY=off \
    go build -trimpath -buildvcs=false -ldflags='-s -w -buildid=' -o "$out/$3" ./cmd/aii-meaning-t3
}
build linux   amd64 aii-meaning-t3-linux-x86_64
build linux   arm64 aii-meaning-t3-linux-arm64
build darwin  arm64 aii-meaning-t3-macos-arm64
build windows amd64 aii-meaning-t3-windows-x86_64.exe
( cd "$out" && for f in aii-meaning-t3-*; do
    if command -v sha256sum >/dev/null 2>&1; then sha256sum "$f"; else shasum -a 256 "$f"; fi
  done )
go version
