# SSH Tunnel Build Script
param(
    [string]$Version = "v1.1.0",
    [string]$OutputDir = ".\release"
)

# Create output directory
if (!(Test-Path $OutputDir)) {
    New-Item -ItemType Directory -Path $OutputDir -Force
}

$BuildTime = Get-Date -Format "yyyy-MM-dd HH:mm:ss"

Write-Host "Building SSH Tunnel $Version..." -ForegroundColor Green
Write-Host "Build Time: $BuildTime" -ForegroundColor Yellow
Write-Host "Output Dir: $OutputDir" -ForegroundColor Yellow
Write-Host ""

# Build Windows x64
Write-Host "Building Windows x64..." -ForegroundColor Cyan
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
go build -ldflags "-s -w -X 'main.Version=$Version' -X 'main.BuildTime=$BuildTime'" -o "$OutputDir\ssh-tunnel-windows-amd64.exe" main.go

# Build Linux x64
Write-Host "Building Linux x64..." -ForegroundColor Cyan
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -ldflags "-s -w -X 'main.Version=$Version' -X 'main.BuildTime=$BuildTime'" -o "$OutputDir\ssh-tunnel-linux-amd64" main.go

# Build macOS x64
Write-Host "Building macOS x64..." -ForegroundColor Cyan
$env:GOOS = "darwin"
$env:GOARCH = "amd64"
go build -ldflags "-s -w -X 'main.Version=$Version' -X 'main.BuildTime=$BuildTime'" -o "$OutputDir\ssh-tunnel-darwin-amd64" main.go

# Reset environment
Remove-Item Env:GOOS -ErrorAction SilentlyContinue
Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue

Write-Host ""
Write-Host "Build completed!" -ForegroundColor Green

# Show results
Get-ChildItem $OutputDir | ForEach-Object {
    $Size = [math]::Round($_.Length/1MB, 2)
    Write-Host "  $($_.Name) - $Size MB" -ForegroundColor White
}

# Generate checksums
Write-Host ""
Write-Host "Generating SHA256 checksums..." -ForegroundColor Cyan
$ChecksumFile = Join-Path $OutputDir "SHA256SUMS.txt"
Get-ChildItem $OutputDir -File | Where-Object { $_.Name -ne "SHA256SUMS.txt" } | ForEach-Object {
    $Hash = Get-FileHash $_.FullName -Algorithm SHA256
    "$($Hash.Hash.ToLower())  $($_.Name)" | Out-File -FilePath $ChecksumFile -Append -Encoding UTF8
}
Write-Host "  Checksums saved to: SHA256SUMS.txt" -ForegroundColor Green
