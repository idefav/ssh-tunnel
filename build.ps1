# PowerShell构建脚本 - Windows版本的build.sh

# 确保bin目录存在
if (-not (Test-Path -Path "bin")) {
    New-Item -Path "bin" -ItemType Directory
}

# Windows AMD64
$env:GOOS = "windows"
$env:GOARCH = "amd64"
Write-Host "编译 Windows AMD64 版本..."
go build -o bin\ssh-tunnel-amd64.exe

# Windows 386
$env:GOOS = "windows"
$env:GOARCH = "386"
Write-Host "编译 Windows 386 版本..."
go build -o bin\ssh-tunnel-386.exe

# macOS AMD64
$env:GOOS = "darwin"
$env:GOARCH = "amd64"
Write-Host "编译 macOS AMD64 版本..."
go build -o bin\ssh-tunnel-amd64-darwin

# Linux AMD64
$env:GOOS = "linux"
$env:GOARCH = "amd64"
Write-Host "编译 Linux AMD64 版本..."
go build -o bin\ssh-tunnel-amd64-linux

# macOS ARM64
$env:GOOS = "darwin"
$env:GOARCH = "arm64"
Write-Host "编译 macOS ARM64 版本..."
go build -o bin\ssh-tunnel-arm64-darwin

# Windows服务版本
$env:GOOS = "windows"
$env:GOARCH = "386"
Write-Host "编译 Windows服务 版本..."
go build -o bin\ssh-tunnel-winsvc.exe .\win_service\main

Write-Host "所有版本编译完成！"
