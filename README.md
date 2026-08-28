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

注意: 首次安装脚本会先检查当前密钥是否已完成 SSH 免密登录，未配置时会自动尝试把本地公钥写入远程服务器；如需手动处理，可参考: [SSH免密登录](https://idefav.github.io/ssh-tunnel/ssh-key-setup.html)

## one-click install

首次安装请使用 GitHub Pages 单入口安装脚本:

```bash
curl -fsSL https://idefav.github.io/ssh-tunnel/install | sh
```

Windows PowerShell:

```powershell
irm https://idefav.github.io/ssh-tunnel/install | iex
```

脚本只处理首次安装。若检测到已有安装、已有服务或已有配置，会直接提示访问管理页版本页面完成更新:

```text
http://127.0.0.1:1083/view/version
```

首次安装时，安装器会自动扫描本机 `.ssh` 目录中的 SSH 密钥对，优先选择已存在的本地公钥作为默认输入，并自动推导对应私钥；如果本机还没有任何 SSH 密钥，脚本会提示自动生成或进入交互式 `ssh-keygen` 生成。随后在写入服务配置前，脚本会先检查该密钥是否已经完成 SSH 免密登录，若未配置则自动尝试完成配置。你也可以手动改成其他公钥或私钥路径。

## commands

```bash
./ssh-tunnel -h
Usage of ./bin/ssh-tunnel-amd64-darwin:
  -admin.address string
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
  -http.domain-filter.enable
        是否启用Http域名过滤
  -http.domain-filter.file-path string
        过滤http请求 (default "C:\\Users\\idefav/.ssh-tunnel/domain.txt")
  -http.local.address string
        Http监听地址 (default "0.0.0.0:1082")
  -http.over.ssh.enable
        是否开启Http Over SSH
  -l string
        本地地址(短命令) (default "0.0.0.0:1081")
  -local.address string
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
  -ssh.pool.size int
        SSH连接池大小 (default 5)
  -ssh.pool.replenish.interval.sec int
        SSH连接池补充间隔(秒) (default 1)
  -ssh.pool.balance.strategy string
        SSH连接池负载均衡策略: least_active/round_robin/random (default "least_active")
  -ssh.probe.url string
        SSH主动探测地址
  -ssh.probe.urls string
        SSH主动探测地址列表(逗号分隔)
  -ssh.probe.timeout.sec int
        SSH主动探测超时(秒) (default 3)
  -ssh.probe.failure.threshold int
        SSH主动探测失败阈值 (default 2)
  -proxy.retry.max.attempts int
        代理请求失败额外重试次数 (default 1)
  -proxy.retry.initial.buffer.bytes int
        代理早期重试初始缓存大小(字节) (default 32768)
  -s string
        服务器IP地址(短命令)
  -server.ip string
        服务器IP地址
  -server.ssh.port int
        服务器SSH端口 (default 22)
  -socks5.enable
        是否开启Socks5代理 (default true)
  -ssh.private_key_path string
        私钥地址 (default "C:\\Users\\idefav/.ssh/id_rsa")
  -u string
        用户名(短命令) (default "root")
  -login.username string
        用户名 (default "root")



```

## AdminUI

默认地址: localhost:1083/view/index

### 主要功能
- 📊 **实时状态监控** - 查看SSH连接状态、隧道状态
- ⚙️ **配置管理** - 在线修改配置参数，支持实时预览
- 🧩 **多 Profile 管理** - 支持维护多套 SSH 配置并在管理页动态切换 🆕
- 🗂️ **Profile 文件持久化** - 保存 Profile 时同步写入 `profiles.json`（格式化JSON）🆕
- 🧭 **路由规则组** - 在 `/view/routes` 将同一服务的多个域名、IP 或 CIDR 归组管理；规则可继承组出口，也可覆盖为固定/随机多 Profile 出口 🆕
- 🧰 **路由批量管理** - 支持跨组多选、搜索筛选、批量迁移到现有/新规则组、继承切换、统一独立出口及批量启停 🆕
- 📁 **进程信息** - 显示程序执行路径和工作目录，便于故障排查
- 📶 **持久化流量统计** - SSH 状态页展示总体、各 Profile 与直连的实时/累计流量及 24 小时、7 天、30 天、按月历史；重启不丢累计 🆕
- 📊 **Profile 节点质量** - 配置页展示近 24 小时真实访问成功率、平均/P95 建连延迟、连续失败，并支持单节点或全部节点手动测试 SSH 握手和出口延迟 🆕
- 🔢 **连接统计** - SSH状态页新增当前SSH连接数与累计重连次数展示 🆕
- ♻️ **计数清零** - SSH状态页支持一键清零重连次数，便于分阶段观测 🆕
- 🏊 **连接池管理** - SSH状态页展示连接池成员状态（健康/可疑/探测中/已摘除），支持手动探测指定成员 🆕
- 🗑️ **池成员摘除** - 支持手动摘除指定活跃成员，一键清理已摘除成员历史 🆕
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

### 路由规则组文件与流量数据库

- 路由规则组写入活动配置文件同目录的 `routes.json`（格式版本 2）；未显式指定配置文件时写入 `~/.ssh-tunnel/routes.json`。示例见 [`examples/routes.json.template`](examples/routes.json.template)。
- 每个组必须配置名称、启停状态和默认出口。组内规则同时省略 `strategy` 与 `targetProfileIds` 时继承组出口；也可以单独覆盖为 `fixed` 或 `random`。
- 路由页可批量选择当前筛选结果；“取消继承并保持当前出口”会先记录每条规则的有效出口再固化，批量迁移本身不会意外改变路径。
- `fixed` 必须选择一个 Profile；`random` 至少选择两个不同 Profile。随机出口在每个新 TCP 连接上重新打乱目标，并在失败时依次尝试，全部失败后不会回退默认 Profile。
- v1 平铺 `routes.json` 和旧 `profiles.json` 中的 `domainRoutes` 会自动迁入确定 ID 的“未分组（自动迁移）”，每条旧规则保留独立出口，升级前后路由行为不变。
- 流量累计与最近 365 天小时历史保存在同目录的 `traffic.db`。数据库基于纯 Go 的 [bbolt v1.4.3](https://github.com/etcd-io/bbolt/blob/v1.4.3/README.md)（[Go 版本声明](https://github.com/etcd-io/bbolt/blob/v1.4.3/go.mod)），每 5 秒批量刷盘，正常停止时强制同步。
- v1.7.0 在同一数据库中增加最近 24 小时 Profile 健康桶：真实访问统计成功率、平均/P95 建连延迟和连续失败；手动测试单独报告 SSH 握手与出口探测耗时。直连请求不会污染 Profile 数据。

### v1.5.0 从旧版本升级

1. 升级前备份当前配置文件和 `profiles.json`，并停止旧进程。
2. 首次启动会自动将 Profile 中的旧 `domainRoutes` 迁移到 `routes.json`。如果多个 Profile 存在重复的规范化路由，程序会拒绝启动并报告冲突，请先清理重复项。
3. `traffic.db` 从升级后开始累计，旧版本仅存内存的流量无法迁移。请确保服务用户可写配置目录，且不要让多个进程共用同一数据库。
4. 迁移后若需降级到 v1.4.x，必须同时恢复升级前的 Profile 备份，因为旧版本不识别独立 `routes.json`。

程序支持在 `/view/version` 下载、SHA256 校验并安装 GitHub Release。`auto-update.enabled=true` 开启的是定时检查，不会在无人确认时自动替换程序。

### v1.6.0 从平铺路由升级到规则组

1. 启动前备份 `routes.json` 和 `profiles.json`。
2. 首次加载版本 1 的 `routes.json` 时，程序会原子写入版本 2，并将原规则迁入“未分组（自动迁移）”；旧规则的策略和目标作为单规则覆盖保留。
3. 组开关与规则开关共同决定是否生效。关闭组不会改变组内规则各自的开关状态，再次开启后会恢复。
4. 版本 2 的 `routes.json` 不能直接被只支持平铺路由的旧程序读取；如需降级，请同时恢复升级前备份。

### v1.6.1 批量管理升级

v1.6.1 继续使用版本 2 `routes.json`，无需迁移配置。升级后可直接在路由页跨组选择规则，并批量迁移、切换继承方式、统一独立出口或启停；“取消继承并保持当前出口”会固化操作前的实际出口，避免迁移时意外改变流量路径。

### v1.7.0 Profile 节点质量升级

v1.7.0 无需迁移 `profiles.json` 或 `routes.json`。启动后会在现有 `traffic.db` 自动创建健康统计桶；访问配置页即可查看五种节点状态和最近 24 小时指标，并可按需测试单节点或全部节点。指标定义、状态规则和 API 见 [`docs/features/profile-health-v1.7.md`](docs/features/profile-health-v1.7.md)。

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

## SSH连接稳定性修复（2026-03-03）

本次稳定性修复包含以下行为调整：

- 修复 `keepAliveMonitor` 的并发计数问题，避免 `sync: negative WaitGroup counter`。
- 单请求失败（目标连接超时/拒绝等）默认不再触发全局 SSH 重连。
- 仅在识别为 SSH 链路级错误时触发重连（例如连接已关闭、通道异常包、broken pipe）。
- 强制同一时刻仅保留一个活跃 SSH 连接；重连成功后会及时释放旧连接。

详细说明见：`docs/features/ssh-stability-fix-2026-03.md`

## SSH 连接池参数（v1.4.18+）

开启多成员 SSH 连接池，让每个 SOCKS5/HTTP 请求均衡复用多条 SSH 链路：

```properties
ssh.pool.size=5
ssh.pool.replenish.interval.sec=1
ssh.pool.balance.strategy=least_active
ssh.probe.url=https://www.google.com
ssh.probe.timeout.sec=3
ssh.probe.failure.threshold=2
proxy.retry.max.attempts=1
```

说明：
- `ssh.pool.size` 控制连接池成员数，增大时自动补充，减小时收缩空闲成员。
- `ssh.pool.balance.strategy` 支持 `least_active`（最少活跃请求）、`round_robin`、`random` 三种策略。
- `ssh.probe.url` / `ssh.probe.urls` 配置主动探测目标，用于检测成员健康。
- `proxy.retry.max.attempts` 请求失败时自动换成员重试次数。

## MacOS boot auto-start settings

把 ssh-tunnel 放到 /usr/local/bin 目录下

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple Computer//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>com.idefav.ssh-tunnel</string>
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
      <string>-http.domain-filter.enable</string>
    </array>
    <key>UserName</key>
    <string>root</string>
    <key>GroupName</key>
    <string>wheel</string>
  </dict>
</plist>
```

把这个文件保存为 `com.idefav.ssh-tunnel.plist`，注意文件名要和 `label` 相同

放到 `/Library/LaunchDaemons` 目录下

```bash
sudo chown -R root /Library/LaunchDaemons/com.idefav.ssh-tunnel.plist
```

```bash
# 加载配置
launchctl load -w /Library/LaunchDaemons/com.idefav.ssh-tunnel.plist

# 卸载配置
launchctl unload /Library/LaunchDaemons/com.idefav.ssh-tunnel.plist

# 修改配置后重载配置
launchctl unload /Library/LaunchDaemons/com.idefav.ssh-tunnel.plist && \
launchctl load -w /Library/LaunchDaemons/com.idefav.ssh-tunnel.plist
```

# Windows Service支持
### 1. 配置文件准备
在C盘跟目录参加 `ssh-tunnel`
在该目录下参加 `.ssh-tunnel` 目录， 并写入 `config.properties`
配置文件完整路径: `C:\ssh-tunnel\.ssh-tunnel\config.properties`

```properties
server.ip=xx.xx.xx.xx
server.ssh.port=22
ssh.private_key_path=C:\\Users\\idefav\\.ssh\\id_rsa
login.username=root
local.address=127.0.0.1:1081
http.local.address=127.0.0.1:1082
http.enable=false
socks5.enable=true
http.over-ssh.enable=false
http.domain-filter.enable=false
http.domain-filter.file-path=C:\\Users\\idefav\\Documents\\ssh-tunnel\\domain.txt
admin.enable=true
admin.address=127.0.0.1:1083

# 自动更新配置
auto-update.enabled=true
auto-update.owner=idefav
auto-update.repo=ssh-tunnel
auto-update.current-version=v0.0.0
auto-update.check-interval=3600
```

### 2. 安装windows服务

```text
 C:\ssh-tunnel\ssh-tunnel-svc.exe install --config=C:\ssh-tunnel\.ssh-tunnel\config.properties
```

### 3. 查看服务
win+r 输入 services.svc 打开服务管理窗口
找到 SSHTunnelService 并启动它

### 4. 在windows配置中启动代理

### 最近更新 🆕

#### 2026-08-28 (v1.7.0)
- ✅ Profile 列表新增可用、异常、不通、未知、测试中五种状态
- ✅ 统计最近 24 小时真实访问成功率、平均/P95 建连延迟、连续失败和最近错误
- ✅ 支持单节点/全部节点手测，分别展示 SSH 握手和出口探测耗时
- ✅ 新增三个管理 API，健康数据持久化到现有 `traffic.db`

#### 2026-08-28 (v1.6.1)
- ✅ 路由页新增跨组多选、组内/全页筛选结果全选及半选状态
- ✅ 支持批量迁移到现有组或原子创建新组、切换继承方式、统一独立出口及批量启停
- ✅ 新增 `POST /admin/routes/batch`，整批校验和原子写入后仅热加载一次
- ✅ `routes.json` 保持版本 2，旧配置无需再次迁移

#### 2026-08-28 (v1.6.0)
- ✅ 路由升级为可折叠规则组，支持名称、说明、组级开关和默认固定/随机出口
- ✅ 单规则可继承组出口，也可独立覆盖策略与多个目标 Profile，并支持跨组移动
- ✅ v1 平铺路由和旧 Profile 路由自动、幂等迁入版本 2 `routes.json`
- ✅ 请求追踪显示规则组，Profile 引用保护和后台连接池热加载同步覆盖组出口

#### 2026-08-27 (v1.5.0)
- ✅ 域名/IP/CIDR 路由拆分为独立页面、`routes.json` 与 API，支持固定、随机多目标、启停和实时热加载
- ✅ 启用规则按需维护后台 Profile SSH 池，随机目标全部失败时明确失败且不回退默认出口
- ✅ 新增 bbolt 持久化总体/Profile/直连流量累计、历史聚合与整体/单 Profile 重置
- ✅ 实时请求增加路由 ID、策略和已尝试 Profile，可核对实际出口与故障转移

#### 2026-04-12 (v1.4.20~v1.4.21)
- ✅ SSH 连接池管理页新增「清理已摘除」按钮，一键清空摘除历史
- ✅ 连接池成员列表支持手动摘除指定活跃成员
- ✅ 新增 API：`POST /admin/ssh/pool/clear-evicted`、`POST /admin/ssh/pool/evict-member`
- ✅ 修复 Linux/macOS 自动升级后新版本缺少可执行权限（chmod 0755）

#### 2026-04-11 (v1.4.17~v1.4.19)
- ✅ Profile 域名路由：按域名/IP/CIDR 路由到指定 SSH 隧道
- ✅ 每个 Profile 维护独立 SSH 连接池，管理页实时查看成员状态
- ✅ 安装脚本 SSH 密钥自动发现与无密码配置向导（Unix/Windows）
- ✅ 修复服务模式下 `--config=` 参数被重复传入的问题

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
- [Profile 节点质量](docs/features/profile-health-v1.7.md) - 24 小时真实访问统计、状态与手动测试
- [多平台部署](docs/setup/MULTIPLATFORM_SERVICE_SETUP.md) - Windows/macOS/Linux服务部署
- [测试脚本使用](scripts/test/README.md) - 测试脚本使用说明
- [日志清理功能](docs/features/) - 日志文件内容清理功能 🆕
