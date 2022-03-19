# Introduction
Open ssh tunnel, start Sock5 port locally by default 1081

## quick start

```bash
./ssh-tunnel -s xx.xx.xx.xx
```

## commands
```bash
./ssh-tunnel -h
Usage of ./ssh-tunnel:
  -l string
        本地地址(短命令) (default "0.0.0.0:1081")
  -local.addr string
        本地地址 (default "0.0.0.0:1081")
  -p int
        服务器SSH端口(短命令) (default 22)
  -pk string
        私钥地址(短命令) (default "/Users/idefav/.ssh/id_rsa")
  -pkh string
        已知主机地址(短命令) (default "/Users/idefav/.ssh/known_hosts")
  -s string
        服务器IP地址(短命令)
  -server.ip string
        服务器IP地址
  -server.ssh.port int
        服务器SSH端口 (default 22)
  -ssh.path.known_hosts string
        已知主机地址 (default "/Users/idefav/.ssh/known_hosts")
  -ssh.path.private_key string
        私钥地址 (default "/Users/idefav/.ssh/id_rsa")
  -u string
        用户名(短命令) (default "root")
  -user string
        用户名 (default "root")
```

## MacOS boot auto-start settings

把 ssh-tunnel 放到 /usr/local/bin 目录下

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple Computer//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>com.idefav.macos.ssh-tunnel</string>
    <key>Disabled</key>
    <false/>
    <key>KeepAlive</key>
    <true/>
    <key>ProcessType</key>
    <string>Background</string>
    <key>ProgramArguments</key>
    <array>
      <string>/usr/local/bin/ssh-tunnel</string>
      <string>-s</string>
      <string>x.x.x.x</string>
    </array>
    <key>UserName</key>
    <string>root</string>
    <key>GroupName</key>
    <string>wheel</string>
  </dict>
</plist>
```

把这个文件保持到 `com.idefav.macos.ssh-tunnel.plist` 注意文件名要和`label` 相同

放到 `/Library/LaunchAgents` 目录下

```bash
sudo chown -R root /Library/LaunchAgents/com.idefav.macos.ssh-tunnel.plist
```

```bash
# 加载配置
launchctl load -w /Library/LaunchAgents/com.idefav.macos.ssh-tunnel.plist

# 卸载配置
launchctl unload /Library/LaunchAgents/com.idefav.macos.ssh-tunnel.plist

# 修改配置后重载配置
launchctl unload /Library/LaunchAgents/com.idefav.macos.ssh-tunnel.plist && \
launchctl load -w /Library/LaunchAgents/com.idefav.macos.ssh-tunnel.plist
```
