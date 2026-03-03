# SSH隧道项目文档和脚本索引

## 📚 文档索引

### 🏠 主要文档
| 文档 | 路径 | 描述 |
|------|------|------|
| 项目说明 | [README.md](../README.md) | 项目主要说明文档 |
| API文档 | [docs/config-api.md](config-api.md) | 配置API接口文档 |

### ⚡ 功能文档
| 功能 | 路径 | 描述 |
|------|------|------|
| 进程信息显示 | [docs/features/process-info-feature.md](features/process-info-feature.md) | 进程信息显示功能详细说明 |
| 服务重启 | [docs/features/restart-service-feature.md](features/restart-service-feature.md) | 服务重启功能详细说明 |
| 多 Profile 动态切换 | [docs/features/multi-profile-switch-design.md](features/multi-profile-switch-design.md) | 多 SSH Profile 动态切换技术方案 |
| SSH稳定性修复 | [docs/features/ssh-stability-fix-2026-03.md](features/ssh-stability-fix-2026-03.md) | 保活与重连逻辑修复说明 |

### 🔧 部署文档
| 文档 | 路径 | 描述 |
|------|------|------|
| 多平台部署 | [docs/setup/MULTIPLATFORM_SERVICE_SETUP.md](setup/MULTIPLATFORM_SERVICE_SETUP.md) | Windows/macOS/Linux服务部署指南 |

### 🌐 Web文档
| 文档 | 路径 | 描述 |
|------|------|------|
| 主页 | [docs/index.html](index.html) | 文档网站主页 |
| 安装指南 | [docs/installation.html](installation.html) | 安装步骤说明 |
| SSH配置 | [docs/ssh-key-setup.html](ssh-key-setup.html) | SSH密钥配置指南 |
| 常见问题 | [docs/faq.html](faq.html) | 常见问题解答 |

## 🔨 脚本索引

### 🏗️ 构建脚本
| 脚本 | 路径 | 平台 | 描述 |
|------|------|------|------|
| 构建脚本 | [scripts/build.sh](../scripts/build.sh) | Linux/macOS | Unix系统构建脚本 |
| 构建脚本 | [scripts/build.ps1](../scripts/build.ps1) | Windows | PowerShell构建脚本 |

### 📥 下载脚本
| 脚本 | 路径 | 平台 | 描述 |
|------|------|------|------|
| 下载脚本 | [scripts/download_latest.sh](../scripts/download_latest.sh) | Linux/macOS | 最新版本下载脚本 |
| 下载脚本 | [scripts/download_latest.ps1](../scripts/download_latest.ps1) | Windows | PowerShell下载脚本 |

### ⚙️ 安装脚本
| 脚本 | 路径 | 平台 | 描述 |
|------|------|------|------|
| 服务安装 | [scripts/install_service.bat](../scripts/install_service.bat) | Windows | Windows服务安装脚本 |

### 🧪 测试脚本
| 脚本 | 路径 | 平台 | 描述 |
|------|------|------|------|
| 重启测试 | [scripts/test/test_restart.sh](../scripts/test/test_restart.sh) | Linux/macOS | 服务重启功能测试 |
| 重启测试 | [scripts/test/test_restart.bat](../scripts/test/test_restart.bat) | Windows | 服务重启功能测试 |
| 多平台测试 | [scripts/test/test_multiplatform_restart.sh](../scripts/test/test_multiplatform_restart.sh) | Linux/macOS | 跨平台重启测试 |
| 多平台测试 | [scripts/test/test_multiplatform_restart.bat](../scripts/test/test_multiplatform_restart.bat) | Windows | 跨平台重启测试 |

## 🎯 快速导航

### 新用户开始
1. [项目说明](../README.md) - 了解项目概况
2. [安装指南](installation.html) - 安装步骤
3. [SSH配置](ssh-key-setup.html) - 配置SSH密钥

### 开发者
1. [API文档](config-api.md) - 接口文档
2. [功能文档](features/) - 功能实现说明
3. [测试脚本](../scripts/test/) - 功能测试

### 运维人员
1. [部署文档](setup/) - 服务部署指南
2. [构建脚本](../scripts/) - 构建和安装脚本
3. [常见问题](faq.html) - 故障排除

## 📋 文档维护

- 📅 最后更新: 2026年3月3日
- 👥 维护者: SSH隧道项目团队
- 📧 反馈: 如有文档问题请提交Issue

---

💡 **提示**: 所有相对路径都基于项目根目录。如果链接无法访问，请检查文件是否存在。
