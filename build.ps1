# 设置脚本和控制台编码为UTF-8
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$OutputEncoding = [System.Text.Encoding]::UTF8
chcp 65001 > $null # 设置控制台代码页为UTF-8 (65001)

# PowerShell构建脚本 - Windows版本的build.sh

# 确保bin目录存在
if (-not (Test-Path -Path "bin")) {
    New-Item -Path "bin" -ItemType Directory
}

# windows
# Windows AMD64
$env:GOOS = "windows"
$env:GOARCH = "amd64"
Write-Host "编译 Windows AMD64 版本..."
go build -o bin\ssh-tunnel-windows-amd64.exe

# Windows 386
$env:GOOS = "windows"
$env:GOARCH = "386"
Write-Host "编译 Windows 386 版本..."
go build -o bin\ssh-tunnel-windows-386.exe

# Windows arm64
$env:GOOS = "windows"
$env:GOARCH = "arm64"
Write-Host "编译 Windows arm64 版本..."
go build -o bin\ssh-tunnel-windows-arm64.exe

# darwin MacOS

# macOS AMD64
$env:GOOS = "darwin"
$env:GOARCH = "amd64"
Write-Host "编译 macOS AMD64 版本..."
go build -o bin\ssh-tunnel-darwin-amd64

# macOS ARM64
$env:GOOS = "darwin"
$env:GOARCH = "arm64"
Write-Host "编译 macOS ARM64 版本..."
go build -o bin\ssh-tunnel-darwin-arm64

# linux

# Linux AMD64
$env:GOOS = "linux"
$env:GOARCH = "amd64"
Write-Host "编译 Linux AMD64 版本..."
go build -o bin\ssh-tunnel-linux-amd64

# Linux arm64
$env:GOOS = "linux"
$env:GOARCH = "arm64"
Write-Host "编译 Linux arm64 版本..."
go build -o bin\ssh-tunnel-linux-arm64

# Service版本

# Windows服务版本
# i386
$env:GOOS = "windows"
$env:GOARCH = "386"
Write-Host "编译 Windows服务 版本..."
go build -o bin\ssh-tunnel-svc-windows-386.exe .\service\main

# amd64
$env:GOOS = "windows"
$env:GOARCH = "amd64"
Write-Host "编译 Windows服务 版本..."
go build -o bin\ssh-tunnel-svc-windows-amd64.exe .\service\main

# arm64
$env:GOOS = "windows"
$env:GOARCH = "arm64"
Write-Host "编译 Windows服务 版本..."
go build -o bin\ssh-tunnel-svc-windows-arm64.exe .\service\main

# macOS服务版本
# amd64
$env:GOOS = "darwin"
$env:GOARCH = "amd64"
Write-Host "编译 macOS服务 版本..."
go build -o bin\ssh-tunnel-svc-darwin-amd64 .\service\main

# arm64
$env:GOOS = "darwin"
$env:GOARCH = "arm64"
Write-Host "编译 macOS服务 版本..."
go build -o bin\ssh-tunnel-svc-darwin-arm64 .\service\main

# linux服务版本
# amd64
$env:GOOS = "linux"
$env:GOARCH = "amd64"
Write-Host "编译 Linux服务 版本..."
go build -o bin\ssh-tunnel-svc-linux-amd64 .\service\main

# arm64
$env:GOOS = "linux"
$env:GOARCH = "arm64"
Write-Host "编译 Linux服务 版本..."
go build -o bin\ssh-tunnel-svc-linux-arm64 .\service\main


Write-Host "所有版本编译完成！"
