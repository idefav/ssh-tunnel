# 测试脚本目录

此目录包含用于测试SSH隧道服务重启功能的脚本文件。

## 脚本说明

### 基础测试脚本
- `test_restart.sh` - Linux/macOS平台的服务重启测试脚本
- `test_restart.bat` - Windows平台的服务重启测试脚本

### 多平台测试脚本
- `test_multiplatform_restart.sh` - 跨平台服务重启测试脚本（Linux/macOS）
- `test_multiplatform_restart.bat` - 跨平台服务重启测试脚本（Windows）

### API测试脚本
- `test-api.js` - 日志清理API测试脚本 🆕
- `test-config.properties` - 测试配置文件 🆕
- `test-github-api.go.bak` - GitHub API测试备份文件 🆕

## 使用方法

### Linux/macOS
```bash
# 基础测试
chmod +x test_restart.sh
./test_restart.sh

# 多平台测试
chmod +x test_multiplatform_restart.sh
./test_multiplatform_restart.sh
```

### Windows
```cmd
# 基础测试
test_restart.bat

# 多平台测试
test_multiplatform_restart.bat
```

## 测试内容

这些脚本主要测试以下功能：
1. 运行模式检测API (`/admin/service/mode`)
2. 服务重启API (`/admin/service/restart`)
3. 多平台兼容性验证
4. 错误处理和回退机制

## 注意事项

- 运行测试前请确保SSH隧道服务正在运行
- 测试脚本会实际调用重启API，请在测试环境中运行
- 确保具有相应的服务管理权限
