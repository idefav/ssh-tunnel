# Introduction
[![Apache license](https://img.shields.io/badge/License-Apache-blue.svg)](https://lbesson.mit-license.org/)
[![Build Release](https://github.com/idefav/ssh-tunnel/actions/workflows/release.yml/badge.svg)](https://github.com/idefav/ssh-tunnel/actions/workflows/release.yml)
[![GitHub release](https://img.shields.io/github/release/idefav/ssh-tunnel.svg)](https://github.com/idefav/ssh-tunnel/releases/)
[![GitHub commits](https://badgen.net/github/commits/idefav/ssh-tunnel)](https://GitHub.com/idefav/ssh-tunnel/commit/)
[![GitHub latest commit](https://badgen.net/github/last-commit/idefav/ssh-tunnel)](https://GitHub.com/idefav/ssh-tunnel/commit/)
[![GitHub forks](https://badgen.net/github/forks/idefav/ssh-tunnel/)](https://GitHub.com/idefav/ssh-tunnel/network/)
[![GitHub stars](https://badgen.net/github/stars/idefav/ssh-tunnel)](https://GitHub.com/idefav/ssh-tunnel/stargazers/)
[![GitHub watchers](https://badgen.net/github/watchers/idefav/ssh-tunnel/)](https://GitHub.com/idefav/ssh-tunnel/watchers/)
[![GitHub contributors](https://img.shields.io/github/contributors/idefav/ssh-tunnel.svg)](https://GitHub.com/idefav/ssh-tunnel/graphs/contributors/)

Open ssh tunnel, start Sock5 port locally by default 1081

## quick start

```bash
./ssh-tunnel -s xx.xx.xx.xx
```

## commands

```bash
./ssh-tunnel -h
Usage of ./bin/ssh-tunnel-amd64-darwin:
  -admin.addr string
        Admin监听地址 (default ":1083")
  -admin.enable
        是否启用Admin页面
  -http.basic.enable
        是否开启Http的Basic认证
  -http.basic.password string
        Http Basic认证, 密码
  -http.basic.username string
        Basic认证, 用户名
  -http.enable
        是否开启Http代理
  -http.filter.domain.enable
        是否启用Http域名过滤
  -http.filter.domain.file-path string
        过滤http请求 (default "/Users/wuzishu/.ssh-tunnel/domain.txt")
  -http.local.addr string
        Http监听地址 (default "0.0.0.0:1082")
  -http.over.ssh.enable
        是否开启Http Over SSH
  -l string
        本地地址(短命令) (default "0.0.0.0:1081")
  -local.addr string
        本地地址 (default "0.0.0.0:1081")
  -p int
        服务器SSH端口(短命令) (default 22)
  -pk string
        私钥地址(短命令) (default "/Users/wuzishu/.ssh/id_rsa")
  -pkh string
        已知主机地址(短命令) (default "/Users/wuzishu/.ssh/known_hosts")
  -s string
        服务器IP地址(短命令)
  -server.ip string
        服务器IP地址
  -server.ssh.port int
        服务器SSH端口 (default 22)
  -socks5.enable
        是否开启Socks5代理 (default true)
  -ssh.path.known_hosts string
        已知主机地址 (default "/Users/wuzishu/.ssh/known_hosts")
  -ssh.path.private_key string
        私钥地址 (default "/Users/wuzishu/.ssh/id_rsa")
  -u string
        用户名(短命令) (default "root")
  -user string
        用户名 (default "root")


```

## AdminUI

默认地址: localhost:1083/view/index
<img width="1353" alt="image" src="https://user-images.githubusercontent.com/6405415/206909223-b47372db-5356-4cbf-8c11-8929d6227896.png">

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
      <string>xx.xx.xx.xx</string>
      <string>-server.ssh.port</string>
      <string>10022</string>
      <string>-l</string>
      <string>0.0.0.0:1081</string>
      <string>-socks5.enable=false</string>
      <string>-http.enable</string>
      <string>-http.over.ssh.enable</string>
      <string>-http.filter.domain.enable</string>
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

# Windows Service支持
### 1. 在C盘跟目录参加 `ssh-tunnel`
在该目录下参加 `.ssh-tunnel` 目录， 并写入 `config.properties`
```text
server.ip=47.56.255.129
server.ssh.port=22
server.ssh.private_key_path=C:\\Users\\idefav\\.ssh\\id_rsa
server.ssh.known_hosts_path=C:\\Users\\idefav\\.ssh\\known_hosts
login.username=root
local.address=0.0.0.0:1081
http.local.address=0.0.0.0:1082
http.enable=true
socks5.enable=true
http.over-ssh.enable=true
http.domain-filter.enable=true
http.filter.domain.file-path=C:\\Users\\idefav\\Documents\\ssh-tunnel\\domain.txt
admin.enable=true
admin.address=:1083
```

### 2. 安装windows服务

```text
 .\ssh-tunnel-winsvc.exe install
```

### 3. 查看服务
win+r 输入 services.svc 打开服务管理窗口
找到 SSHTunnelService 并启动它

### 4. 在windows配置中启动代理


Contributors
[![Contributors over time](https://contributor-graph-api.apiseven.com/contributors-svg?chart=contributorOverTime&repo=idefav/ssh-tunnel)](https://www.apiseven.com/en/contributor-graph?chart=contributorOverTime&repo=idefav/ssh-tunnel)
