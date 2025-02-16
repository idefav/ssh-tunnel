#!/bin/bash

GOOS=windows GOARCH=amd64 go build -o bin/ssh-tunnel-amd64.exe

GOOS=windows GOARCH=386 go build -o bin/ssh-tunnel-386.exe

GOOS=darwin GOARCH=amd64 go build -o bin/ssh-tunnel-amd64-darwin

GOOS=linux GOARCH=amd64 go build -o bin/ssh-tunnel-amd64-linux

GOOS=darwin GOARCH=arm64 go build -o bin/ssh-tunnel-arm64-darwin

GOOS=windows GOARCH=386 go build  -o bin/ssh-tunnel-winsvc.exe .\win_service\main