# SSH连接稳定性修复（2026-03-03）

## 背景

在高并发代理请求和SSH链路波动场景下，存在以下问题：

1. `keepAliveMonitor` 与外部 `WaitGroup` 复用导致计数失衡，触发 `sync: negative WaitGroup counter`。
2. 单次请求的目标站点超时被误判为SSH链路故障，触发不必要的全局SSH重连。
3. SOCKS5链路将多类请求错误统一归类为网络错误，放大为重连风暴。

## 修复内容

### 1) 保活并发修复

- 将 `keepAliveMonitor` 改为内部自管理，不再依赖调用方传入 `WaitGroup`。
- 保留保活探测与 `client.Wait()` 监听能力，避免 `Done()` 与 `Add()` 跨调用栈失配。

### 2) 重连触发条件收敛

- 新增 SSH 链路级错误分类：仅在明确 SSH 通道异常时触发重连。
- 目标站点连接超时、拒绝等请求级失败仅返回当前请求失败，不再默认触发SSH重连。

### 3) SOCKS5 错误分类修复

- 当 SSH 客户端未就绪或检测到 SSH 通道异常时，返回 `SSHReconnectRequired`。
- 普通目标连接失败（例如目标站点不可达）不再触发全局重连。

### 4) 单连接生命周期保障

- 强制同一时刻仅保留一个活跃 SSH 连接。
- 新连接设置时会原子替换并立即关闭旧连接，避免长期运行后连接泄露。
- 保活监控绑定具体连接实例，旧监控协程不会误关闭新连接。

## 行为变化说明

- **会触发SSH重连**：`unexpected packet`、`ssh connection closed`、`broken pipe`、`use of closed network connection` 等 SSH 链路级错误。
- **不会触发SSH重连**：单个目标域名连接超时、目标拒绝连接、普通请求级失败。

## 参数建议

建议在网络波动场景使用如下配置：

```properties
ssh.dest.dial.timeout.sec=8
ssh.keepalive.interval.sec=5
ssh.keepalive.count.max=4
ssh.reconnect.max.retries=20
ssh.reconnect.max.interval.sec=5
```

说明：
- `ssh.dest.dial.timeout.sec` 适度提高可减少误判超时。
- `ssh.keepalive.interval.sec` 与 `ssh.keepalive.count.max` 共同决定链路失效判定速度与稳定性平衡。
