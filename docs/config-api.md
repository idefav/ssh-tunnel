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
- `SshKnownHostsPath` - SSH已知主机文件路径

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
- `LogFilePath` - 日志文件路径
- `HomeDir` - 应用主目录

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

## 相关文档

- [进程信息功能](features/process-info-feature.md) - 进程信息显示功能详细说明
- [服务重启功能](features/restart-service-feature.md) - 服务重启功能详细说明
- [多平台部署](setup/MULTIPLATFORM_SERVICE_SETUP.md) - 部署配置指南
