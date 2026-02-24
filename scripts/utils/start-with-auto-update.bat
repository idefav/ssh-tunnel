@echo off
echo 启动SSH隧道服务（带自动更新功能）
echo =====================================

rem 设置配置文件路径
set CONFIG_FILE=test-config.properties

rem 检查配置文件是否存在
if not exist %CONFIG_FILE% (
    echo 错误：配置文件 %CONFIG_FILE% 不存在
    echo 请先创建配置文件或使用示例配置文件
    pause
    exit /b 1
)

echo 使用配置文件: %CONFIG_FILE%
echo 管理面板地址: http://localhost:1083
echo 版本管理页面: http://localhost:1083/view/version
echo.

rem 启动服务
echo 正在启动服务...
ssh-tunnel-new.exe --admin.enable=true --admin.address=:1083

pause
