# 多 SSH Profile 动态切换技术方案

## 1. 背景与目标

当前系统以单一配置驱动 SSH 隧道运行，在多环境（开发/测试/生产）、多线路（主备出口）场景下，存在切换成本高、人工误操作风险大、回滚不可控等问题。

本方案目标：
- 支持维护多份 SSH 连接配置（Profile）。
- 支持在运行时动态切换激活 Profile。
- 切换过程可观测、可回滚、可审计。
- 与现有模块保持兼容，优先最小改造落地。

### 当前实现进展（2026-02）
- 已实现 `profiles` + `activeProfileId` 存储模型，并支持从 `profiles.json` 文件读取。
- 已实现 Profile 管理 API：列表、保存/更新、切换、切换状态、删除。
- 已实现管理页 Profile 弹窗（新增/编辑/复制），并支持 Profile ID 重复校验。
- 已实现切换后真实重连：刷新运行时配置并强制断开旧 SSH 后按新 profile 重连。
- 已实现 `重新连接 SSH` 按钮读取最新配置并应用 active profile 后重连。
- 已实现 `profiles.json` 文件美化写入，提升人工维护可读性。

---

## 2. 当前架构分析（现状）

### 2.1 配置层
- `cfg/cfg.go` 当前采用单实例 `AppConfig` 模型，字段为扁平结构。
- `viper` 热更新能力存在，但语义是“更新当前配置”，不是“切换配置集合中的一份”。

### 2.2 隧道层
- `tunnel/config.go` 通过 `Load(config, wg)` 初始化全局 `DefaultSshTunnel`。
- `tunnel/tunnel.go` 与 `tunnel/reconnect.go` 针对单连接状态设计，缺少 Profile 维度运行态管理。

### 2.3 管理接口与视图
- `api/admin/admin.go` 已有配置更新、服务重启、重连等接口。
- `views/handler/admin.go` 可展示并编辑配置，但不支持 Profile 列表/激活切换。

### 2.4 启动入口
- `main.go` 与 `service/main/main.go` 分别用于交互模式和服务模式。
- 两者都依赖单配置加载流程。

---

## 3. 方案总览

### 3.1 MVP（推荐先落地）
- 保持“单活运行”：任意时刻仅一个 Active Profile 生效。
- 引入 Profile 存储模型（`profiles` + `activeProfileId`）。
- 增加切换事务：校验 -> 预连接 -> 提交切换 -> 失败回滚。
- 管理端新增 Profile CRUD 与切换 API。

### 3.2 增强版（后续）
- 增加切换审计历史、变更审批、计划切换（定时）。
- 支持 Active + Standby 预热连接，进一步降低切换抖动。
- 指标看板与告警联动（切换成功率、回滚次数、耗时分位）。

---

## 4. 配置数据模型设计

建议内部统一模型：

```json
{
  "configVersion": 2,
  "activeProfileId": "prod-main",
  "profiles": [
    {
      "id": "prod-main",
      "name": "生产主链路",
      "enabled": true,
      "ssh": {
        "serverIp": "10.0.0.10",
        "serverSshPort": 22,
        "loginUser": "root",
        "sshPrivateKeyPath": "C:\\Users\\ops\\.ssh\\id_rsa"
      },
      "proxy": {
        "enableSocks5": true,
        "localAddress": "0.0.0.0:1081",
        "enableHttp": true,
        "httpLocalAddress": "0.0.0.0:1082",
        "enableHttpOverSSH": true
      },
      "auth": {
        "httpBasicAuthEnable": false,
        "httpBasicUserName": "",
        "httpBasicPassword": ""
      },
      "filter": {
        "enableHttpDomainFilter": false,
        "httpDomainFilterFilePath": "C:\\ssh-tunnel\\.ssh-tunnel\\domain.txt"
      },
      "runtime": {
        "retryIntervalSec": 3,
        "logFilePath": "C:\\ssh-tunnel\\.ssh-tunnel\\console.log"
      }
    }
  ]
}
```

约束：
- `activeProfileId` 必须指向 `enabled=true` 的 Profile。
- `id` 全局唯一，建议仅允许 `[a-z0-9-]`。
- 同实例中本地监听端口冲突应在切换前校验。

---

## 5. 配置存储与迁移策略

### 5.1 存储策略（建议）
- 方案 A（MVP）：新增 `profiles.json` 存多 Profile，`config.properties` 保留为当前激活配置镜像。
- 方案 B（兼容优先）：继续用 `config.properties`，采用 `profiles.<id>.*` 命名空间键。

推荐优先方案 A，迁移与回滚更清晰。

### 5.2 迁移策略（v1 -> v2）
1. 启动检测 `configVersion`。
2. 若是旧版单配置：自动生成 `default` Profile。
3. 设置 `activeProfileId=default`。
4. 备份旧配置文件（如 `config.properties.bak.v1`）。

---

## 6. API 设计

新增接口（基于 `api/admin`）：

### 6.1 列表与详情
- `GET /admin/profiles`
- `GET /admin/profiles/{id}`

### 6.2 新增/更新/删除
- `POST /admin/profiles`
- `PUT /admin/profiles/{id}`
- `DELETE /admin/profiles/{id}`

### 6.3 切换与状态
- `POST /admin/profiles/{id}/switch`
- `GET /admin/profiles/switch/status?switchId=...`

切换请求示例：

```json
{
  "targetProfileId": "test-a",
  "operator": "admin-ui",
  "reason": "联调切换"
}
```

切换响应示例：

```json
{
  "success": true,
  "switchId": "sw_20260225_103000_001",
  "fromProfileId": "prod-main",
  "toProfileId": "test-a",
  "status": "COMPLETED",
  "durationMs": 420
}
```

---

## 7. 动态切换时序与状态机

状态机：
- `IDLE`
- `VALIDATING`
- `PRECONNECTING`
- `REBINDING`
- `VERIFYING`
- `COMPLETED`
- `ROLLING_BACK`
- `ROLLED_BACK`
- `FAILED`

时序（简化）：
1. 获取全局切换锁。
2. 校验目标 Profile（字段/密钥/端口）。
3. 预连接目标 SSH（不切流）。
4. 原子替换运行时配置快照。
5. 重绑/重启相关连接。
6. 健康检查通过后提交。
7. 任一阶段失败则回滚旧 Profile。

---

## 8. 并发控制与一致性

- 切换全局互斥：同一时刻仅允许一个切换事务。
- `switch` 与 `reconnect` 协同锁：切换期间抑制自动重连抢占。
- 配置持久化采用“临时文件 + 原子替换”。
- 对外状态读取统一返回同一 `revision` 快照。

---

## 9. 回滚与故障处理

触发条件：
- Profile 校验失败。
- 预连接失败。
- 监听绑定失败。
- 切换后健康检查失败。

回滚动作：
1. 恢复旧 `activeProfileId` 与旧配置快照。
2. 重新加载旧配置并重建连接。
3. 记录失败事件（errorCode、errorMessage、switchId）。

---

## 10. 安全与权限考虑

- 切换接口需管理员权限。
- 敏感字段（密码、密钥内容）仅存引用，返回脱敏值。
- 切换操作写审计日志：操作者、来源 IP、时间、结果。
- 防误操作：禁止删除当前 Active Profile；禁止全部 Profile 置为 disabled。

---

## 11. 代码改动清单（按文件）

建议改动：
- `cfg/cfg.go`：新增 Profile 模型、读写与迁移逻辑。
- `tunnel/config.go`：支持按 Active Profile 构建运行参数。
- `tunnel/tunnel.go`：运行时快照管理与切换控制点。
- `tunnel/reconnect.go`：重连与切换锁协同。
- `api/admin/admin.go`：新增 Profile 管理与切换 API。
- `views/handler/admin.go`：新增 Profile 数据展示与切换入口。
- `main.go`：启动时迁移检查并加载 Active Profile。
- `service/main/main.go`：服务模式同样使用 Active Profile 启动流程。

---

## 12. 实施计划（里程碑）

### M1（1 周）
- 配置模型扩展 + 迁移能力。

### M2（1~1.5 周）
- Profile CRUD + Switch API + 回滚机制。

### M3（0.5~1 周）
- 管理页面改造 + 状态展示。

### M4（0.5 周）
- 联调、灰度、回归与发布文档。

---

## 13. 测试与验收标准

测试范围：
- 单元测试：模型校验、状态机转移、幂等行为。
- 集成测试：成功切换、失败回滚、并发冲突。
- 回归测试：旧配置兼容、现有接口不退化。

验收建议：
- 切换成功率 >= 99%（可达网络前提）。
- 切换平均耗时 <= 2 秒（同网络条件）。
- 回滚可在 <= 3 秒恢复旧链路可用。
- 审计日志可完整追踪每次切换。

---

## 14. 风险与缓解

- 风险：历史键名不一致导致迁移遗漏。  
  缓解：建立统一 key 映射表 + 迁移前备份。

- 风险：切换与重连并发冲突。  
  缓解：统一锁顺序与状态机门禁。

- 风险：端口占用导致切换失败。  
  缓解：切换前端口预检 + 失败快速回滚。

- 风险：服务模式与交互模式行为不一致。  
  缓解：抽取共享切换流程，双入口复用。

---

## 15. 总结

本方案在保持现有架构稳定的前提下，引入多 Profile 管理与动态切换能力，核心价值在于：
- 降低人工运维成本；
- 提升故障切换效率；
- 提供可追溯、可恢复的配置变更路径。

建议按 MVP 先落地，再逐步增强为可审计、可灰度、可观测的生产级能力。
