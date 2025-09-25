# SSH-Tunnel

[![Apache license](https://img.shields.io/badge/License-Apache-blue.svg)](https://lbesson.mit-license.org/)
[![Build Release](https://github.com/idefav/ssh-tunnel/actions/workflows/release.yml/badge.svg)](https://github.com/idefav/ssh-tunnel/actions/workflows/release.yml)
[![GitHub release](https://img.shields.io/github/release/idefav/ssh-tunnel.svg)](https://github.com/idefav/ssh-tunnel/releases/)
[![GitHub commits](https://badgen.net/github/commits/idefav/ssh-tunnel)](https://GitHub.com/idefav/ssh-tunnel/commit/)
[![GitHub latest commit](https://badgen.net/github/last-commit/idefav/ssh-tunnel)](https://GitHub.com/idefav/ssh-tunnel/commit/)
[![GitHub forks](https://badgen.net/github/forks/idefav/ssh-tunnel/)](https://GitHub.com/idefav/ssh-tunnel/network/)
[![GitHub stars](https://badgen.net/github/stars/idefav/ssh-tunnel)](https://GitHub.com/idefav/ssh-tunnel/stargazers/)
[![GitHub watchers](https://badgen.net/github/watchers/idefav/ssh-tunnel/)](https://GitHub.com/idefav/ssh-tunnel/watchers/)
[![GitHub contributors](https://img.shields.io/github/contributors/idefav/ssh-tunnel.svg)](https://GitHub.com/idefav/ssh-tunnel/graphs/contributors/)

一个强大、简便的SSH隧道代理工具，支持SOCKS5和HTTP代理，内置Web管理界面，跨平台支持。

## ✨ 核心特性

- 🚀 **即开即用** - 仅需一个SSH连接，零服务端安装
- 🔐 **安全可靠** - 基于SSH协议，数据传输加密安全
- 🌐 **双重代理** - 同时支持SOCKS5和HTTP代理
- 🎯 **智能过滤** - 支持域名过滤，精确控制代理流量
- 📊 **管理界面** - 内置Web UI，实时监控连接状态和日志
- 🔒 **认证保护** - 支持HTTP Basic认证，提供额外安全层
- 🔄 **自动重连** - 连接断开时自动重连，保持服务稳定
- 🖥️ **跨平台** - 支持Windows、macOS、Linux多平台
- ⚙️ **服务化** - 支持Windows服务和macOS LaunchAgent

## 🎯 主要功能

| 功能 | 描述 | 默认状态 |
|------|------|----------|
| **SOCKS5代理** | 在本地1081端口提供SOCKS5代理服务 | ✅ 默认开启 |
| **HTTP代理** | 在本地1082端口提供HTTP代理服务 | ❌ 可选开启 |
| **域名过滤** | 根据域名文件决定是否通过隧道转发 | ❌ 可选开启 |
| **Basic认证** | HTTP代理支持用户名密码认证 | ❌ 可选开启 |
| **管理界面** | Web管理面板 (localhost:1083/view/index) | ❌ 可选开启 |
| **自动重连** | 连接断开时自动重连机制 | ✅ 默认开启 |
| **配置文件** | 支持配置文件和命令行参数 | ✅ 支持 |

## 🚀 快速开始

### 基础使用

```bash
./ssh-tunnel -s xx.xx.xx.xx
```

这将使用默认设置启动SSH隧道，在本地1081端口开启SOCKS5代理。

### 高级用法

```bash
# 开启所有功能
./ssh-tunnel -s xx.xx.xx.xx \
  -http.enable \
  -http.over.ssh.enable \
  -http.filter.domain.enable \
  -admin.enable

# 使用自定义端口和认证
./ssh-tunnel -s xx.xx.xx.xx \
  -p 2222 \
  -u myuser \
  -l 0.0.0.0:1088 \
  -http.enable \
  -http.local.addr 0.0.0.0:1089 \
  -http.basic.enable \
  -http.basic.username admin \
  -http.basic.password secret123
```

**注意**: 需要配置本地服务器到目标服务器的SSH免密登录，免密登录请参考: [SSH免密登录](https://idefav.github.io/ssh-tunnel/ssh-key-setup.html)

## 📋 命令行参数

### 连接配置
| 参数 | 短参数 | 描述 | 默认值 |
|------|--------|------|--------|
| `-server.ip` | `-s` | 服务器IP地址 | *必填* |
| `-server.ssh.port` | `-p` | 服务器SSH端口 | `22` |
| `-user` | `-u` | SSH用户名 | `root` |
| `-ssh.path.private_key` | `-pk` | SSH私钥路径 | `~/.ssh/id_rsa` |
| `-ssh.path.known_hosts` | `-pkh` | known_hosts文件路径 | `~/.ssh/known_hosts` |

### 代理配置
| 参数 | 描述 | 默认值 |
|------|------|--------|
| `-socks5.enable` | 是否开启SOCKS5代理 | `true` |
| `-local.addr` (`-l`) | SOCKS5代理监听地址 | `0.0.0.0:1081` |
| `-http.enable` | 是否开启HTTP代理 | `false` |
| `-http.local.addr` | HTTP代理监听地址 | `0.0.0.0:1082` |
| `-http.over.ssh.enable` | HTTP流量是否通过SSH | `false` |

### 域名过滤
| 参数 | 描述 | 默认值 |
|------|------|--------|
| `-http.filter.domain.enable` | 是否启用域名过滤 | `false` |
| `-http.filter.domain.file-path` | 域名过滤文件路径 | `~/.ssh-tunnel/domain.txt` |

### HTTP认证
| 参数 | 描述 | 默认值 |
|------|------|--------|
| `-http.basic.enable` | 是否开启HTTP Basic认证 | `false` |
| `-http.basic.username` | Basic认证用户名 | - |
| `-http.basic.password` | Basic认证密码 | - |

### 管理界面
| 参数 | 描述 | 默认值 |
|------|------|--------|
| `-admin.enable` | 是否启用管理界面 | `false` |
| `-admin.addr` | 管理界面监听地址 | `:1083` |

### 其他设置
| 参数 | 描述 | 默认值 |
|------|------|--------|
| `-retry.interval.sec` | 重连间隔时间(秒) | `3` |

## 🎛️ 管理界面

启用管理界面后，可通过浏览器访问 `http://localhost:1083/view/index` 进行管理。

### 功能特性
- **SSH状态** - 实时显示SSH连接状态和相关信息
- **应用日志** - 查看详细的运行日志信息
- **域名列表** - 查看和编辑域名过滤规则
- **配置信息** - 查看当前应用的所有配置参数
- **实时监控** - 连接状态、流量统计等实时数据

![管理界面](https://github.com/user-attachments/assets/fb5b016d-5e98-4a5f-ac37-1b7fe5fc5c50)

## 📁 配置文件

支持使用配置文件简化命令行参数，配置文件搜索路径：
- 当前目录: `./config.properties`
- 用户目录: `~/.ssh-tunnel/config.properties`
- 系统目录: `/etc/config/ssh-tunnel/config.properties` (Linux)

### 配置文件示例

```properties
# 服务器配置
server.ip=xx.xx.xx.xx
server.ssh.port=22
login.username=root
server.ssh.private_key_path=~/.ssh/id_rsa
server.ssh.known_hosts_path=~/.ssh/known_hosts

# 代理配置
local.address=0.0.0.0:1081
socks5.enable=true
http.enable=true
http.local.address=0.0.0.0:1082
http.over-ssh.enable=true

# 域名过滤
http.domain-filter.enable=true
http.filter.domain.file-path=~/.ssh-tunnel/domain.txt

# HTTP认证
http.basic.enable=true
http.basic.username=admin
http.basic.password=secret123

# 管理界面
admin.enable=true
admin.address=:1083

# 重连设置
retry.interval.sec=3
```

## 🎯 域名过滤

域名过滤功能允许你精确控制哪些域名的请求通过SSH隧道转发。

### 启用域名过滤

```bash
./ssh-tunnel -s xx.xx.xx.xx \
  -http.enable \
  -http.filter.domain.enable \
  -http.filter.domain.file-path /path/to/domain.txt
```

### 域名过滤文件格式

在 `domain.txt` 文件中，每行一个域名规则：

```text
# 支持通配符
*.google.com
*.github.com
*.stackoverflow.com

# 精确匹配
github.com
stackoverflow.com
example.org

# 支持子域名
sub.example.com
```

规则说明：
- 支持 `*` 通配符匹配
- 一行一个规则
- `#` 开头为注释行
- 匹配的域名将通过SSH隧道转发


## 🖥️ 系统服务配置

### MacOS 开机自启动设置

#### 1. 安装程序
将 ssh-tunnel 放到 `/usr/local/bin` 目录下：

```bash
sudo cp ssh-tunnel /usr/local/bin/
sudo chmod +x /usr/local/bin/ssh-tunnel
```

#### 2. 创建 LaunchAgent 配置文件

创建文件 `com.idefav.macos.ssh-tunnel.plist`：

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
      <string>-admin.enable</string>
    </array>
    <key>UserName</key>
    <string>root</string>
    <key>GroupName</key>
    <string>wheel</string>
    <key>StandardOutPath</key>
    <string>/var/log/ssh-tunnel.log</string>
    <key>StandardErrorPath</key>
    <string>/var/log/ssh-tunnel.error.log</string>
  </dict>
</plist>
```

#### 3. 安装和管理服务

```bash
# 复制配置文件到系统目录
sudo cp com.idefav.macos.ssh-tunnel.plist /Library/LaunchAgents/

# 设置文件权限
sudo chown root:wheel /Library/LaunchAgents/com.idefav.macos.ssh-tunnel.plist

# 加载并启动服务
sudo launchctl load -w /Library/LaunchAgents/com.idefav.macos.ssh-tunnel.plist

# 卸载服务
sudo launchctl unload /Library/LaunchAgents/com.idefav.macos.ssh-tunnel.plist

# 修改配置后重载服务
sudo launchctl unload /Library/LaunchAgents/com.idefav.macos.ssh-tunnel.plist && \
sudo launchctl load -w /Library/LaunchAgents/com.idefav.macos.ssh-tunnel.plist
```

### Windows 服务支持

#### 1. 配置文件准备

在C盘根目录创建 `ssh-tunnel` 文件夹，然后创建配置目录和文件：

```
C:\ssh-tunnel\.ssh-tunnel\config.properties
```

配置文件内容：

```properties
server.ip=xx.xx.xx.xx
server.ssh.port=22
server.ssh.private_key_path=C:\\Users\\%USERNAME%\\.ssh\\id_rsa
server.ssh.known_hosts_path=C:\\Users\\%USERNAME%\\.ssh\\known_hosts
login.username=root
local.address=0.0.0.0:1081
http.local.address=0.0.0.0:1082
http.enable=true
socks5.enable=true
http.over-ssh.enable=true
http.domain-filter.enable=true
http.filter.domain.file-path=C:\\Users\\%USERNAME%\\Documents\\ssh-tunnel\\domain.txt
admin.enable=true
admin.address=:1083
```

#### 2. 安装Windows服务

使用管理员权限运行命令提示符：

```cmd
.\ssh-tunnel-svc-windows-amd64.exe install --config=C:\ssh-tunnel\.ssh-tunnel\config.properties
```

#### 3. 管理Windows服务

```cmd
# 启动服务
net start SSHTunnelService

# 停止服务
net stop SSHTunnelService

# 卸载服务
.\ssh-tunnel-svc-windows-amd64.exe uninstall
```

或通过图形界面：
1. 按 `Win + R` 键，输入 `services.msc`
2. 找到 `SSHTunnelService` 服务
3. 右键选择启动/停止/重启

## 📥 安装指南

### 从 GitHub Releases 下载

访问 [Releases 页面](https://github.com/idefav/ssh-tunnel/releases) 下载适合你系统的预编译版本：

- **Windows**: `ssh-tunnel-windows-amd64.exe`
- **macOS**: `ssh-tunnel-darwin-amd64`
- **Linux**: `ssh-tunnel-linux-amd64`

### 从源码编译

```bash
# 克隆仓库
git clone https://github.com/idefav/ssh-tunnel.git
cd ssh-tunnel

# 编译
go build -o ssh-tunnel main.go

# 或使用 make (如果有 Makefile)
make build
```

### 使用 Go 安装

```bash
go install github.com/idefav/ssh-tunnel@latest
```

## 🔧 常见问题

### Q: SSH连接失败怎么办？
A: 
1. 检查SSH免密登录是否配置正确
2. 确认服务器IP和端口是否正确
3. 验证用户名和私钥路径
4. 查看详细日志输出

### Q: 代理无法连接？
A: 
1. 检查本地端口是否被占用
2. 确认防火墙设置允许端口访问
3. 验证代理配置是否正确

### Q: 域名过滤不生效？
A: 
1. 确认域名过滤文件路径正确
2. 检查文件格式和语法
3. 确保HTTP代理已启用

### Q: 管理界面无法访问？
A: 
1. 确认已启用管理界面 (`-admin.enable`)
2. 检查管理端口是否被占用
3. 尝试使用 `127.0.0.1:1083/view/index` 访问

## 🤝 贡献指南

欢迎提交 Issue 和 Pull Request！

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 打开 Pull Request

## 📄 许可证

本项目基于 Apache License 2.0 许可证开源。详见 [LICENSE](LICENSE) 文件。

## 🌟 支持项目

如果这个项目对你有帮助，请给个 ⭐️ Star！

## 📊 贡献者

[![Contributors over time](https://contributor-graph-api.apiseven.com/contributors-svg?chart=contributorOverTime&repo=idefav/ssh-tunnel)](https://www.apiseven.com/en/contributor-graph?chart=contributorOverTime&repo=idefav/ssh-tunnel)
