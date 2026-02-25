# Project Guidelines

## Code Style
- 语言以 Go 为主，按目录划分模块：`cfg/`、`tunnel/`、`api/`、`views/`、`updater/`。
- 并发优先使用 `safe.GO(...)`，避免裸 `go`：参考 `safe/safe.go`、`tunnel/config.go`。
- 错误处理以“早返回 + 日志”模式为主，入口层使用 panic 恢复：参考 `main.go`、`service/main/main.go`。
- 配置键统一在 `cfg/cfg.go` 常量区维护，新增键必须同步映射与管理端展示。
- 变更应保持最小侵入，优先复用现有函数与数据结构。

## Architecture
- 启动主链路：`main.go` / `service/main/main.go` -> `cfg`(viper) -> `tunnel.Load` -> `admin.Load`。
- 配置模型由 `cfg.AppConfig` 管理，运行时更新通过 `AppConfig.Update()` 同步。
- 隧道核心在 `tunnel/`：SSH 建链、SOCKS5/HTTP 代理、域名过滤、自动重连。
- 管理接口在 `api/admin/admin.go`，页面渲染在 `views/handler/admin.go`。
- 静态与模板资源通过 `views/init.go` embed 打包。

## Build and Test
- Windows 快速构建：`go build -o ssh-tunnel.exe .`
- 跨平台构建：`./scripts/build.sh` 或 `./scripts/build.ps1`
- 发布构建：`./scripts/build-release.ps1 -Version vX.Y.Z -OutputDir ./release`
- 单元测试：`go test -v ./...`
- VS Code 任务可用：`Build SSH Tunnel`（`go build -o ssh-tunnel.exe .`）。

## Project Conventions
- **保留原有约定**：
	1. md 文件统一放到 `docs` 文件夹。
	2. 脚本文件放到 `scripts` 文件夹。
	3. 功能更新后，需要同时更新根 `README.md` 和 GitHub docs（`docs/` 下相关文档）。
- 新增功能文档后，更新文档索引：`docs/README.md`、`docs/INDEX.md`。
- 参数兼容依赖 `wordSepNormailzeFunc`（`-`/`_` -> `.`），新增 CLI 键保持兼容语义。
- 涉及配置字段变更时，需同步：`cfg/cfg.go`、`api/admin/admin.go`、`views/handler/admin.go`。

## Integration Points
- SSH：`golang.org/x/crypto/ssh`（见 `tunnel/config.go`、`tunnel/tunnel.go`）。
- 配置与参数：`viper + pflag + fsnotify`（见 `main.go`、`service/main/main.go`）。
- 服务管理：`github.com/kardianos/service`（见 `service/main/main.go`）。
- 管理路由：`gorilla/mux`（见 `api/admin/admin.go`、`router/admin.go`）。
- 自动更新：GitHub Release（见 `updater/*.go`、`views/handler/admin.go`）。

## Security
- 私钥与密码字段属于敏感信息，禁止写入明文日志。
- 管理接口具高权限（配置更新/重启/切换），修改时优先限制监听范围与访问边界。
- 当前 SSH 主机校验使用 `ssh.InsecureIgnoreHostKey()`（`tunnel/config.go`），涉及该处改动需明确安全影响。
- 执行系统命令的逻辑（service restart/update）需保留最小权限原则并补充失败回滚路径。
