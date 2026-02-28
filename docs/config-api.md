# SSH隧道配置API文档

## 概述

SSH隧道应用提供了Web管理界面和配置API，允许用户通过浏览器界面管理配置、查看状态和控制服务。

## 页面路由

### 管理界面路由

| 路由 | 方法 | 描述 |
|------|------|------|
| `/view/index` | GET | 主页面 - 显示隧道状态和域名缓存 |
| `/view/app/config` | GET | 配置管理页面 - 显示和编辑应用配置 |
| `/view/logs` | GET | 日志查看页面 - 实时查看应用日志 |

### API接口

#### 配置管理API

| 接口 | 方法 | 描述 | 参数 |
|------|------|------|------|
| `/admin/config/update` | POST | 更新配置项 | `key`, `value`, `type` |
| `/admin/config/cleanup` | POST | 清理重复配置 | 无 |

#### Profile管理API 🆕

| 接口 | 方法 | 描述 | 参数 |
|------|------|------|------|
| `/admin/profiles` | GET | 获取 Profile 列表与当前激活 Profile | 无 |
| `/admin/profiles/upsert` | POST | 新增或更新 Profile | JSON: `profileId`, `profile` |
| `/admin/profiles/switch` | POST | 切换当前激活 Profile 并触发重连 | JSON: `targetProfileId` |
| `/admin/profiles/switch/status` | GET | 查询最近一次或指定 switchId 的切换状态 | Query: `switchId`(可选) |
| `/admin/profiles/delete` | POST | 删除指定 Profile（不可删除当前激活） | JSON: `profileId` |

> 说明：Profile 保存后会同时写入配置键 `profiles.json` 与文件 `profiles.json`（美化格式，便于人工查看和维护）。

#### SSH连接API 🆕

| 接口 | 方法 | 描述 | 返回 |
|------|------|------|------|
| `/admin/ssh/state` | GET | 获取当前 SSH 客户端状态 | `version/localAddr/remoteAddr/sessionId/user` |
| `/admin/ssh/reconnect` | POST | 使用最新配置执行真实 SSH 重连 | 成功返回新的 SSH 会话信息 |
| `/admin/ssh/metrics` | GET | 获取当前 SSH 代理实时上下行速率与累计流量 | `uploadSpeed/downloadSpeed/uploadBytesTotal/downloadBytesTotal` |
| `/admin/ssh/test` | POST | 执行 SSH 延迟与当前速率测试 | `latencyMs/uploadSpeed/downloadSpeed` |

`/admin/ssh/reconnect` 的执行顺序：
1. 重载配置文件
2. 应用 active profile（`profiles.json`）
3. 刷新隧道运行时参数（地址/端口/用户/私钥认证）
4. 强制断开旧连接
5. 立即建立新连接并返回新会话信息

#### 服务控制API

| 接口 | 方法 | 描述 | 返回 |
|------|------|------|------|
| `/admin/service/restart` | POST | 重启服务 | `{success: boolean, message: string}` |
| `/admin/service/mode` | GET | 获取运行模式 | `{success: boolean, canRestart: boolean, message: string}` |

## 配置管理页面数据结构

### AppConfig 结构

```go
type AppConfig struct {
    ConfigFilePath   string                     // 配置文件路径
    Config           map[string]interface{}     // 配置项键值对
    ConfigMeta       map[string]ConfigMetadata  // 配置项元数据
    ConfigKeys       map[string]string          // 实际配置键映射
    ExecutablePath   string                     // 程序执行路径
    WorkingDirectory string                     // 当前工作目录
}
```

### ConfigMetadata 结构

```go
type ConfigMetadata struct {
    Type        string `json:"type"`        // 数据类型: string/int/bool
    Description string `json:"description"` // 配置项描述
    Category    string `json:"category"`    // 配置分类
    Required    bool   `json:"required"`    // 是否必需
    ActualKey   string `json:"actualKey"`   // 实际配置键名
}
```

### ProfileStore 结构 🆕

```go
type ProfileStore struct {
    ActiveProfileID string                `json:"activeProfileId"` // 当前激活 Profile ID
    Profiles        map[string]SSHProfile `json:"profiles"`        // 全部 Profile
}

type SSHProfile struct {
    ServerIp                 string `json:"serverIp"`
    ServerSshPort            int    `json:"serverSshPort"`
    LoginUser                string `json:"loginUser"`
    SshPrivateKeyPath        string `json:"sshPrivateKeyPath"`
    LocalAddress             string `json:"localAddress"`
    HttpLocalAddress         string `json:"httpLocalAddress"`
    EnableHttp               bool   `json:"enableHttp"`
    EnableSocks5             bool   `json:"enableSocks5"`
    EnableHttpOverSSH        bool   `json:"enableHttpOverSSH"`
    HttpBasicAuthEnable      bool   `json:"httpBasicAuthEnable"`
    HttpBasicUserName        string `json:"httpBasicUserName"`
    HttpBasicPassword        string `json:"httpBasicPassword"`
    EnableHttpDomainFilter   bool   `json:"enableHttpDomainFilter"`
    HttpDomainFilterFilePath string `json:"httpDomainFilterFilePath"`
    RetryIntervalSec         int    `json:"retryIntervalSec"`
}
```

## 进程信息功能

### 数据获取

进程信息在页面加载时通过以下Go函数获取：

```go
// 获取程序执行路径
func getExecutablePath() string {
    executable, err := os.Executable()
    if err != nil {
        return "无法获取程序路径: " + err.Error()
    }
    realPath, err := filepath.EvalSymlinks(executable)
    if err != nil {
        return executable
    }
    return realPath
}

// 获取当前工作目录
func getWorkingDirectory() string {
    workDir, err := os.Getwd()
    if err != nil {
        return "无法获取工作目录: " + err.Error()
    }
    return workDir
}
```

### 显示字段

| 字段 | 类型 | 描述 | 示例 |
|------|------|------|------|
| `ExecutablePath` | string | 程序二进制文件的完整路径 | `/usr/local/bin/ssh-tunnel` |
| `WorkingDirectory` | string | 程序运行时的当前工作目录 | `/home/user/ssh-tunnel` |

## 配置项分类

### 服务器配置
- `ServerIp` - SSH服务器IP地址
- `ServerSshPort` - SSH服务器端口
- `LoginUser` - SSH登录用户名

### SSH配置
- `SshPrivateKeyPath` - SSH私钥文件路径


### 代理配置
- `LocalAddress` - 本地SOCKS5代理监听地址
- `HttpLocalAddress` - 本地HTTP代理监听地址
- `EnableHttp` - 启用HTTP代理
- `EnableSocks5` - 启用SOCKS5代理
- `EnableHttpOverSSH` - 启用HTTP Over SSH

### 认证配置
- `HttpBasicAuthEnable` - 启用HTTP Basic认证
- `HttpBasicUserName` - HTTP Basic认证用户名
- `HttpBasicPassword` - HTTP Basic认证密码

### 过滤配置
- `EnableHttpDomainFilter` - 启用域名过滤
- `HttpDomainFilterFilePath` - 域名过滤文件路径

### 管理配置
- `EnableAdmin` - 启用管理界面
- `AdminAddress` - 管理界面监听地址

### 高级配置
- `RetryIntervalSec` - 连接重试间隔(秒)
- `SSHDialTimeoutSec` - SSH握手超时(秒)
- `SSHDestDialTimeoutSec` - SSH目标连接超时(秒)
- `SSHKeepAliveIntervalSec` - SSH保活间隔(秒)
- `SSHKeepAliveCountMax` - SSH保活最大连续失败次数
- `SSHReconnectMaxRetries` - SSH重连最大重试次数
- `SSHReconnectMaxIntervalSec` - SSH重连最大退避间隔(秒)
- `LogFilePath` - 日志文件路径
- `HomeDir` - 应用主目录

## 快速重连推荐配置

```properties
retry.interval.sec=1
ssh.dial.timeout.sec=5
ssh.dest.dial.timeout.sec=3
ssh.keepalive.interval.sec=2
ssh.keepalive.count.max=2
ssh.reconnect.max.retries=20
ssh.reconnect.max.interval.sec=5
```

上述组合可在 SSH 链路失效后更快触发重连并恢复代理能力，适用于“运行一段时间后大量 `context deadline exceeded`”场景。

## 配置更新流程

### 1. 前端操作
1. 用户点击配置项的编辑按钮
2. 弹出编辑对话框，显示当前值和配置元数据
3. 用户修改配置值并提交

### 2. API调用
```javascript
fetch('/admin/config/update', {
    method: 'POST',
    body: formData // 包含 key, value, type
})
```

### 3. 后端处理
1. 接收配置更新请求
2. 验证配置键和值的有效性
3. 更新配置文件
4. 返回操作结果

### 4. 响应处理
```json
{
    "success": true,
    "message": "配置更新成功！"
}
```

## 错误处理

### 配置更新错误
- 无效的配置键
- 类型转换失败
- 文件写入权限不足

### 进程信息获取错误
- 程序路径获取失败
- 工作目录获取失败
- 符号链接解析失败

### 服务控制错误
- 服务不存在
- 权限不足
- 系统命令执行失败

## 安全考虑

### 1. 认证机制
- 管理界面需要启用才能访问
- 可配置管理界面监听地址

### 2. 配置验证
- 严格验证配置键的有效性
- 类型检查和范围验证
- 敏感信息处理

### 3. 权限控制
- 文件系统访问权限检查
- 服务控制权限验证
- 网络接口绑定限制

## 使用示例

### 获取配置页面
```bash
curl http://localhost:1083/view/app/config
```

### 更新配置项
```bash
curl -X POST http://localhost:1083/admin/config/update \
  -F "key=ServerIp" \
  -F "value=192.168.1.100" \
  -F "type=string"
```

### 重启服务
```bash
curl -X POST http://localhost:1083/admin/service/restart \
  -H "Content-Type: application/json"
```

### 检查运行模式
```bash
curl http://localhost:1083/admin/service/mode
```

### 获取 Profile 列表
```bash
curl http://localhost:1083/admin/profiles
```

### 保存 Profile
```bash
curl -X POST http://localhost:1083/admin/profiles/upsert \
    -H "Content-Type: application/json" \
    -d '{
        "profileId": "prod",
        "profile": {
            "serverIp": "1.2.3.4",
            "serverSshPort": 22,
            "loginUser": "root",
            "sshPrivateKeyPath": "C:/Users/test/.ssh/id_rsa",
            "localAddress": "0.0.0.0:1081",
            "httpLocalAddress": "0.0.0.0:1082",
            "enableSocks5": true,
            "enableHttp": true,
            "retryIntervalSec": 5
        }
    }'
```

### 切换 Profile
```bash
curl -X POST http://localhost:1083/admin/profiles/switch \
    -H "Content-Type: application/json" \
    -d '{"targetProfileId":"prod"}'
```

### 查询切换状态
```bash
curl http://localhost:1083/admin/profiles/switch/status
```

### 查询指定切换任务状态
```bash
curl "http://localhost:1083/admin/profiles/switch/status?switchId=sw_1730000000000000000"
```

### 删除 Profile
```bash
curl -X POST http://localhost:1083/admin/profiles/delete \
    -H "Content-Type: application/json" \
    -d '{"profileId":"prod"}'
```

### 重新连接 SSH（读取最新配置）
```bash
curl -X POST http://localhost:1083/admin/ssh/reconnect
```

## 相关文档

- [进程信息功能](features/process-info-feature.md) - 进程信息显示功能详细说明
- [服务重启功能](features/restart-service-feature.md) - 服务重启功能详细说明
- [多 SSH Profile 动态切换方案](features/multi-profile-switch-design.md) - 多 Profile 功能设计与实现计划
- [多平台部署](setup/MULTIPLATFORM_SERVICE_SETUP.md) - 部署配置指南
