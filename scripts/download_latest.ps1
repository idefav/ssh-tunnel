# 下载最新版本的SSH Tunnel服务
# 该脚本会自动检测系统架构并下载对应的服务包

# 获取系统信息
$arch = [System.Environment]::GetEnvironmentVariable("PROCESSOR_ARCHITECTURE")
$is64Bit = [System.Environment]::Is64BitOperatingSystem

# 确定操作系统和架构
$os = "windows"
$goarch = ""

if ($arch -eq "ARM64") {
    $goarch = "arm64"
} elseif ($is64Bit) {
    $goarch = "amd64"
} else {
    $goarch = "386"
}

Write-Host "检测到系统: $os, 架构: $goarch"

# 创建临时目录用于下载
$tempDir = Join-Path $env:TEMP "ssh-tunnel-download"
if (!(Test-Path $tempDir)) {
    New-Item -ItemType Directory -Path $tempDir | Out-Null
}

# GitHub API URL 获取最新版本
$apiUrl = "https://api.github.com/repos/idefav/ssh-tunnel/releases/latest"
Write-Host "正在获取最新版本信息..."

try {
    # 使用TLS 1.2
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

    $releaseInfo = Invoke-RestMethod -Uri $apiUrl -Method Get
    $version = $releaseInfo.tag_name
    Write-Host "找到最新版本: $version"

    # 查找对应当前系统和架构的资产
    $assetName = "ssh-tunnel-svc-$os-$goarch"
    if ($os -eq "windows") {
        $assetName += ".exe"
    }

    $asset = $releaseInfo.assets | Where-Object { $_.name -eq $assetName }

    if ($asset) {
        $downloadUrl = $asset.browser_download_url
        $outputFile = Join-Path $tempDir $asset.name

        Write-Host "正在下载 $assetName..."
        Invoke-WebRequest -Uri $downloadUrl -OutFile $outputFile

        # 复制到bin目录
        $binDir = Join-Path $PSScriptRoot "bin"
        if (!(Test-Path $binDir)) {
            New-Item -ItemType Directory -Path $binDir | Out-Null
        }

        $destFile = Join-Path $binDir $asset.name
        Copy-Item -Path $outputFile -Destination $destFile -Force

        Write-Host "下载完成! 文件已保存到: $destFile"
    } else {
        Write-Host "错误: 未找到适合当前系统的安装包 ($assetName)" -ForegroundColor Red
    }
} catch {
    Write-Host "下载过程中发生错误: $_" -ForegroundColor Red
}

# 清理临时文件
if (Test-Path $tempDir) {
    Remove-Item -Path $tempDir -Recurse -Force
}
