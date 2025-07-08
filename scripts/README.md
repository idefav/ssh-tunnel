# SSH隧道脚本目录

本目录包含SSH隧道项目的各种脚本文件，用于构建、部署、测试和维护。

## 目录结构

### 📁 test/
测试脚本
- 服务重启功能测试脚本
- 多平台兼容性测试脚本
- API测试脚本
- 测试配置文件
- 详情请查看 `test/README.md`

### 📁 utils/
通用工具脚本
- `start-with-auto-update.bat` - Windows自动更新启动脚本
- `start-with-auto-update.sh` - Linux/macOS自动更新启动脚本

### 📁 dev/
开发辅助脚本（预留目录）
- 开发环境配置脚本
- 调试辅助工具
- 代码质量检查脚本
- 系统信息收集脚本
- 日志分析工具
- 配置文件处理脚本

### 📄 根目录脚本

#### 构建脚本
- `build.sh` - Linux/macOS平台构建脚本
- `build.ps1` - Windows平台PowerShell构建脚本

#### 下载脚本
- `download_latest.sh` - Linux/macOS平台最新版本下载脚本
- `download_latest.ps1` - Windows平台PowerShell下载脚本

#### 安装脚本
- `install_service.bat` - Windows服务安装脚本

## 脚本使用说明

### 构建项目
```bash
# Linux/macOS
chmod +x build.sh
./build.sh

# Windows PowerShell
.\build.ps1
```

### 下载最新版本
```bash
# Linux/macOS
chmod +x download_latest.sh
./download_latest.sh

# Windows PowerShell
.\download_latest.ps1
```

### 安装服务 (Windows)
```cmd
# 以管理员权限运行
install_service.bat
```

### 运行测试
```bash
# 进入测试目录
cd test/

# 查看测试说明
cat README.md

# 运行测试脚本
./test_restart.sh
```

## 权限要求

- **构建脚本**: 需要Go环境和相应的构建权限
- **下载脚本**: 需要网络访问权限
- **安装脚本**: 需要管理器权限
- **测试脚本**: 需要服务管理权限

## 注意事项

1. 运行脚本前请检查执行权限
2. Windows环境建议使用PowerShell或以管理员权限运行CMD
3. Linux/macOS环境注意脚本的可执行权限设置
4. 测试脚本请在测试环境中运行，避免影响生产服务
