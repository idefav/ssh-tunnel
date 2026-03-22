#!/bin/bash

set -euo pipefail

VERSION="${VERSION:-dev}"
BUILD_TIME="${BUILD_TIME:-$(date -u '+%Y-%m-%d %H:%M:%S')}"
LDFLAGS="-s -w -X 'ssh-tunnel/buildinfo.Version=${VERSION}' -X 'ssh-tunnel/buildinfo.BuildTime=${BUILD_TIME}'"

# windows
GOOS=windows GOARCH=amd64 go build -ldflags "$LDFLAGS" -o bin/ssh-tunnel-windows-amd64.exe
GOOS=windows GOARCH=386 go build -ldflags "$LDFLAGS" -o bin/ssh-tunnel-windows-386.exe
GOOS=windows GOARCH=arm64 go build -ldflags "$LDFLAGS" -o bin/ssh-tunnel-windows-arm64.exe

# darwin
GOOS=darwin GOARCH=amd64 go build -ldflags "$LDFLAGS" -o bin/ssh-tunnel-darwin-amd64
GOOS=darwin GOARCH=arm64 go build -ldflags "$LDFLAGS" -o bin/ssh-tunnel-darwin-arm64

# linux
GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o bin/ssh-tunnel-linux-amd64
GOOS=linux GOARCH=arm64 go build -ldflags "$LDFLAGS" -o bin/ssh-tunnel-linux-arm64

# build service

# windows service
GOOS=windows GOARCH=386 go build -ldflags "$LDFLAGS" -o bin/ssh-tunnel-svc-windows-386.exe ./service/main
GOOS=windows GOARCH=amd64 go build -ldflags "$LDFLAGS" -o bin/ssh-tunnel-svc-windows-amd64.exe ./service/main
GOOS=windows GOARCH=arm64 go build -ldflags "$LDFLAGS" -o bin/ssh-tunnel-svc-windows-arm64.exe ./service/main

# darwin service
GOOS=darwin GOARCH=amd64 go build -ldflags "$LDFLAGS" -o bin/ssh-tunnel-svc-darwin-amd64 ./service/main
GOOS=darwin GOARCH=arm64 go build -ldflags "$LDFLAGS" -o bin/ssh-tunnel-svc-darwin-arm64 ./service/main

# linux service
GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o bin/ssh-tunnel-svc-linux-amd64 ./service/main
GOOS=linux GOARCH=arm64 go build -ldflags "$LDFLAGS" -o bin/ssh-tunnel-svc-linux-arm64 ./service/main

rm -f bin/SHA256SUMS
(
  cd bin
  sha256sum \
    ssh-tunnel-windows-amd64.exe \
    ssh-tunnel-windows-386.exe \
    ssh-tunnel-windows-arm64.exe \
    ssh-tunnel-darwin-amd64 \
    ssh-tunnel-darwin-arm64 \
    ssh-tunnel-linux-amd64 \
    ssh-tunnel-linux-arm64 \
    ssh-tunnel-svc-windows-386.exe \
    ssh-tunnel-svc-windows-amd64.exe \
    ssh-tunnel-svc-windows-arm64.exe \
    ssh-tunnel-svc-darwin-amd64 \
    ssh-tunnel-svc-darwin-arm64 \
    ssh-tunnel-svc-linux-amd64 \
    ssh-tunnel-svc-linux-arm64 \
    > SHA256SUMS
)
