# v1.7.0 Profile 节点质量统计与手动测试

v1.7.0 在应用配置页的 SSH Profile 管理表格中增加节点状态和最近 24 小时访问质量。统计来自真实代理请求的目标建连结果，并与按需执行的 SSH/出口测试分开展示。

## 指标含义

- **访问建连延迟**：HTTP、HTTPS 或 SOCKS5 请求经指定 Profile 建立目标 TCP 连接的耗时。平均值和近似 P95 只计算成功访问。
- **SSH 握手延迟**：点击“测试”后，使用 Profile 配置建立一次性 SSH 连接的耗时。
- **出口探测延迟**：SSH 握手成功后，通过该连接访问 `ssh.probe.urls`、兼容的 `ssh.probe.url` 或内置默认地址的耗时。

这些指标不是纯网络 RTT。直连请求不计入 Profile 健康统计；随机路由发生故障转移时，每个实际尝试的候选 Profile 会分别记录一次。

## 状态

| 状态 | 含义 |
|------|------|
| `testing` | 当前正在执行手动测试 |
| `reachable` | 最新有效结果成功 |
| `degraded` | 被动访问出现连续失败，但尚未达到 `ssh.probe.failure.threshold` |
| `unreachable` | 被动失败达到阈值，或最近一次手动测试失败 |
| `unknown` | 没有样本，或最近结果已超过 24 小时 |

页面正常情况下每 5 秒刷新，测试批次运行时每秒刷新逐节点进度。错误内容会在表格中截断显示，将鼠标停留在错误上可查看完整信息。

## 手动测试

- 每行“测试”只测试当前 Profile；“测试全部”对列表中的全部 Profile 建立测试批次。
- 一个批次最多并发测试 3 个节点；全局同时只允许一个批次。
- 测试连接完成后立即关闭，不切换活动 Profile，不进入连接池，也不计入流量或被动成功率。
- 手动结果包含 SSH 握手、出口探测、目标地址、错误以及开始/完成时间。

## API

```http
GET /admin/profiles/health

POST /admin/profiles/test
Content-Type: application/json

{"profileIds":["jp","us"]}

GET /admin/profiles/test/status?testId=pt_...
```

启动测试成功返回 HTTP 202；空列表或未知 Profile 返回 400；已有批次运行时返回 409。完整字段见 [配置 API](../config-api.md)。

## 数据与升级

健康数据使用现有配置目录中的 `traffic.db`：

- 分钟桶保留最近 24 小时的成功/失败次数、成功延迟总和和固定延迟直方图。
- 最新状态记录连续失败、最近目标、失败分类、错误和最后手动测试。
- Profile 的服务器、SSH 端口、用户或私钥路径改变时自动清除旧节点数据；删除 Profile 时同步清除。

升级不修改 `profiles.json` 或 `routes.json`，无需迁移配置文件。首次启动 v1.7.0 时会自动创建新的 bbolt 桶。
