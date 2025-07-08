# SSH Tunnel 发布版本构建脚本
# 用于构建不同平台的发布版本

param(
    [string]$Version = "v1.0.0",
    [string]$OutputDir = ".\release"
)

# 创建输出目录
if (!(Test-Path $OutputDir)) {
    New-Item -ItemType Directory -Path $OutputDir -Force
}

# 设置构建时间
$BuildTime = Get-Date -Format "yyyy-MM-dd HH:mm:ss"

# 通用构建标志
$CommonFlags = @(
    "-ldflags",
    "-s -w -X 'main.Version=$Version' -X 'main.BuildTime=$BuildTime'"
)

# 构建目标平台
$Targets = @(
    @{OS="windows"; Arch="amd64"; Ext=".exe"},
    @{OS="windows"; Arch="386"; Ext=".exe"},
    @{OS="linux"; Arch="amd64"; Ext=""},
    @{OS="linux"; Arch="386"; Ext=""},
    @{OS="darwin"; Arch="amd64"; Ext=""},
    @{OS="darwin"; Arch="arm64"; Ext=""}
)

Write-Host "开始构建 SSH Tunnel $Version..." -ForegroundColor Green
Write-Host "构建时间: $BuildTime" -ForegroundColor Yellow
Write-Host "输出目录: $OutputDir" -ForegroundColor Yellow
Write-Host ""

foreach ($Target in $Targets) {
    $OutputName = "ssh-tunnel-$($Target.OS)-$($Target.Arch)$($Target.Ext)"
    $OutputPath = Join-Path $OutputDir $OutputName
    
    Write-Host "构建 $($Target.OS)/$($Target.Arch)..." -ForegroundColor Cyan
    
    # 设置环境变量
    $env:GOOS = $Target.OS
    $env:GOARCH = $Target.Arch
    $env:CGO_ENABLED = "0"
    
    # 执行构建
    try {
        & go build @CommonFlags -o $OutputPath main.go
        if ($LASTEXITCODE -eq 0) {
            $FileSize = (Get-Item $OutputPath).Length
            Write-Host "  ✓ 构建成功: $OutputName ($([math]::Round($FileSize/1MB, 2)) MB)" -ForegroundColor Green
        } else {
            Write-Host "  ✗ 构建失败: $OutputName" -ForegroundColor Red
        }
    } catch {
        Write-Host "  ✗ 构建错误: $($_.Exception.Message)" -ForegroundColor Red
    }
}

# 重置环境变量
Remove-Item Env:GOOS -ErrorAction SilentlyContinue
Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue

Write-Host ""
Write-Host "构建完成！输出文件位于: $OutputDir" -ForegroundColor Green

# 显示构建结果
Write-Host ""
Write-Host "构建结果:" -ForegroundColor Yellow
Get-ChildItem $OutputDir | ForEach-Object {
    $Size = [math]::Round($_.Length/1MB, 2)
    Write-Host "  $($_.Name) - $Size MB" -ForegroundColor White
}

# 创建校验和文件
Write-Host ""
Write-Host "生成 SHA256 校验和..." -ForegroundColor Cyan
$ChecksumFile = Join-Path $OutputDir "SHA256SUMS"
Get-ChildItem $OutputDir -File | Where-Object { $_.Name -ne "SHA256SUMS" } | ForEach-Object {
    $Hash = Get-FileHash $_.FullName -Algorithm SHA256
    "$($Hash.Hash.ToLower())  $($_.Name)" | Out-File -FilePath $ChecksumFile -Append -Encoding UTF8
}

Write-Host "  ✓ 校验和文件已生成: SHA256SUMS" -ForegroundColor Green

Write-Host ""
Write-Host "发布构建完成！" -ForegroundColor Green -BackgroundColor Black
