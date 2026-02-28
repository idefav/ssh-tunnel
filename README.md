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

注意: 需要配置本地服务器到目标服务器的SSH免密登录, 免密登录请参考: [SSH免密登录](https://idefav.github.io/ssh-tunnel/ssh-key-setup.html)

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
        过滤http请求 (default "C:\\Users\\idefav/.ssh-tunnel/domain.txt")
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
        私钥地址(短命令) (default "C:\\Users\\idefav/.ssh/id_rsa")
  -retry.interval.sec int
        重试间隔时间(秒) (default 3)
  -ssh.dial.timeout.sec int
        SSH握手超时(秒) (default 5)
  -ssh.dest.dial.timeout.sec int
        SSH目标连接超时(秒) (default 3)
  -ssh.keepalive.interval.sec int
        SSH保活间隔(秒) (default 2)
  -ssh.keepalive.count.max int
        SSH保活最大连续失败次数 (default 2)
  -ssh.reconnect.max.retries int
        SSH重连最大重试次数 (default 20)
  -ssh.reconnect.max.interval.sec int
        SSH重连最大退避间隔(秒) (default 5)
  -s string
        服务器IP地址(短命令)
  -server.ip string
        服务器IP地址
  -server.ssh.port int
        服务器SSH端口 (default 22)
  -socks5.enable
        是否开启Socks5代理 (default true)
  -ssh.path.private_key string
        私钥地址 (default "C:\\Users\\idefav/.ssh/id_rsa")
  -u string
        用户名(短命令) (default "root")
  -user string
        用户名 (default "root")



```

## AdminUI

默认地址: localhost:1083/view/index

### 主要功能
- 📊 **实时状态监控** - 查看SSH连接状态、隧道状态
- ⚙️ **配置管理** - 在线修改配置参数，支持实时预览
- 🧩 **多 Profile 管理** - 支持维护多套 SSH 配置并在管理页动态切换 🆕
- 🗂️ **Profile 文件持久化** - 保存 Profile 时同步写入 `profiles.json`（格式化JSON）🆕
- 📁 **进程信息** - 显示程序执行路径和工作目录，便于故障排查
- 📶 **SSH链路指标** - SSH状态页支持延迟测试、实时上下行速率和累计流量展示 🆕
- 🔄 **服务控制** - 支持重启服务以应用新配置
- 📋 **域名缓存** - 查看和管理域名匹配缓存
- 🌐 **域名管理** - 响应式域名列表界面，充分利用浏览器空间 🆕
- 📝 **日志查看** - 实时查看应用运行日志
- 🆕 **版本管理** - 自动检查GitHub Release更新，支持一键更新

### UI优化特性 🆕
- 🖥️ **自适应高度** - 域名管理页面动态适配浏览器高度
- 📱 **响应式设计** - 支持不同屏幕尺寸的最佳显示效果
- ✨ **交互增强** - 优化悬停效果和视觉反馈

### 版本管理功能 🆕
- 🔄 **自动更新检查** - 定时从GitHub Release检测新版本
- 🔒 **SHA256校验** - 确保下载文件的完整性和安全性
- 📋 **版本列表** - 显示所有可用版本和发布说明
- 📄 **分页展示** - 可用版本列表支持分页浏览
- 🎯 **指定版本更新** - 支持在版本列表中选中指定版本执行更新
- ⚙️ **更新设置** - 可自定义检查间隔和仓库信息
- 📱 **响应式界面** - 现代化的版本管理Web界面

访问版本管理页面: `http://localhost:1083/view/version`

## SSH快速重连参数（建议）

当出现大量 `Get Dest Connection Failed(...): context deadline exceeded` 时，建议启用以下参数以获得 1-5 秒恢复：

```properties
retry.interval.sec=1
ssh.dial.timeout.sec=5
ssh.dest.dial.timeout.sec=3
ssh.keepalive.interval.sec=2
ssh.keepalive.count.max=2
ssh.reconnect.max.retries=20
ssh.reconnect.max.interval.sec=5
```

说明：
- `ssh.dest.dial.timeout.sec` 控制每次通过 SSH 建立目标连接的超时。
- `ssh.keepalive.*` 控制失效检测速度，值越小恢复越快但探测更频繁。
- `ssh.reconnect.max.interval.sec` 控制指数退避上限，避免失效后长时间等待重试。

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
### 1. 配置文件准备
在C盘跟目录参加 `ssh-tunnel`
在该目录下参加 `.ssh-tunnel` 目录， 并写入 `config.properties`
配置文件完整路径: `C:\ssh-tunnel\.ssh-tunnel\config.properties`

```properties
server.ip=xx.xx.xx.xx
server.ssh.port=22
server.ssh.private_key_path=C:\\Users\\idefav\\.ssh\\id_rsa
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

# 自动更新配置
auto-update.enabled=true
auto-update.owner=idefav
auto-update.repo=ssh-tunnel
auto-update.current-version=v0.0.0
auto-update.check-interval=3600
```

### 2. 安装windows服务

```text
 .\ssh-tunnel-svc-windows-amd64.exe install  --config=C:\ssh-tunnel\.ssh-tunnel\config.properties
```

### 3. 查看服务
win+r 输入 services.svc 打开服务管理窗口
找到 SSHTunnelService 并启动它

### 4. 在windows配置中启动代理

### 最近更新 🆕

#### 2026-02-26 - Profile/SSH重连增强
- ✅ Profile 编辑区改为弹窗，支持新增/编辑/复制（复制时自动清空 Profile ID）
- ✅ Profile 列表新增 SSH 用户列，便于区分不同账号
- ✅ 保存 Profile 时同步写入 `profiles.json` 文件，并使用美化 JSON 输出
- ✅ Profile 切换后刷新隧道运行时参数（地址/端口/用户/私钥），再强制断开并重连
- ✅ `重新连接 SSH` 按钮改为读取最新配置并应用 active profile 后再重连
- ✅ 修复 HTTP 代理转发中 `io.Copy` 因目标连接为 nil 导致的 panic

#### 2026-02-25 - Multi Profile MVP
- ✅ 管理页新增 Profile 管理卡片（列表、切换、编辑回填、保存）
- ✅ 新增管理API：`/admin/profiles`、`/admin/profiles/upsert`、`/admin/profiles/switch`
- ✅ 新增切换状态查询API：`/admin/profiles/switch/status`
- ✅ 新增 Profile 删除能力（禁止删除当前激活 Profile）
- ✅ 切换 Profile 后自动触发 SSH 重连

#### 2025-06-23 - UI优化
- 🎨 **置顶按钮样式优化**: 改进置顶按钮的视觉效果和交互体验
  - 增大按钮尺寸 (40px → 48px) 提升用户体验
  - 添加现代化阴影效果和完美居中对齐
  - 优化点击动画和悬停反馈效果
  - 详细文档: [置顶按钮优化说明](docs/features/back-to-top-optimization.md)

Contributors
[![Contributors over time](https://contributor-graph-api.apiseven.com/contributors-svg?chart=contributorOverTime&repo=idefav/ssh-tunnel)](https://www.apiseven.com/en/contributor-graph?chart=contributorOverTime&repo=idefav/ssh-tunnel)

## 项目结构

```
ssh-tunnel/
├── 📁 docs/              # 项目文档 📝
│   ├── 📁 features/      # 功能说明文档
│   ├── 📁 setup/         # 部署配置文档
│   ├── 📁 assets/        # 静态资源
│   ├── PANIC_RECOVERY_REPORT.md  # Panic恢复机制测试报告 🆕
│   ├── AUTO_UPDATE_*.md  # 自动更新相关文档
│   └── README.md         # 文档目录说明
├── 📁 scripts/           # 脚本文件 🔧
│   ├── 📁 test/          # 测试脚本 (包含API测试)
│   ├── 📁 utils/         # 工具脚本 (启动脚本等)
│   ├── 📁 dev/           # 开发脚本 (预留)
│   └── README.md         # 脚本目录说明
├── 📁 api/               # API接口
├── 📁 tunnel/            # 隧道核心功能
├── 📁 service/           # 服务管理
├── 📁 views/             # Web界面
├── 📁 safe/              # 安全模块 (Panic恢复) 🆕
└── 📁 cfg/               # 配置管理
```

### 文档资源

- 📖 [功能文档](docs/features/) - 各功能模块详细说明
- 🔧 [部署文档](docs/setup/) - 多平台部署指南
- 🧪 [测试脚本](scripts/test/) - 功能测试脚本和API测试
- 📝 [API文档](docs/config-api.md) - 配置API接口说明
- 🛡️ [安全机制](docs/PANIC_RECOVERY_REPORT.md) - Panic恢复机制报告

### 快速导航

- [进程信息功能](docs/features/process-info-feature.md) - 新增的进程信息显示功能
- [服务重启功能](docs/features/restart-service-feature.md) - 服务重启功能说明
- [多平台部署](docs/setup/MULTIPLATFORM_SERVICE_SETUP.md) - Windows/macOS/Linux服务部署
- [测试脚本使用](scripts/test/README.md) - 测试脚本使用说明
- [日志清理功能](docs/features/) - 日志文件内容清理功能 🆕
