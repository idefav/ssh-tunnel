# 自动更新功能说明

SSH隧道工具现在支持自动更新功能，可以定时从GitHub Release检测新版本并支持自动下载和更新。

## 功能特性

- ✅ 定时检查GitHub Release新版本
- ✅ SHA256文件完整性校验
- ✅ 可视化版本管理界面
- ✅ 支持开启/关闭自动更新
- ✅ 自定义检查间隔时间
- ✅ 显示所有可用版本
- ✅ 手动下载和更新版本

## 配置说明

在配置文件中添加以下配置项：

```properties
# 自动更新配置
auto-update.enabled=true                    # 是否启用自动更新检查
auto-update.owner=idefav                    # GitHub仓库所有者
auto-update.repo=ssh-tunnel                 # GitHub仓库名称
auto-update.current-version=v1.0.0          # 当前版本号
auto-update.check-interval=3600             # 检查更新间隔(秒)，默认1小时
```

## 访问版本管理界面

1. 启动SSH隧道服务并启用管理面板：
   ```bash
   ./ssh-tunnel --admin.enable=true --admin.address=:1083
   ```

2. 在浏览器中访问：`http://localhost:1083/view/version`

## 版本管理界面功能

### 当前版本信息
- 显示当前安装的版本号
- 显示文件SHA256校验和
- 显示安装时间和文件大小
- 显示运行平台和架构信息

### 版本列表
- 显示GitHub上所有可用的Release版本
- 区分当前版本、最新版本和可更新版本
- 显示版本发布说明
- 显示下载文件和下载次数

### 更新设置
- 开启/关闭自动更新检查
- 设置检查间隔时间（5-1440分钟）
- 配置GitHub仓库信息
- 实时保存设置

### 手动操作
- 手动检查更新
- 下载指定版本
- 一键更新到最新版本

## API接口

### 检查更新
```
POST /api/version/check
```

### 下载版本
```
POST /api/version/download
Content-Type: application/x-www-form-urlencoded

version=v1.1.0&fileName=ssh-tunnel.exe&downloadUrl=https://...
```

### 更新到指定版本
```
POST /api/version/update
Content-Type: application/x-www-form-urlencoded

version=v1.1.0&fileName=ssh-tunnel.exe&downloadUrl=https://...
```

### 保存更新设置
```
POST /api/version/settings
Content-Type: application/json

{
  "enabled": true,
  "checkInterval": 60,
  "githubOwner": "idefav",
  "githubRepo": "ssh-tunnel"
}
```

## 安全性说明

1. **文件完整性验证**：所有下载的文件都会进行SHA256校验，确保文件完整性。

2. **版本比较**：系统会自动比较版本号，只提示更新到更新的版本。

3. **手动确认**：更新操作需要用户手动确认，避免意外更新。

4. **网络安全**：所有GitHub API请求都使用HTTPS加密传输。

## 注意事项

1. **网络要求**：需要能够访问GitHub API和Release文件下载地址。

2. **权限要求**：更新操作可能需要相应的文件系统写入权限。

3. **服务重启**：更新完成后需要重启服务才能生效。

4. **备份建议**：建议在更新前备份当前可用的版本文件。

## 故障排除

### 检查更新失败
- 检查网络连接
- 确认GitHub仓库信息正确
- 检查防火墙设置

### 下载失败
- 检查磁盘空间
- 确认下载目录权限
- 检查网络稳定性

### 更新失败
- 检查文件权限
- 确认服务未被其他进程占用
- 查看错误日志

## 版本信息

- 功能版本：v1.0.0
- 支持平台：Windows, Linux, macOS
- Go版本要求：1.24+
