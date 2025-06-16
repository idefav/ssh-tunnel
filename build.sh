#!/bin/bash

# windows
GOOS=windows GOARCH=amd64 go build -o bin/ssh-tunnel-windows-amd64.exe
GOOS=windows GOARCH=386 go build -o bin/ssh-tunnel-windows-386.exe
GOOS=windows GOARCH=arm64 go build -o bin/ssh-tunnel-windows-arm64.exe

# darwin
GOOS=darwin GOARCH=amd64 go build -o bin/ssh-tunnel-darwin-amd64
GOOS=darwin GOARCH=arm64 go build -o bin/ssh-tunnel-darwin-arm64

# linux
GOOS=linux GOARCH=amd64 go build -o bin/ssh-tunnel-linux-amd64
GOOS=linux GOARCH=arm64 go build -o bin/ssh-tunnel-linux-arm64

# build service

# windows service
GOOS=windows GOARCH=386 go build  -o bin/ssh-tunnel-svc-windows-386.exe ./service/main/main
GOOS=windows GOARCH=amd64 go build  -o bin/ssh-tunnel-svc-windows-amd64.exe ./service/main/main
GOOS=windows GOARCH=arm64 go build  -o bin/ssh-tunnel-svc-windows-arm64.exe ./service/main/main

# darwin service
GOOS=darwin GOARCH=amd64 go build -o bin/ssh-tunnel-svc-darwin-amd64 ./service/main/main
GOOS=darwin GOARCH=arm64 go build -o bin/ssh-tunnel-svc-darwin-arm64 ./service/main/main

# linux service
GOOS=linux GOARCH=amd64 go build -o bin/ssh-tunnel-svc-linux-amd64 ./service/main/main
GOOS=linux GOARCH=arm64 go build -o bin/ssh-tunnel-svc-linux-arm64 ./service/main/main