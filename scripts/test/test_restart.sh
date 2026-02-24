#!/bin/bash

# SSH隧道服务重启功能测试脚本
# 用于验证重启API的响应

echo "=== SSH隧道服务重启功能测试 ==="
echo

# 检查服务是否运行
echo "1. 检查服务状态..."
if ! curl -s http://localhost:1083/view/app/config > /dev/null 2>&1; then
    echo "   ❌ 服务未运行或管理界面不可访问"
    echo "   请先启动SSH隧道服务"
    exit 1
fi
echo "   ✅ 服务正在运行"

# 测试重启API
echo
echo "2. 测试重启API..."
response=$(curl -s -X POST http://localhost:1083/admin/service/restart \
    -H "Content-Type: application/json" \
    -w "%{http_code}")

http_code=${response: -3}
response_body=${response%???}

if [ "$http_code" = "200" ]; then
    echo "   ✅ API响应成功 (HTTP $http_code)"
    echo "   响应内容: $response_body"
else
    echo "   ❌ API响应失败 (HTTP $http_code)"
    echo "   响应内容: $response_body"
    exit 1
fi

# 等待处理完成
echo
echo "3. 等待重启处理..."
for i in {8..1}; do
    echo "   等待 $i 秒..."
    sleep 1
done

# 再次检查服务状态
echo
echo "4. 检查重启后状态..."
sleep 2
if curl -s http://localhost:1083/view/app/config > /dev/null 2>&1; then
    echo "   ✅ 重启后服务正常运行"
else
    echo "   ⚠️  重启后服务状态未确定，可能正在重启中"
fi

echo
echo "=== 测试完成 ==="
echo "请检查应用日志以确认重启处理结果"
