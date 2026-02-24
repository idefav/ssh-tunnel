@echo off
setlocal enabledelayedexpansion

REM SSH隧道服务重启功能测试脚本
REM 用于验证重启API的响应

echo === SSH隧道服务重启功能测试 ===
echo.

REM 检查服务是否运行
echo 1. 检查服务状态...
curl -s http://localhost:1083/view/app/config >nul 2>&1
if !errorlevel! neq 0 (
    echo    ❌ 服务未运行或管理界面不可访问
    echo    请先启动SSH隧道服务
    pause
    exit /b 1
)
echo    ✅ 服务正在运行

REM 测试重启API
echo.
echo 2. 测试重启API...
curl -s -X POST http://localhost:1083/admin/service/restart -H "Content-Type: application/json" -o response.tmp
if !errorlevel! neq 0 (
    echo    ❌ API请求失败
    del response.tmp 2>nul
    pause
    exit /b 1
)

echo    ✅ API请求成功
echo    响应内容:
type response.tmp
del response.tmp

REM 等待处理完成
echo.
echo 3. 等待重启处理...
for /l %%i in (8,-1,1) do (
    echo    等待 %%i 秒...
    timeout /t 1 /nobreak >nul
)

REM 再次检查服务状态
echo.
echo 4. 检查重启后状态...
timeout /t 2 /nobreak >nul
curl -s http://localhost:1083/view/app/config >nul 2>&1
if !errorlevel! equ 0 (
    echo    ✅ 重启后服务正常运行
) else (
    echo    ⚠️  重启后服务状态未确定，可能正在重启中
)

echo.
echo === 测试完成 ===
echo 请检查应用日志以确认重启处理结果
pause
