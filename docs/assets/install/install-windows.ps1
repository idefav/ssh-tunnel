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

function Convert-ToUserPath {
    param([string]$PathValue)

    if ([string]::IsNullOrWhiteSpace($PathValue)) {
        return $PathValue
    }

    $homePath = [System.IO.Path]::GetFullPath($HOME)
    $targetPath = [System.IO.Path]::GetFullPath($PathValue)
    if ($targetPath.Equals($homePath, [System.StringComparison]::OrdinalIgnoreCase)) {
        return "~"
    }

    $homePrefix = $homePath.TrimEnd('\', '/') + [System.IO.Path]::DirectorySeparatorChar
    if ($targetPath.StartsWith($homePrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
        return "~\" + $targetPath.Substring($homePrefix.Length)
    }

    return $PathValue
}

function Get-SshKeySelection {
    $sshDir = Join-Path $HOME ".ssh"
    $keyNames = @("id_ed25519", "id_ed25519_sk", "id_ecdsa", "id_ecdsa_sk", "id_rsa", "id_dsa", "identity")
    $candidates = New-Object System.Collections.Generic.List[object]
    $seen = @{}

    if (-not (Test-Path $sshDir -PathType Container)) {
        return [pscustomobject]@{
            InputPath = "~\.ssh\id_rsa"
            PrivatePath = Expand-UserPath -PathValue "~\.ssh\id_rsa"
            PublicPath = $null
            HasKeys = $false
        }
    }

    foreach ($name in $keyNames) {
        $privatePath = Join-Path $sshDir $name
        $publicPath = "$privatePath.pub"
        if ((Test-Path $privatePath -PathType Leaf) -and (Test-Path $publicPath -PathType Leaf) -and -not $seen.ContainsKey($publicPath)) {
            $seen[$publicPath] = $true
            $candidates.Add([pscustomobject]@{
                InputPath = Convert-ToUserPath -PathValue $publicPath
                PrivatePath = $privatePath
                PublicPath = $publicPath
                HasKeys = $true
                AllowMissing = $false
            })
        }
    }

    Get-ChildItem -Path $sshDir -Filter "*.pub" -File -ErrorAction SilentlyContinue | Sort-Object Name | ForEach-Object {
        $publicPath = $_.FullName
        $privatePath = $publicPath.Substring(0, $publicPath.Length - 4)
        if ((Test-Path $privatePath -PathType Leaf) -and -not $seen.ContainsKey($publicPath)) {
            $seen[$publicPath] = $true
            $candidates.Add([pscustomobject]@{
                InputPath = Convert-ToUserPath -PathValue $publicPath
                PrivatePath = $privatePath
                PublicPath = $publicPath
                HasKeys = $true
                AllowMissing = $false
            })
        }
    }

    if ($candidates.Count -eq 0) {
        foreach ($name in $keyNames) {
            $privatePath = Join-Path $sshDir $name
            if ((Test-Path $privatePath -PathType Leaf) -and -not $seen.ContainsKey($privatePath)) {
                $seen[$privatePath] = $true
                $candidates.Add([pscustomobject]@{
                    InputPath = Convert-ToUserPath -PathValue $privatePath
                    PrivatePath = $privatePath
                    PublicPath = $null
                    HasKeys = $true
                    AllowMissing = $false
                })
            }
        }

        Get-ChildItem -Path $sshDir -File -ErrorAction SilentlyContinue | Sort-Object Name | ForEach-Object {
            $privatePath = $_.FullName
            if ($privatePath -match '\.pub$') {
                return
            }
            if ($_.Name -in @('authorized_keys', 'authorized_keys2', 'known_hosts', 'known_hosts.old', 'config')) {
                return
            }
            if (-not $seen.ContainsKey($privatePath)) {
                $seen[$privatePath] = $true
                $candidates.Add([pscustomobject]@{
                    InputPath = Convert-ToUserPath -PathValue $privatePath
                    PrivatePath = $privatePath
                    PublicPath = $null
                    HasKeys = $true
                    AllowMissing = $false
                })
            }
        }
    }

    if ($candidates.Count -gt 0) {
        Write-Note "Detected local SSH keys:"
        foreach ($candidate in $candidates) {
            if ($candidate.PublicPath) {
                Write-Note "  - $((Convert-ToUserPath -PathValue $candidate.PublicPath)) -> $((Convert-ToUserPath -PathValue $candidate.PrivatePath))"
            }
            else {
                Write-Note "  - $((Convert-ToUserPath -PathValue $candidate.PrivatePath))"
            }
        }
        return $candidates[0]
    }

    return [pscustomobject]@{
        InputPath = "~\.ssh\id_rsa"
        PrivatePath = Expand-UserPath -PathValue "~\.ssh\id_rsa"
        PublicPath = $null
        HasKeys = $false
    }
}

function Get-SshKeyComment {
    $hostName = [Environment]::MachineName
    if ([string]::IsNullOrWhiteSpace($hostName)) {
        $hostName = "local"
    }
    return "ssh-tunnel@$hostName"
}

function New-SshKeySelectionAuto {
    $sshKeygen = Get-Command ssh-keygen -ErrorAction SilentlyContinue
    if (-not $sshKeygen) {
        Fail "ssh-keygen is required to generate a new SSH key pair."
    }

    while ($true) {
        $pathInput = Read-Default -Prompt "Enter new SSH private key path" -DefaultValue "~\.ssh\id_ed25519"
        $privateKeyPath = Expand-UserPath -PathValue $pathInput
        $publicKeyPath = "$privateKeyPath.pub"
        if ((Test-Path $privateKeyPath) -or (Test-Path $publicKeyPath)) {
            Write-Note "Target key path already exists: $(Convert-ToUserPath -PathValue $privateKeyPath)"
            continue
        }
        break
    }

    $comment = Read-Default -Prompt "Enter SSH key comment" -DefaultValue (Get-SshKeyComment)

    if ($DryRun) {
        Write-Note "Dry run: would generate a new SSH key pair at $(Convert-ToUserPath -PathValue $privateKeyPath)"
        return [pscustomobject]@{
            InputPath = Convert-ToUserPath -PathValue $publicKeyPath
            PrivatePath = $privateKeyPath
            PublicPath = $publicKeyPath
            HasKeys = $true
            AllowMissing = $true
        }
    }

    $parentDir = Split-Path -Path $privateKeyPath -Parent
    if (-not (Test-Path $parentDir -PathType Container)) {
        New-Item -ItemType Directory -Path $parentDir -Force | Out-Null
    }

    & $sshKeygen.Source -t ed25519 -f $privateKeyPath -N "" -C $comment
    if ($LASTEXITCODE -ne 0) {
        Fail "ssh-keygen failed to create a new SSH key pair."
    }

    Write-Note "Generated SSH key pair: $(Convert-ToUserPath -PathValue $privateKeyPath)"
    return [pscustomobject]@{
        InputPath = Convert-ToUserPath -PathValue $publicKeyPath
        PrivatePath = $privateKeyPath
        PublicPath = $publicKeyPath
        HasKeys = $true
        AllowMissing = $false
    }
}

function New-SshKeySelectionInteractive {
    $sshKeygen = Get-Command ssh-keygen -ErrorAction SilentlyContinue
    if (-not $sshKeygen) {
        Fail "ssh-keygen is required to generate a new SSH key pair."
    }

    if ($DryRun) {
        Write-Note "Dry run: would start interactive ssh-keygen and then continue with the generated key."
        return [pscustomobject]@{
            InputPath = "~\.ssh\id_ed25519.pub"
            PrivatePath = Expand-UserPath -PathValue "~\.ssh\id_ed25519"
            PublicPath = Expand-UserPath -PathValue "~\.ssh\id_ed25519.pub"
            HasKeys = $true
            AllowMissing = $true
        }
    }

    Write-Note "Starting interactive ssh-keygen. Generate a key pair, then return to continue installation."
    & $sshKeygen.Source
    if ($LASTEXITCODE -ne 0) {
        Fail "Interactive ssh-keygen did not complete successfully."
    }

    return Get-SshKeySelection
}

function Ensure-LocalSshKeySelection {
    $selection = Get-SshKeySelection
    if ($selection.HasKeys) {
        return $selection
    }

    Write-Note "No local SSH key pairs were found in $(Convert-ToUserPath -PathValue (Join-Path $HOME '.ssh'))."
    while ($true) {
        $mode = Read-Default -Prompt "Generate a new SSH key pair now? (a=auto, i=interactive, m=manual)" -DefaultValue "a"
        switch -Regex ($mode) {
            '^(a|auto)$' {
                return New-SshKeySelectionAuto
            }
            '^(i|interactive)$' {
                $selection = New-SshKeySelectionInteractive
                if ($selection.HasKeys) {
                    return $selection
                }
                Write-Note "No usable SSH key pair was found after generation. Please try again."
            }
            '^(m|manual)$' {
                Write-Note "Manual mode selected. Enter an existing SSH key path to continue."
                return [pscustomobject]@{
                    InputPath = "~\.ssh\id_ed25519"
                    PrivatePath = Expand-UserPath -PathValue "~\.ssh\id_ed25519"
                    PublicPath = $null
                    HasKeys = $false
                    AllowMissing = $false
                }
            }
            default {
                Write-Note "Please answer a, i, or m."
            }
        }
    }
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

function Resolve-SshPublicKeyPath {
    param(
        [string]$PrivatePath,
        [string]$PublicPath
    )

    if (-not [string]::IsNullOrWhiteSpace($PublicPath) -and (Test-Path $PublicPath -PathType Leaf)) {
        return $PublicPath
    }

    $derivedPublicPath = "$PrivatePath.pub"
    if (Test-Path $derivedPublicPath -PathType Leaf) {
        return $derivedPublicPath
    }

    $sshKeygen = Get-Command ssh-keygen -ErrorAction SilentlyContinue
    if (-not $sshKeygen) {
        return $null
    }

    $generatedPublicPath = Join-Path $env:TEMP ("ssh-tunnel-authorized-key-" + [guid]::NewGuid().ToString('N') + ".pub")
    $output = & $sshKeygen.Source -y -f $PrivatePath 2>$null
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace(($output | Out-String))) {
        if (Test-Path $generatedPublicPath) {
            Remove-Item -Path $generatedPublicPath -Force
        }
        return $null
    }

    Set-Content -Path $generatedPublicPath -Value (($output | Out-String).Trim()) -Encoding ascii
    return $generatedPublicPath
}

function Test-PasswordlessSsh {
    param(
        [string]$ServerIp,
        [int]$ServerPort,
        [string]$LoginUser,
        [string]$PrivateKeyPath
    )

    $sshCommand = Get-Command ssh -ErrorAction SilentlyContinue
    if (-not $sshCommand) {
        Fail "OpenSSH client (ssh) is required to verify passwordless login."
    }

    & $sshCommand.Source -p $ServerPort -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new -o LogLevel=ERROR -i $PrivateKeyPath "$LoginUser@$ServerIp" exit *> $null
    return $LASTEXITCODE -eq 0
}

function Invoke-PasswordlessSshSetup {
    param(
        [string]$ServerIp,
        [int]$ServerPort,
        [string]$LoginUser,
        [string]$PrivateKeyPath,
        [string]$PublicKeyPath
    )

    $sshCommand = Get-Command ssh -ErrorAction SilentlyContinue
    if (-not $sshCommand) {
        Fail "OpenSSH client (ssh) is required to configure passwordless login."
    }

    $resolvedPublicKeyPath = Resolve-SshPublicKeyPath -PrivatePath $PrivateKeyPath -PublicPath $PublicKeyPath
    if ([string]::IsNullOrWhiteSpace($resolvedPublicKeyPath) -or -not (Test-Path $resolvedPublicKeyPath -PathType Leaf)) {
        Fail "Failed to locate or derive a public key for $PrivateKeyPath."
    }

    if ((Get-Item $resolvedPublicKeyPath).Length -le 0) {
        Fail "Public key is empty: $resolvedPublicKeyPath"
    }

    Write-Note "Passwordless SSH is not configured. Attempting automatic setup..."
    Write-Note "Public key source: $(Convert-ToUserPath -PathValue $resolvedPublicKeyPath)"

    $remoteCommand = 'umask 077; mkdir -p ~/.ssh && touch ~/.ssh/authorized_keys && chmod 700 ~/.ssh && chmod 600 ~/.ssh/authorized_keys && pub=$(cat) && (grep -qxF "$pub" ~/.ssh/authorized_keys || printf "%s\n" "$pub" >> ~/.ssh/authorized_keys)'
    try {
        (Get-Content -Path $resolvedPublicKeyPath -Raw) | & $sshCommand.Source -p $ServerPort -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new -o LogLevel=ERROR "$LoginUser@$ServerIp" $remoteCommand
        if ($LASTEXITCODE -ne 0) {
            Fail "Automatic passwordless SSH setup failed while updating authorized_keys."
        }
    }
    finally {
        if ($resolvedPublicKeyPath -ne $PublicKeyPath -and $resolvedPublicKeyPath -ne "$PrivateKeyPath.pub" -and (Test-Path $resolvedPublicKeyPath)) {
            Remove-Item -Path $resolvedPublicKeyPath -Force -ErrorAction SilentlyContinue
        }
    }

    return $resolvedPublicKeyPath
}

function Ensure-PasswordlessSsh {
    param(
        [switch]$DryRun,
        [string]$ServerIp,
        [int]$ServerPort,
        [string]$LoginUser,
        [string]$PrivateKeyPath,
        [string]$PublicKeyPath
    )

    if ($DryRun) {
        Write-Note "Dry run: would verify passwordless SSH for $LoginUser@$ServerIp with $(Convert-ToUserPath -PathValue $PrivateKeyPath) and auto-configure it if missing."
        return $PublicKeyPath
    }

    Write-Note "Checking passwordless SSH login..."
    if (Test-PasswordlessSsh -ServerIp $ServerIp -ServerPort $ServerPort -LoginUser $LoginUser -PrivateKeyPath $PrivateKeyPath) {
        Write-Note "Passwordless SSH is already configured."
        return $PublicKeyPath
    }

    $resolvedPublicKeyPath = Invoke-PasswordlessSshSetup -ServerIp $ServerIp -ServerPort $ServerPort -LoginUser $LoginUser -PrivateKeyPath $PrivateKeyPath -PublicKeyPath $PublicKeyPath
    Write-Note "Re-checking passwordless SSH login..."
    if (Test-PasswordlessSsh -ServerIp $ServerIp -ServerPort $ServerPort -LoginUser $LoginUser -PrivateKeyPath $PrivateKeyPath) {
        Write-Note "Passwordless SSH configured successfully."
        return $resolvedPublicKeyPath
    }

    Fail "Automatic passwordless SSH setup failed. Verify the remote account password, SSH password authentication, and ~/.ssh/authorized_keys permissions."
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

$keySelection = Ensure-LocalSshKeySelection
$defaultKeyInput = $keySelection.InputPath
$sshPublicKeyPath = $keySelection.PublicPath
$allowMissingSshKey = [bool]$keySelection.AllowMissing

while ($true) {
    $keyInput = Read-Default -Prompt "Enter SSH key path (public or private)" -DefaultValue $defaultKeyInput
    $resolvedKeyInput = Expand-UserPath -PathValue $keyInput
    if ($resolvedKeyInput.EndsWith('.pub', [System.StringComparison]::OrdinalIgnoreCase)) {
        $sshPublicKeyPath = $resolvedKeyInput
        $sshKeyPath = $resolvedKeyInput.Substring(0, $resolvedKeyInput.Length - 4)
        if ($DryRun -and $allowMissingSshKey) {
            break
        }
        if (-not (Test-Path $sshPublicKeyPath -PathType Leaf)) {
            Write-Note "Public key not found: $sshPublicKeyPath"
            continue
        }
        if (-not (Test-Path $sshKeyPath -PathType Leaf)) {
            Write-Note "Private key not found for public key: $sshKeyPath"
            continue
        }
        break
    }

    $sshKeyPath = $resolvedKeyInput
    if ($DryRun -and $allowMissingSshKey) {
        $sshPublicKeyPath = "$sshKeyPath.pub"
        break
    }
    if (Test-Path $sshKeyPath -PathType Leaf) {
        $derivedPublicKeyPath = "$sshKeyPath.pub"
        if (Test-Path $derivedPublicKeyPath -PathType Leaf) {
            $sshPublicKeyPath = $derivedPublicKeyPath
        }
        else {
            $sshPublicKeyPath = $null
        }
        break
    }
    Write-Note "SSH key not found: $sshKeyPath"
}

if ($sshPublicKeyPath) {
    Write-Note "Using SSH public key: $(Convert-ToUserPath -PathValue $sshPublicKeyPath)"
}
Write-Note "Using SSH private key: $(Convert-ToUserPath -PathValue $sshKeyPath)"

$sshPublicKeyPath = Ensure-PasswordlessSsh -DryRun:$DryRun -ServerIp $serverIp -ServerPort $serverPort -LoginUser $loginUser -PrivateKeyPath $sshKeyPath -PublicKeyPath $sshPublicKeyPath

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
    if ($sshPublicKeyPath) {
        Write-Note "  ssh.public_key_path.derived=$sshPublicKeyPath"
    }
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
