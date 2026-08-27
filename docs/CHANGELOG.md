# 更新日志

## v1.5.0 (2026-08-27)

### 🆕 新功能
- 域名/IP/CIDR 路由从 Profile 拆分为独立 `routes.json`、管理页面和 CRUD/启停 API。
- 路由支持固定目标以及随机多 Profile 目标；每个新连接重新随机排序并逐个故障转移，全部失败时不回退默认 Profile。
- 后台 Profile SSH 池根据启用规则实时增删；Profile 连接参数变化时重建，存在规则引用时禁止删除。
- 使用 `go.etcd.io/bbolt v1.4.3` 持久化总体、Profile 和直连流量，提供实时速度、累计、365 天小时历史、日/月聚合及重置 API。
- SSH 状态页增加流量明细、历史图表和二次确认重置；请求追踪增加路由 ID、策略和已尝试 Profile。

### 🔄 迁移
- 首次启动自动将旧 `SSHProfile.domainRoutes` 迁移为确定 ID 的独立规则，成功写入后清理旧字段，重复启动保持幂等。

### ⚠️ 升级说明
- 升级前建议备份活动配置文件和 `profiles.json`；迁移完成后降级到 v1.4.x 时，需同时恢复旧 Profile 备份。
- 多个 Profile 中存在同类型、同规范化匹配模式时，迁移会拒绝启动并报告冲突，不会静默改变路由语义。
- `traffic.db` 从 v1.5.0 首次启动后开始累计，旧版本的内存流量无法补录。同一配置目录不能被多个进程同时打开。
- `auto-update.enabled` 只开启定时版本检查；下载和安装需在 `/view/version` 人工确认。

## v1.4.22 (2026-04-12)

### 📝 文档
- **README 更新**：补充 SSH 连接池参数说明（`ssh.pool.*`、`ssh.probe.*`、`proxy.retry.*`）及管理功能介绍。
- **CHANGELOG 补全**：补充 v1.4.20 / v1.4.21 版本变更记录。
- **HTML 文档更新**：同步更新 `docs/index.html`、`docs/documentation.html`、`docs/faq.html`，新增连接池管理、摘除成员、自动升级权限修复等内容。
- **文档索引更新**：`docs/INDEX.md` 新增 v1.4.22 文档索引条目。

## v1.4.21 (2026-04-12)

### 🐛 修复
- **自动升级权限修复**：修复 Linux/macOS 平台自动升级下载新版本二进制后缺少可执行权限（`0644`）导致无法启动的问题，下载完成后自动 `chmod 0755`。

## v1.4.20 (2026-04-12)

### 🆕 新功能
- **一键清理已摘除成员**：SSH 连接池管理页新增「清理已摘除」按钮，当存在已摘除成员时自动显示，一键清空摘除历史记录。
- **手动摘除指定成员**：SSH 连接池成员列表中，正常/可疑/探测中成员支持手动点击摘除，立即从活跃池中移除并记录到摘除历史。
- **新增 API 端点**：`POST /admin/ssh/pool/clear-evicted`（清理已摘除成员）、`POST /admin/ssh/pool/evict-member?id=<N>`（手动摘除指定成员）。

## v1.4.19 (2026-04-11)

### 🐛 修复
- 修复服务模式下通过 `--config=` 指定配置文件时，参数会被重复传入后续 flag 解析的问题。
- 调整启动参数过滤逻辑，保留自定义配置路径读取，同时避免影响其他命令行参数解析。

## v1.4.18 (2026-04-11)

### 🆕 新功能
- **Profile 域名路由**：支持为独立 Profile 配置域名、IP、CIDR 路由规则，命中的请求自动走对应 SSH 隧道。
- **按 Profile 独立连接池**：每个路由 Profile 可维护独立 SSH 连接池，并支持在管理页实时查看成员状态。
- **浏览器应用安装支持**：新增站点图标、Web App Manifest 与 Service Worker，可在浏览器中安装为独立应用。

### 🔧 技术改进
- 主隧道与路由隧道统一切换为 SSH 连接池模型，补充成员探测、摘除、重试与全局成员 ID。
- Profile 的 `sshPoolSize` 支持在管理页保存后即时生效，增大自动补池，减小时即时收缩空闲成员。
- SSH 状态页新增连接池成员表、请求来源 Profile 展示、手动探测入口与更多诊断指标。
- 日志页优化为默认加载最近 1000 行历史并继续追踪，降低前端渲染压力。

### 🐛 修复
- 去除 profiles 文件高频加载日志，避免控制台持续刷屏。
- 修复手动探测按钮无响应的问题。
- 修复路由请求在请求表中显示错误 Profile 的问题。
- 修复多隧道场景下连接池成员 ID 冲突的问题。

## v1.4.17 (2026-04-11)

### 🆕 新功能
- **安装脚本 SSH 密钥自动发现**：install-unix.sh 新增自动发现 `~/.ssh` 下已有密钥对，引导用户选择或生成新密钥，减少手动填写。
- **无密码登录配置向导**：新增交互式无密码配置流程，支持自动生成 SSH 密钥并完成 `ssh-copy-id` 操作。
- **Windows 安装脚本同步增强**：install-windows.ps1 同步实现 SSH 密钥发现与无密码配置功能。
- **Profile 表单默认值优化**：新建 Profile 时，表单以当前运行配置为默认值预填充，降低重复输入成本。

### 🔧 技术改进
- 提取 `applyProfileFormValues()` 函数，统一 create / edit / copy 三种 Profile 操作的表单赋值逻辑。
- 新增安装脚本单元测试：`test_install_unix_ssh_key_discovery.sh`、`test_install_unix_ssh_key_generation.sh`、`test_install_unix_passwordless_setup.sh`。

### 📚 文档更新
- 更新 `docs/installation.html`、`docs/ssh-key-setup.html`、`docs/setup/MULTIPLATFORM_SERVICE_SETUP.md`、根 `README.md`。

## v1.3.1 (2026-03-03)

### 🛠 稳定性修复
- 修复 SSH 保活监控并发计数问题，消除 `sync: negative WaitGroup counter` 风险。
- 收敛重连触发条件，避免单请求目标超时导致全局 SSH 重连。
- 修复 SOCKS5 错误分类：仅在 SSH 链路异常时触发重连。
- SSH状态页新增连接统计：当前SSH连接数、累计重连次数。
- SSH状态页新增“重连计数清零”操作。

### 🔍 行为说明
- 请求级错误（目标超时、拒绝连接）仅影响当前请求，不会直接触发 SSH 重连。
- SSH 链路级错误（连接关闭、通道异常、broken pipe）会触发自动重连。

### 📚 文档更新
- 新增 `docs/features/ssh-stability-fix-2026-03.md`
- 更新根 `README.md` 稳定性说明章节
- 更新 `docs/README.md`、`docs/INDEX.md` 索引

## v1.3.0 (2025-06-21)

### 🆕 新功能
- **进程信息显示**: 在配置管理页面新增进程信息显示区域
  - 显示程序执行路径 (可执行文件的完整路径)
  - 显示当前工作目录
  - 支持一键复制路径信息
  - 自动解析符号链接获取真实路径
  - 跨平台兼容 (Windows/macOS/Linux)

### 🔧 技术改进
- 扩展 `AppConfig` 结构体，新增 `ExecutablePath` 和 `WorkingDirectory` 字段
- 新增 `getExecutablePath()` 和 `getWorkingDirectory()` 辅助函数
- 增强配置页面的信息展示能力
- 改进错误处理机制

### 📚 文档更新
- 新增 [进程信息功能文档](docs/features/process-info-feature.md)
- 更新主 README.md，增加 AdminUI 功能说明
- 完善 [配置API文档](docs/config-api.md)
- 更新文档索引和导航链接

### 🎨 界面优化
- 采用统一的卡片布局设计
- 保持与现有配置文件路径展示的视觉一致性
- 响应式设计，支持移动端显示
- 复制操作的视觉反馈优化

### 🔍 用途场景
- **故障排查**: 快速确认程序运行位置和版本
- **部署管理**: 获取精确路径用于服务配置
- **开发调试**: 验证程序运行环境和路径配置
- **环境对比**: 在不同环境间对比程序运行状态

---

## v1.2.0 (之前版本)

### 🔄 服务重启功能
- 智能检测运行模式 (服务模式/直接运行模式)
- 支持多平台服务重启 (Windows/macOS/Linux)
- 安全确认对话框
- 重启状态反馈

### ⚙️ 配置管理增强
- 在线配置编辑
- 配置项分类和搜索
- 配置清理功能
- 实时配置验证

### 🌐 管理界面优化
- 现代化UI设计
- 响应式布局
- 实时状态监控
- 日志查看功能

---

## 发布说明

### 兼容性
- 向后兼容之前的配置文件格式
- 不影响现有API接口
- 无需额外的依赖项

### 升级说明
1. 直接替换可执行文件即可
2. 无需修改现有配置
3. 新功能自动生效

### 系统要求
- Go 1.16+ (开发环境)
- 支持 Windows 7+, macOS 10.12+, Linux (各主流发行版)
- 浏览器支持: Chrome 70+, Firefox 65+, Safari 12+

### 文件变更
```
修改的文件:
- views/handler/admin.go     (新增进程信息获取功能)
- views/app_config.gohtml    (新增进程信息显示界面)

新增的文件:
- docs/features/process-info-feature.md  (功能文档)
- docs/CHANGELOG.md                      (更新日志)

更新的文件:
- README.md                  (更新功能说明)
- docs/INDEX.md             (更新文档索引)
- docs/config-api.md        (完善API文档)
```

### 下一版本计划
- 🔮 配置模板功能
- 📊 性能监控面板
- 🔐 高级认证选项
- 📱 移动端应用支持
