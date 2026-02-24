#!/bin/bash

# 多平台服务重启测试脚本
# 用于验证不同操作系统的服务重启命令

echo "=== 多平台服务重启测试 ==="
echo

# 检测操作系统
OS=$(uname -s)
echo "检测到操作系统: $OS"

case $OS in
    "Darwin")
        echo "macOS 系统 - 测试 launchctl 命令"
        echo "服务名称: com.idefav.ssh-tunnel"
        
        # 检查 launchctl 是否可用
        if command -v launchctl &> /dev/null; then
            echo "✅ launchctl 命令可用"
            
            # 测试查看服务状态（不会实际操作）
            echo "测试命令: launchctl list | grep ssh-tunnel"
            launchctl list | grep ssh-tunnel || echo "服务未找到或未运行"
        else
            echo "❌ launchctl 命令不可用"
        fi
        ;;
        
    "Linux")
        echo "Linux 系统 - 测试服务管理命令"
        echo "服务名称: ssh-tunnel"
        
        # 检查 systemctl 是否可用
        if command -v systemctl &> /dev/null; then
            echo "✅ systemctl 命令可用 (systemd)"
            
            # 测试查看服务状态
            echo "测试命令: systemctl status ssh-tunnel"
            systemctl status ssh-tunnel 2>/dev/null || echo "服务未找到或未运行"
        else
            echo "⚠️  systemctl 不可用，检查传统 service 命令"
        fi
        
        # 检查传统 service 命令
        if command -v service &> /dev/null; then
            echo "✅ service 命令可用 (SysV init)"
            
            # 测试查看服务状态
            echo "测试命令: service ssh-tunnel status"
            service ssh-tunnel status 2>/dev/null || echo "服务未找到或未运行"
        else
            echo "❌ service 命令不可用"
        fi
        ;;
        
    "CYGWIN"*|"MINGW"*|"MSYS"*)
        echo "Windows 系统 (模拟环境) - 测试 Windows 服务命令"
        echo "服务名称: SSHTunnelService"
        
        # 检查 sc 命令
        if command -v sc &> /dev/null; then
            echo "✅ sc 命令可用"
            
            # 测试查看服务状态
            echo "测试命令: sc query SSHTunnelService"
            sc query SSHTunnelService 2>/dev/null || echo "服务未找到或未运行"
        else
            echo "❌ sc 命令不可用"
        fi
        
        # 检查 net 命令
        if command -v net &> /dev/null; then
            echo "✅ net 命令可用"
        else
            echo "❌ net 命令不可用"
        fi
        ;;
        
    *)
        echo "未知操作系统: $OS"
        echo "程序将使用通用重启方法（简单退出）"
        ;;
esac

echo
echo "=== 测试完成 ==="
echo "注意: 这只是命令可用性测试，未执行实际的服务重启操作"
