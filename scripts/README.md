# Scripts

本目录包含构建、兼容下载、测试和辅助运行脚本。

## 首次安装

首次安装请优先使用 GitHub Pages 单入口，而不是直接调用旧版下载脚本：

```bash
# Linux/macOS
curl -fsSL https://idefav.github.io/ssh-tunnel/install | sh
```

```powershell
# Windows PowerShell
irm https://idefav.github.io/ssh-tunnel/install | iex
```

一键安装只处理首次安装。已有实例后，请在管理页面版本页完成升级。

首装交互会自动扫描当前用户 `.ssh` 目录中的 SSH 密钥，优先选择已存在的公钥作为默认输入；如果本机还没有密钥，会提示自动生成或进入交互式生成。也允许手动改成其他公钥或私钥路径；在写入配置前会先校验 SSH 免密登录，必要时自动尝试配置。

## 主要脚本

- `build.sh`: Linux/macOS 构建脚本
- `build.ps1`: Windows 构建脚本
- `download_latest.sh`: Linux/macOS 旧版下载脚本，保留兼容用途
- `download_latest.ps1`: Windows 旧版下载脚本，保留兼容用途
- `install_service.bat`: Windows 旧版服务安装辅助脚本
- `utils/start-with-auto-update.sh`: Unix 自动更新启动辅助脚本
- `utils/start-with-auto-update.bat`: Windows 自动更新启动辅助脚本

## 测试

- `test/`: 脚本测试与示例配置
- `test/README.md`: 测试说明入口
