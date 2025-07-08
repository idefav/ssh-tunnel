# 多平台服务配置指南

本文档说明如何在不同操作系统上将SSH隧道程序配置为系统服务，以支持重启功能。

## Windows 系统

### 安装服务
```cmd
# 使用程序自带的服务管理功能
ssh-tunnel.exe install

# 或者使用service目录下的专用服务程序
cd service
ssh-tunnel-svc-windows-amd64.exe install
```

### 服务管理
```cmd
# 启动服务
sc start SSHTunnelService
# 或
net start SSHTunnelService

# 停止服务
sc stop SSHTunnelService
# 或  
net stop SSHTunnelService

# 查看服务状态
sc query SSHTunnelService

# 卸载服务
ssh-tunnel.exe uninstall
```

### 配置文件位置
- 默认路径: `C:\ssh-tunnel\.ssh-tunnel\config.properties`
- 可通过 `--config` 参数指定

## macOS 系统

### 创建 LaunchDaemon
创建文件 `/Library/LaunchDaemons/com.idefav.ssh-tunnel.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.idefav.ssh-tunnel</string>
    
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/ssh-tunnel</string>
        <string>--config=/etc/ssh-tunnel/config.properties</string>
    </array>
    
    <key>RunAtLoad</key>
    <true/>
    
    <key>KeepAlive</key>
    <true/>
    
    <key>StandardOutPath</key>
    <string>/var/log/ssh-tunnel.log</string>
    
    <key>StandardErrorPath</key>
    <string>/var/log/ssh-tunnel.error.log</string>
    
    <key>WorkingDirectory</key>
    <string>/usr/local/bin</string>
</dict>
</plist>
```

### 服务管理
```bash
# 加载服务
sudo launchctl load /Library/LaunchDaemons/com.idefav.ssh-tunnel.plist

# 启动服务
sudo launchctl start com.idefav.ssh-tunnel

# 停止服务
sudo launchctl stop com.idefav.ssh-tunnel

# 卸载服务
sudo launchctl unload /Library/LaunchDaemons/com.idefav.ssh-tunnel.plist

# 查看服务状态
launchctl list | grep ssh-tunnel
```

### 配置文件位置
- 推荐路径: `/etc/ssh-tunnel/config.properties`
- 用户路径: `~/.ssh-tunnel/config.properties`

## Linux 系统

### systemd (现代Linux发行版)

创建文件 `/etc/systemd/system/ssh-tunnel.service`:

```ini
[Unit]
Description=SSH Tunnel Service
After=network.target

[Service]
Type=simple
User=ssh-tunnel
Group=ssh-tunnel
ExecStart=/usr/local/bin/ssh-tunnel --config=/etc/ssh-tunnel/config.properties
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

#### 服务管理
```bash
# 重新加载systemd配置
sudo systemctl daemon-reload

# 启用服务（开机自启）
sudo systemctl enable ssh-tunnel

# 启动服务
sudo systemctl start ssh-tunnel

# 停止服务
sudo systemctl stop ssh-tunnel

# 重启服务
sudo systemctl restart ssh-tunnel

# 查看服务状态
sudo systemctl status ssh-tunnel

# 查看日志
sudo journalctl -u ssh-tunnel -f
```

### SysV init (传统Linux系统)

创建文件 `/etc/init.d/ssh-tunnel`:

```bash
#!/bin/bash
# ssh-tunnel        SSH Tunnel Service
# chkconfig: 35 99 99
# description: SSH Tunnel Service
#

. /etc/rc.d/init.d/functions

USER="ssh-tunnel"
DAEMON="ssh-tunnel"
ROOT_DIR="/usr/local/bin"

SERVER="$ROOT_DIR/$DAEMON"
LOCK_FILE="/var/lock/subsys/ssh-tunnel"

do_start() {
    if [ ! -f "$LOCK_FILE" ] ; then
        echo -n "Starting $DAEMON: "
        runuser -l "$USER" -c "$SERVER --config=/etc/ssh-tunnel/config.properties" && echo_success || echo_failure
        RETVAL=$?
        echo
        [ $RETVAL -eq 0 ] && touch $LOCK_FILE
    else
        echo "$DAEMON is locked."
    fi
}
do_stop() {
    echo -n $"Shutting down $DAEMON: "
    pid=`ps -aefw | grep "$DAEMON" | grep -v " grep " | awk '{print $2}'`
    kill -9 $pid > /dev/null 2>&1
    [ $? -eq 0 ] && echo_success || echo_failure
    RETVAL=$?
    echo
    [ $RETVAL -eq 0 ] && rm -f $LOCK_FILE
}

case "$1" in
    start)
        do_start
        ;;
    stop)
        do_stop
        ;;
    restart)
        do_stop
        do_start
        ;;
    *)
        echo "Usage: $0 {start|stop|restart}"
        RETVAL=1
esac

exit $RETVAL
```

#### 服务管理
```bash
# 设置可执行权限
sudo chmod +x /etc/init.d/ssh-tunnel

# 启用服务（开机自启）
sudo chkconfig ssh-tunnel on

# 启动服务
sudo service ssh-tunnel start

# 停止服务
sudo service ssh-tunnel stop

# 重启服务
sudo service ssh-tunnel restart

# 查看服务状态
sudo service ssh-tunnel status
```

## 用户和权限设置

### 创建专用用户（推荐）
```bash
# Linux/macOS
sudo useradd -r -s /bin/false ssh-tunnel
sudo mkdir -p /etc/ssh-tunnel
sudo chown ssh-tunnel:ssh-tunnel /etc/ssh-tunnel

# 设置SSH密钥权限
sudo chmod 600 /etc/ssh-tunnel/id_rsa
sudo chown ssh-tunnel:ssh-tunnel /etc/ssh-tunnel/id_rsa
```

## 配置文件示例

### config.properties
```properties
# 服务器配置
server.ip=your-server-ip
server.ssh.port=22
user=your-username

# SSH配置
ssh.path.private_key=/etc/ssh-tunnel/id_rsa
ssh.path.known_hosts=/etc/ssh-tunnel/known_hosts

# 本地代理配置
local.addr=127.0.0.1:1080
http.local.addr=127.0.0.1:8080

# 功能开关
socks5.enable=true
http.enable=true
http.over.ssh.enable=true

# 管理界面
admin.enable=true
admin.addr=127.0.0.1:1083

# 日志配置
log.file.path=/var/log/ssh-tunnel.log
```

## 验证服务模式

在配置页面访问 `http://localhost:1083/view/app/config`，如果程序正确配置为服务模式：
- 会显示"重启服务"按钮
- 按钮下方会显示"程序作为系统服务运行，支持在线重启"

如果是直接运行模式：
- 不会显示"重启服务"按钮  
- 会显示"程序直接运行模式，配置更改后需要手动重启程序"

## 故障排除

### 常见问题
1. **权限问题**: 确保服务用户有读取配置文件和SSH密钥的权限
2. **网络问题**: 检查防火墙设置，确保相关端口开放
3. **SSH密钥问题**: 确保SSH密钥格式正确且有正确的权限
4. **配置文件问题**: 检查配置文件路径和格式

### 日志查看
- Windows: 事件查看器 或 配置文件中指定的日志文件
- macOS: `/var/log/ssh-tunnel.log` 或 `Console.app`
- Linux: `journalctl -u ssh-tunnel` 或 配置文件中指定的日志文件
