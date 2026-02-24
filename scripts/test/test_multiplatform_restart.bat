@echo off
setlocal enabledelayedexpansion

REM 多平台服务重启测试脚本 - Windows版本
REM 用于验证Windows系统的服务重启命令

echo === Windows服务重启测试 ===
echo.

echo 检测到操作系统: Windows
echo 服务名称: SSHTunnelService
echo.

REM 检查sc命令
echo 1. 测试sc命令...
sc query >nul 2>&1
if !errorlevel! equ 0 (
    echo    ✅ sc命令可用
    
    REM 测试查看服务状态
    echo    测试命令: sc query SSHTunnelService
    sc query SSHTunnelService >nul 2>&1
    if !errorlevel! equ 0 (
        echo    ✅ 服务存在且可查询
        sc query SSHTunnelService | findstr "STATE"
    ) else (
        echo    ⚠️  服务未找到或未运行
    )
) else (
    echo    ❌ sc命令不可用
)

echo.

REM 检查net命令
echo 2. 测试net命令...
net help >nul 2>&1
if !errorlevel! equ 0 (
    echo    ✅ net命令可用
    
    REM 测试查看服务列表
    echo    测试命令: net start (查找SSH相关服务)
    net start | findstr /i "ssh" >nul 2>&1
    if !errorlevel! equ 0 (
        echo    ✅ 找到SSH相关服务
        net start | findstr /i "ssh"
    ) else (
        echo    ⚠️  未找到SSH相关服务
    )
) else (
    echo    ❌ net命令不可用
)

echo.

REM 检查PowerShell Get-Service
echo 3. 测试PowerShell Get-Service...
powershell -Command "Get-Service" >nul 2>&1
if !errorlevel! equ 0 (
    echo    ✅ PowerShell Get-Service可用
    
    REM 测试查看服务状态
    echo    测试命令: Get-Service SSHTunnelService
    powershell -Command "Get-Service SSHTunnelService" >nul 2>&1
    if !errorlevel! equ 0 (
        echo    ✅ 服务存在
        powershell -Command "Get-Service SSHTunnelService | Select-Object Name,Status"
    ) else (
        echo    ⚠️  服务未找到
    )
) else (
    echo    ❌ PowerShell Get-Service不可用
)

echo.
echo === 测试完成 ===
echo 注意: 这只是命令可用性测试，未执行实际的服务重启操作
echo.
echo 如果服务存在，重启命令将是:
echo   sc stop SSHTunnelService
echo   sc start SSHTunnelService
echo 或者:
echo   net stop SSHTunnelService  
echo   net start SSHTunnelService
pause
