# v1.5.0 独立路由与流量统计 / v1.6.0 规则组 / v1.6.1 批量编辑

## 路由规则组

路由管理位于 `/view/routes`，规则保存于活动配置目录的 `routes.json`。未显式指定配置文件时，默认路径为 `~/.ssh-tunnel/routes.json`。当前格式为版本 2：多个相关域名、IP 或 CIDR 可以放进带名称和说明的规则组。

规则支持：

- `domain`：域名后缀和 `*.example.com` 通配域名。
- `ip`：IPv4 精确值或通配符，例如 `192.168.*`。
- `cidr`：IPv4/IPv6 CIDR，例如 `10.0.0.0/8`。
- 组默认出口：支持 `fixed` 或 `random`，供组内规则继承。
- 单规则独立出口：可覆盖组默认值为 `fixed` 或 `random`；`random` 会在每个新 TCP 连接重新打乱至少两个 Profile，按顺序尝试故障转移。所有目标失败时不回退默认 Profile。

组开关和规则开关共同决定最终是否生效；关闭组会保留每条规则原有的开关状态。有效规则引用的非激活 Profile 会自动维护后台 SSH 连接池。组默认出口、规则继承/覆盖、规则移动、启停、Profile 参数更新和激活 Profile 切换均会热加载。被任何规则组默认出口或单规则独立出口引用的 Profile 不能删除，即使对应组或规则已停用。

路由页支持跨组多选和批量编辑：可迁移到现有组或在同一次原子操作中新建组，批量切换继承方式、固化各自当前出口、统一独立出口及启用/停用。搜索和筛选后的选择在组折叠或筛选变化时仍会保留。

## 流量统计

SOCKS5、HTTP、HTTPS CONNECT、初始 HTTP 请求和长连接都在实际 `Read`/`Write` 时计量。数据包括：

- 所有代理流量总体。
- 每个实际使用的 Profile。
- 不经 SSH 的直连流量。
- 24 小时、7 天、30 天和最近 12 个月历史。

累计和最近 365 天的小时桶保存在配置目录下的 `traffic.db`。内存增量每 5 秒通过一个 bbolt 事务刷盘，正常停止时强制同步。数据库打开失败或已被另一个进程锁定时，程序会拒绝启动。

整体重置会清零总体、Profile 和直连累计；单 Profile 重置会同时从总体累计中扣除对应数值。重置不删除历史小时桶。

## 从 v1.4.x 升级

1. 备份配置文件和 `profiles.json`，然后停止旧进程。
2. 首次启动自动将旧 `domainRoutes` 生成为指向原 Profile 的启用 `fixed` 规则。
3. 如果多个 Profile 存在相同的规范化模式，启动会报迁移冲突；清理重复路由后重启即可继续。
4. 旧版本未持久化的流量无法迁移，v1.5.0 将从 0 开始累计。
5. 如需降级到 v1.4.x，同时恢复升级前的 Profile 备份；老版本不识别 `routes.json`。

## 从版本 1 平铺路由升级

版本 1 的 `routes.json` 会自动迁入确定 ID 的“未分组（自动迁移）”。组默认出口取第一条旧规则，但每条旧规则仍保留自己的策略和目标作为独立覆盖，因此升级不会改变原有出口。迁移先完成全量校验，再通过临时文件和原子替换写入；重复启动不会重复生成规则。降级到只支持版本 1 的程序前，需要恢复升级前的 `routes.json` 和 `profiles.json` 备份。

## API

规则组 API：`GET /admin/routes`、`POST /admin/route-groups/upsert`、`POST /admin/route-groups/toggle`、`POST /admin/route-groups/delete`。规则使用 `POST /admin/routes/upsert`、`POST /admin/routes/batch`、`POST /admin/routes/toggle`、`POST /admin/routes/delete`。

流量 API：`GET /admin/ssh/metrics`、`GET /admin/traffic/history`、`POST /admin/traffic/reset`。请求参数和返回结构见 [配置 API 文档](../config-api.md)。
