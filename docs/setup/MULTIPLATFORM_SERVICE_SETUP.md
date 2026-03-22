# 多平台服务安装说明

首次安装建议优先使用 GitHub Pages 单入口安装脚本：

```bash
curl -fsSL https://idefav.github.io/ssh-tunnel/install | sh
```

Windows PowerShell:

```powershell
irm https://idefav.github.io/ssh-tunnel/install | iex
```

一键安装只用于首次安装。后续升级请在管理页面版本页完成：

```text
http://127.0.0.1:1083/view/version
```

如果你需要手动部署服务，请保持下面的服务名、落盘路径和配置键不变，这样管理页的“重启服务”和“版本更新”才能正确识别当前实例。

## Windows

### 文件布局

- 二进制: `C:\ssh-tunnel\ssh-tunnel-svc.exe`
- 配置: `C:\ssh-tunnel\.ssh-tunnel\config.properties`
- 服务名: `SSHTunnelService`

### 安装服务

```powershell
C:\ssh-tunnel\ssh-tunnel-svc.exe install --config=C:\ssh-tunnel\.ssh-tunnel\config.properties
Start-Service SSHTunnelService
```

### 常用管理命令

```powershell
Start-Service SSHTunnelService
Stop-Service SSHTunnelService
Get-Service SSHTunnelService
```

## macOS

### 文件布局

- 二进制: `/usr/local/bin/ssh-tunnel`
- 配置: `/etc/ssh-tunnel/config.properties`
- LaunchDaemon: `/Library/LaunchDaemons/com.idefav.ssh-tunnel.plist`
- 服务标识: `com.idefav.ssh-tunnel`

### LaunchDaemon 示例

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

    <key>WorkingDirectory</key>
    <string>/usr/local/bin</string>

    <key>StandardOutPath</key>
    <string>/usr/local/var/log/ssh-tunnel.log</string>

    <key>StandardErrorPath</key>
    <string>/usr/local/var/log/ssh-tunnel.error.log</string>
</dict>
</plist>
```

### 常用管理命令

```bash
sudo launchctl load /Library/LaunchDaemons/com.idefav.ssh-tunnel.plist
sudo launchctl start com.idefav.ssh-tunnel
sudo launchctl stop com.idefav.ssh-tunnel
sudo launchctl unload /Library/LaunchDaemons/com.idefav.ssh-tunnel.plist
```

## Linux

### 文件布局

- 二进制: `/usr/local/bin/ssh-tunnel`
- 配置: `/etc/ssh-tunnel/config.properties`
- systemd 服务: `/etc/systemd/system/ssh-tunnel.service`
- SysV 脚本: `/etc/init.d/ssh-tunnel`
- 服务名: `ssh-tunnel`

### systemd 示例

```ini
[Unit]
Description=SSH Tunnel Service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/ssh-tunnel --config=/etc/ssh-tunnel/config.properties
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

### systemd 常用命令

```bash
sudo systemctl daemon-reload
sudo systemctl enable ssh-tunnel
sudo systemctl start ssh-tunnel
sudo systemctl restart ssh-tunnel
sudo systemctl status ssh-tunnel
```

### SysV 示例

```bash
#!/bin/sh
### BEGIN INIT INFO
# Provides:          ssh-tunnel
# Required-Start:    $remote_fs $network
# Required-Stop:     $remote_fs $network
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
### END INIT INFO

DAEMON="/usr/local/bin/ssh-tunnel"
CONFIG="/etc/ssh-tunnel/config.properties"
PIDFILE="/var/run/ssh-tunnel.pid"

start() {
    if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
        echo "ssh-tunnel already running"
        return 0
    fi
    "$DAEMON" --config="$CONFIG" >/dev/null 2>&1 &
    echo $! > "$PIDFILE"
}

stop() {
    if [ -f "$PIDFILE" ]; then
        kill "$(cat "$PIDFILE")" 2>/dev/null || true
        rm -f "$PIDFILE"
    fi
}

case "$1" in
    start) start ;;
    stop) stop ;;
    restart) stop; start ;;
    *) echo "Usage: $0 {start|stop|restart}"; exit 1 ;;
esac
```

## 规范配置键示例

```properties
home.dir=/var/lib/ssh-tunnel
server.ip=your-server-ip
server.ssh.port=22
ssh.private_key_path=/etc/ssh-tunnel/id_rsa
login.username=root
local.address=127.0.0.1:1081
http.local.address=127.0.0.1:1082
socks5.enable=true
http.enable=false
http.over-ssh.enable=false
http.domain-filter.enable=false
http.domain-filter.file-path=/var/lib/ssh-tunnel/domain.txt
admin.enable=true
admin.address=127.0.0.1:1083
log.file.path=/var/log/ssh-tunnel.log
auto-update.enabled=true
auto-update.owner=idefav
auto-update.repo=ssh-tunnel
auto-update.current-version=v0.0.0
auto-update.check-interval=3600
```
