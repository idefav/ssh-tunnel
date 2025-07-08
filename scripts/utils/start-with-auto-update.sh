#!/bin/bash

echo "启动SSH隧道服务（带自动更新功能）"
echo "====================================="

# 设置配置文件路径
CONFIG_FILE="test-config.properties"

# 检查配置文件是否存在
if [ ! -f "$CONFIG_FILE" ]; then
    echo "错误：配置文件 $CONFIG_FILE 不存在"
    echo "请先创建配置文件或使用示例配置文件"
    exit 1
fi

echo "使用配置文件: $CONFIG_FILE"
echo "管理面板地址: http://localhost:1083"
echo "版本管理页面: http://localhost:1083/view/version"
echo ""

# 检查可执行文件
EXECUTABLE="./ssh-tunnel-new"
if [ ! -f "$EXECUTABLE" ]; then
    echo "正在编译程序..."
    go build -o ssh-tunnel-new .
    if [ $? -ne 0 ]; then
        echo "编译失败"
        exit 1
    fi
fi

# 启动服务
echo "正在启动服务..."
$EXECUTABLE --admin.enable=true --admin.address=:1083
