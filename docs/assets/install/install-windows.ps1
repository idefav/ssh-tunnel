[CmdletBinding()]
param(
    [switch]$DryRun
)

$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$RepoOwner = "idefav"
$RepoName = "ssh-tunnel"
$ReleaseApiUrl = "https://api.github.com/repos/$RepoOwner/$RepoName/releases/latest"
$ReleaseDownloadBase = "https://github.com/$RepoOwner/$RepoName/releases/latest/download"

$ServiceName = "SSHTunnelService"
$InstallRoot = "C:\ssh-tunnel"
$ConfigDir = Join-Path $InstallRoot ".ssh-tunnel"
$ConfigPath = Join-Path $ConfigDir "config.properties"
$BinaryPath = Join-Path $InstallRoot "ssh-tunnel-svc.exe"

function Write-Note {
    param([string]$Message)
    Write-Host $Message
}

function Fail {
    param([string]$Message)
    throw $Message
}

function Read-Default {
    param(
        [string]$Prompt,
        [string]$DefaultValue
    )

    $suffix = if ([string]::IsNullOrWhiteSpace($DefaultValue)) { "" } else { " [$DefaultValue]" }
    $value = Read-Host "$Prompt$suffix"
    if ([string]::IsNullOrWhiteSpace($value)) {
        return $DefaultValue
    }
    return $value.Trim()
}

function Expand-UserPath {
    param([string]$PathValue)

    if ([string]::IsNullOrWhiteSpace($PathValue)) {
        return $PathValue
    }
    if ($PathValue -eq "~") {
        return $HOME
    }
    if ($PathValue.StartsWith('~\')) {
        return Join-Path $HOME $PathValue.Substring(2)
    }
    if ($PathValue.StartsWith("~/")) {
        return Join-Path $HOME $PathValue.Substring(2)
    }
    return [Environment]::ExpandEnvironmentVariables($PathValue)
}

function Test-IsAdministrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Get-PortInUse {
    param([int]$Port)

    if (Get-Command Get-NetTCPConnection -ErrorAction SilentlyContinue) {
        return [bool](Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue)
    }

    $matches = netstat -ano | Select-String -Pattern "[:\.]$Port\s+.*LISTENING"
    return $matches.Count -gt 0
}

function Read-Port {
    param(
        [string]$Label,
        [int]$DefaultPort
    )

    $port = $DefaultPort
    if (Get-PortInUse -Port $DefaultPort) {
        while ($true) {
            $candidate = Read-Default -Prompt "$Label port is already in use, enter a new port" -DefaultValue ([string]($DefaultPort + 1000))
            if ([int]::TryParse($candidate, [ref]$port) -and $port -ge 1 -and $port -le 65535) {
                if (-not (Get-PortInUse -Port $port)) {
                    return $port
                }
                Write-Note "Port $port is also in use."
            } else {
                Write-Note "Please enter a valid port between 1 and 65535."
            }
        }
    }
    return $port
}

function Get-AdminUrlFromConfig {
    param([string]$PathValue)

    if (Test-Path $PathValue) {
        $line = Get-Content $PathValue | Where-Object { $_ -match '^admin\.address=' } | Select-Object -Last 1
        if ($line) {
            $value = ($line -replace '^admin\.address=', '').Trim()
            if ($value -match ':(\d+)$') {
                return "http://127.0.0.1:$($Matches[1])/view/version"
            }
        }
    }
    return "http://127.0.0.1:1083/view/version"
}

function Get-ReleaseMetadata {
    return Invoke-RestMethod -Uri $ReleaseApiUrl -Method Get
}

function Get-ArchName {
    $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
    switch ($arch) {
        "x64" { return "amd64" }
        "x86" { return "386" }
        "arm64" { return "arm64" }
        default { Fail "Unsupported architecture: $arch" }
    }
}

function Test-ExistingInstall {
    if (Test-Path $ConfigPath) { return $true }
    if (Test-Path $BinaryPath) { return $true }
    if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) { return $true }
    return $false
}

$Release = Get-ReleaseMetadata
$ReleaseTag = $Release.tag_name
if ([string]::IsNullOrWhiteSpace($ReleaseTag)) {
    Fail "Failed to resolve latest release tag."
}

if (Test-ExistingInstall) {
    $adminUrl = Get-AdminUrlFromConfig -PathValue $ConfigPath
    Write-Note "Detected an existing installation."
    Write-Note "Use the management page to update instead of re-running the one-click installer:"
    Write-Note $adminUrl
    exit 0
}

if (-not $DryRun -and -not (Test-IsAdministrator)) {
    Fail "Please run this installer from an elevated PowerShell session."
}

$ArchName = Get-ArchName
$AssetName = "ssh-tunnel-svc-windows-$ArchName.exe"
$AssetUrl = "$ReleaseDownloadBase/$AssetName"
$ChecksumUrl = "$ReleaseDownloadBase/SHA256SUMS"

Write-Note "Installing SSH Tunnel $ReleaseTag"
Write-Note "Detected platform: windows/$ArchName"
Write-Note "Service mode: Windows Service"

$serverIp = Read-Default -Prompt "Enter SSH server IP or hostname" -DefaultValue ""
while ([string]::IsNullOrWhiteSpace($serverIp)) {
    Write-Note "Value cannot be empty."
    $serverIp = Read-Default -Prompt "Enter SSH server IP or hostname" -DefaultValue ""
}

while ($true) {
    $serverPortInput = Read-Default -Prompt "Enter SSH server port" -DefaultValue "22"
    if ([int]::TryParse($serverPortInput, [ref]$serverPort) -and $serverPort -ge 1 -and $serverPort -le 65535) {
        break
    }
    Write-Note "Please enter a valid port between 1 and 65535."
}

$loginUser = Read-Default -Prompt "Enter SSH login username" -DefaultValue "root"

while ($true) {
    $keyInput = Read-Default -Prompt "Enter SSH private key path" -DefaultValue "~\.ssh\id_rsa"
    $sshKeyPath = Expand-UserPath -PathValue $keyInput
    if (Test-Path $sshKeyPath) {
        break
    }
    Write-Note "Private key not found: $sshKeyPath"
}

while ($true) {
    $bindChoice = Read-Default -Prompt "Bind services to localhost only? (y/n)" -DefaultValue "y"
    switch -Regex ($bindChoice) {
        '^(y|yes)$' {
            $socksHost = "127.0.0.1"
            $httpHost = "127.0.0.1"
            $adminHost = "127.0.0.1"
            break
        }
        '^(n|no)$' {
            $socksHost = "0.0.0.0"
            $httpHost = "0.0.0.0"
            $adminHost = ""
            break
        }
        default {
            Write-Note "Please answer y or n."
            continue
        }
    }
    break
}

$socksPort = Read-Port -Label "SOCKS5 proxy" -DefaultPort 1081
$httpPort = Read-Port -Label "HTTP proxy" -DefaultPort 1082
$adminPort = Read-Port -Label "Admin UI" -DefaultPort 1083

$localAddress = "$socksHost`:$socksPort"
$httpLocalAddress = "$httpHost`:$httpPort"
$adminAddress = if ([string]::IsNullOrWhiteSpace($adminHost)) { ":$adminPort" } else { "$adminHost`:$adminPort" }
$adminUrl = "http://127.0.0.1:$adminPort/view/version"
$logFilePath = Join-Path $ConfigDir "console.log"
$domainFilePath = Join-Path $ConfigDir "domain.txt"

if ($DryRun) {
    Write-Note "Dry run only. No files will be written."
    Write-Note "Latest release: $ReleaseTag"
    Write-Note "Binary asset: $AssetName"
    Write-Note "Binary destination: $BinaryPath"
    Write-Note "Config destination: $ConfigPath"
    Write-Note "Generated config summary:"
    Write-Note "  server.ip=$serverIp"
    Write-Note "  server.ssh.port=$serverPort"
    Write-Note "  login.username=$loginUser"
    Write-Note "  ssh.private_key_path=$sshKeyPath"
    Write-Note "  local.address=$localAddress"
    Write-Note "  http.local.address=$httpLocalAddress"
    Write-Note "  admin.address=$adminAddress"
    Write-Note "  home.dir=$ConfigDir"
    Write-Note "  log.file.path=$logFilePath"
    exit 0
}

$tempDir = Join-Path $env:TEMP "ssh-tunnel-install-$([guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
try {
    $downloadedAsset = Join-Path $tempDir $AssetName
    $checksumPath = Join-Path $tempDir "SHA256SUMS"

    Write-Note "Downloading $AssetName..."
    Invoke-WebRequest -Uri $AssetUrl -OutFile $downloadedAsset
    Invoke-WebRequest -Uri $ChecksumUrl -OutFile $checksumPath

    $expectedHash = $null
    foreach ($line in Get-Content $checksumPath) {
        $parts = $line -split '\s+', 2
        if ($parts.Count -ge 2 -and $parts[1].TrimStart('*') -eq $AssetName) {
            $expectedHash = $parts[0].ToLowerInvariant()
            break
        }
    }
    if ([string]::IsNullOrWhiteSpace($expectedHash)) {
        Fail "Failed to resolve expected SHA256 for $AssetName."
    }

    $actualHash = (Get-FileHash -Path $downloadedAsset -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualHash -ne $expectedHash) {
        Fail "SHA256 verification failed."
    }

    New-Item -ItemType Directory -Path $InstallRoot -Force | Out-Null
    New-Item -ItemType Directory -Path $ConfigDir -Force | Out-Null
    Copy-Item -Path $downloadedAsset -Destination $BinaryPath -Force

    $configContent = @"
home.dir=$ConfigDir
server.ip=$serverIp
server.ssh.port=$serverPort
ssh.private_key_path=$sshKeyPath
login.username=$loginUser
local.address=$localAddress
http.local.address=$httpLocalAddress
http.enable=false
socks5.enable=true
http.over-ssh.enable=false
http.domain-filter.enable=false
http.domain-filter.file-path=$domainFilePath
admin.enable=true
admin.address=$adminAddress
retry.interval.sec=3
ssh.dial.timeout.sec=5
ssh.dest.dial.timeout.sec=3
ssh.keepalive.interval.sec=2
ssh.keepalive.count.max=2
ssh.reconnect.max.retries=20
ssh.reconnect.max.interval.sec=5
log.file.path=$logFilePath
auto-update.enabled=true
auto-update.owner=$RepoOwner
auto-update.repo=$RepoName
auto-update.current-version=$ReleaseTag
auto-update.check-interval=3600
"@

    Set-Content -Path $ConfigPath -Value $configContent -Encoding ascii
    if (-not (Test-Path $domainFilePath)) {
        Set-Content -Path $domainFilePath -Value "" -Encoding ascii
    }

    & $BinaryPath install "--config=$ConfigPath"
    Start-Service -Name $ServiceName

    Write-Note "Installation completed successfully."
    Write-Note "Admin UI: $adminUrl"
    Write-Note "Future upgrades should be done from the management page version screen."
}
finally {
    if (Test-Path $tempDir) {
        Remove-Item -Path $tempDir -Recurse -Force
    }
}
