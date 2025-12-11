# Introduction

[中文](README.md) | English

[![Apache license](https://img.shields.io/badge/License-Apache-blue.svg)](https://lbesson.mit-license.org/)
[![Build Release](https://github.com/idefav/ssh-tunnel/actions/workflows/release.yml/badge.svg)](https://github.com/idefav/ssh-tunnel/actions/workflows/release.yml)
[![GitHub release](https://img.shields.io/github/release/idefav/ssh-tunnel.svg)](https://github.com/idefav/ssh-tunnel/releases/)
[![GitHub commits](https://badgen.net/github/commits/idefav/ssh-tunnel)](https://GitHub.com/idefav/ssh-tunnel/commit/)
[![GitHub latest commit](https://badgen.net/github/last-commit/idefav/ssh-tunnel)](https://GitHub.com/idefav/ssh-tunnel/commit/)
[![GitHub forks](https://badgen.net/github/forks/idefav/ssh-tunnel/)](https://GitHub.com/idefav/ssh-tunnel/network/)
[![GitHub stars](https://badgen.net/github/stars/idefav/ssh-tunnel)](https://GitHub.com/idefav/ssh-tunnel/stargazers/)
[![GitHub watchers](https://badgen.net/github/watchers/idefav/ssh-tunnel/)](https://GitHub.com/idefav/ssh-tunnel/watchers/)
[![GitHub contributors](https://img.shields.io/github/contributors/idefav/ssh-tunnel.svg)](https://GitHub.com/idefav/ssh-tunnel/graphs/contributors/)

Open ssh tunnel, start SOCKS5 port locally by default 1081

## Quick Start

```bash
./ssh-tunnel -s xx.xx.xx.xx
```

Note: You need to configure SSH passwordless login from your local server to the target server. For passwordless login, please refer to: [SSH Passwordless Login](https://idefav.github.io/ssh-tunnel/ssh-key-setup.html)

## Commands

```bash
./ssh-tunnel -h
Usage of ./bin/ssh-tunnel-amd64-darwin:
  -admin.addr string
        Admin listening address (default ":1083")
  -admin.enable
        Enable Admin page
  -http.basic.enable
        Enable HTTP Basic Authentication
  -http.basic.password string
        HTTP Basic Authentication password
  -http.basic.username string
        Basic Authentication username
  -http.enable
        Enable HTTP proxy
  -http.filter.domain.enable
        Enable HTTP domain filtering
  -http.filter.domain.file-path string
        HTTP request filter (default "C:\\Users\\idefav/.ssh-tunnel/domain.txt")
  -http.local.addr string
        HTTP listening address (default "0.0.0.0:1082")
  -http.over.ssh.enable
        Enable HTTP Over SSH
  -l string
        Local address (short command) (default "0.0.0.0:1081")
  -local.addr string
        Local address (default "0.0.0.0:1081")
  -p int
        Server SSH port (short command) (default 22)
  -pk string
        Private key path (short command) (default "C:\\Users\\idefav/.ssh/id_rsa")
  -pkh string
        Known hosts path (short command) (default "C:\\Users\\idefav/.ssh/known_hosts")
  -retry.interval.sec int
        Retry interval time (seconds) (default 3)
  -s string
        Server IP address (short command)
  -server.ip string
        Server IP address
  -server.ssh.port int
        Server SSH port (default 22)
  -socks5.enable
        Enable SOCKS5 proxy (default true)
  -ssh.path.known_hosts string
        Known hosts path (default "C:\\Users\\idefav/.ssh/known_hosts")
  -ssh.path.private_key string
        Private key path (default "C:\\Users\\idefav/.ssh/id_rsa")
  -u string
        Username (short command) (default "root")
  -user string
        Username (default "root")



```

## AdminUI

Default address: localhost:1083/view/index
![image](https://github.com/user-attachments/assets/fb5b016d-5e98-4a5f-ac37-1b7fe5fc5c50)


## MacOS Auto-Start Settings

Place ssh-tunnel in the /usr/local/bin directory

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

Save this file as `com.idefav.macos.ssh-tunnel.plist` (note: the filename should match the `label`)

Place it in the `/Library/LaunchAgents` directory

```bash
sudo chown -R root /Library/LaunchAgents/com.idefav.macos.ssh-tunnel.plist
```

```bash
# Load configuration
launchctl load -w /Library/LaunchAgents/com.idefav.macos.ssh-tunnel.plist

# Unload configuration
launchctl unload /Library/LaunchAgents/com.idefav.macos.ssh-tunnel.plist

# Reload configuration after modification
launchctl unload /Library/LaunchAgents/com.idefav.macos.ssh-tunnel.plist && \
launchctl load -w /Library/LaunchAgents/com.idefav.macos.ssh-tunnel.plist
```

# Windows Service Support
### 1. Configuration File Preparation
Create a `ssh-tunnel` directory in the C: drive root
Create a `.ssh-tunnel` directory within it, and add the `config.properties` file
Complete configuration file path: `C:\ssh-tunnel\.ssh-tunnel\config.properties`

```properties
server.ip=xx.xx.xx.xx
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

### 2. Install Windows Service

```text
 .\ssh-tunnel-svc-windows-amd64.exe install  --config=C:\ssh-tunnel\.ssh-tunnel\config.properties
```

### 3. View Service
Press Win+R, type services.msc to open the Service Management window
Find SSHTunnelService and start it

### 4. Configure Proxy in Windows Settings


## Contributors
[![Contributors over time](https://contributor-graph-api.apiseven.com/contributors-svg?chart=contributorOverTime&repo=idefav/ssh-tunnel)](https://www.apiseven.com/en/contributor-graph?chart=contributorOverTime&repo=idefav/ssh-tunnel)
