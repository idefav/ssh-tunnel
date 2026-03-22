[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$OutputEncoding = [System.Text.Encoding]::UTF8
chcp 65001 > $null

if (-not (Test-Path -Path "bin")) {
    New-Item -Path "bin" -ItemType Directory | Out-Null
}

$Version = if ($env:VERSION) { $env:VERSION } else { "dev" }
$BuildTime = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
$LdFlags = "-s -w -X 'ssh-tunnel/buildinfo.Version=$Version' -X 'ssh-tunnel/buildinfo.BuildTime=$BuildTime'"

function Build-Target {
    param(
        [string]$GoOS,
        [string]$GoArch,
        [string]$Output,
        [string]$Package = "."
    )

    $env:GOOS = $GoOS
    $env:GOARCH = $GoArch
    Write-Host "Building $Output ..."
    go build -ldflags $LdFlags -o $Output $Package
}

Build-Target -GoOS "windows" -GoArch "amd64" -Output "bin\ssh-tunnel-windows-amd64.exe"
Build-Target -GoOS "windows" -GoArch "386" -Output "bin\ssh-tunnel-windows-386.exe"
Build-Target -GoOS "windows" -GoArch "arm64" -Output "bin\ssh-tunnel-windows-arm64.exe"
Build-Target -GoOS "darwin" -GoArch "amd64" -Output "bin\ssh-tunnel-darwin-amd64"
Build-Target -GoOS "darwin" -GoArch "arm64" -Output "bin\ssh-tunnel-darwin-arm64"
Build-Target -GoOS "linux" -GoArch "amd64" -Output "bin\ssh-tunnel-linux-amd64"
Build-Target -GoOS "linux" -GoArch "arm64" -Output "bin\ssh-tunnel-linux-arm64"

Build-Target -GoOS "windows" -GoArch "386" -Output "bin\ssh-tunnel-svc-windows-386.exe" -Package ".\service\main"
Build-Target -GoOS "windows" -GoArch "amd64" -Output "bin\ssh-tunnel-svc-windows-amd64.exe" -Package ".\service\main"
Build-Target -GoOS "windows" -GoArch "arm64" -Output "bin\ssh-tunnel-svc-windows-arm64.exe" -Package ".\service\main"
Build-Target -GoOS "darwin" -GoArch "amd64" -Output "bin\ssh-tunnel-svc-darwin-amd64" -Package ".\service\main"
Build-Target -GoOS "darwin" -GoArch "arm64" -Output "bin\ssh-tunnel-svc-darwin-arm64" -Package ".\service\main"
Build-Target -GoOS "linux" -GoArch "amd64" -Output "bin\ssh-tunnel-svc-linux-amd64" -Package ".\service\main"
Build-Target -GoOS "linux" -GoArch "arm64" -Output "bin\ssh-tunnel-svc-linux-arm64" -Package ".\service\main"

$checksumTargets = @(
    "ssh-tunnel-windows-amd64.exe",
    "ssh-tunnel-windows-386.exe",
    "ssh-tunnel-windows-arm64.exe",
    "ssh-tunnel-darwin-amd64",
    "ssh-tunnel-darwin-arm64",
    "ssh-tunnel-linux-amd64",
    "ssh-tunnel-linux-arm64",
    "ssh-tunnel-svc-windows-386.exe",
    "ssh-tunnel-svc-windows-amd64.exe",
    "ssh-tunnel-svc-windows-arm64.exe",
    "ssh-tunnel-svc-darwin-amd64",
    "ssh-tunnel-svc-darwin-arm64",
    "ssh-tunnel-svc-linux-amd64",
    "ssh-tunnel-svc-linux-arm64"
)

$checksumPath = "bin\\SHA256SUMS"
if (Test-Path $checksumPath) {
    Remove-Item $checksumPath -Force
}
foreach ($target in $checksumTargets) {
    $fullPath = Join-Path "bin" $target
    if (Test-Path $fullPath) {
        $hash = Get-FileHash $fullPath -Algorithm SHA256
        "$($hash.Hash.ToLower())  $target" | Out-File -FilePath $checksumPath -Append -Encoding ascii
    }
}

Remove-Item Env:GOOS -ErrorAction SilentlyContinue
Remove-Item Env:GOARCH -ErrorAction SilentlyContinue

Write-Host "All builds completed."
