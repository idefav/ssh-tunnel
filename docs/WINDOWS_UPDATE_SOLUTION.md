# Windows 自动更新解决方案

## 问题描述
在Windows系统下进行自更新时，会遇到以下错误：
```
更新失败: 替换文件失败: rename C:\Users\idefav\AppData\Local\JetBrains\GoLand2025.1\tmp\GoLand\___go_build_ssh_tunnel.exe C:\Users\idefav\AppData\Local\JetBrains\GoLand2025.1\tmp\GoLand\___go_build_ssh_tunnel.exe.old: The process cannot access the file because it is being used by another process.
```

这是因为Windows系统不允许修改正在运行的可执行文件。

## 解决方案

### 1. 批处理脚本更新方案

程序使用批处理脚本来解决Windows下的更新问题：

1. **生成更新脚本**：创建一个时间戳命名的批处理文件
2. **启动脚本**：以最小化窗口方式启动批处理
3. **程序退出**：主程序退出，释放文件锁
4. **文件替换**：批处理脚本等待3秒后替换文件
5. **重启处理**：根据运行模式处理重启

### 2. 服务模式 vs 命令模式

#### 服务模式
- 批处理脚本会自动重启Windows服务
- 用户无需手动干预
- 自动检测服务重启状态

#### 命令模式  
- 批处理脚本提示用户手动重启
- 更新完成后需要用户按任意键
- 批处理窗口显示更新状态

### 3. 批处理脚本内容

#### 服务模式脚本示例：
```batch
@echo off
echo Starting SSH Tunnel Update Process...
echo Waiting for main process to exit...
timeout /t 3 /nobreak >nul

echo Backing up current file...
if exist "ssh-tunnel.exe.backup" del "ssh-tunnel.exe.backup"
if exist "ssh-tunnel.exe" ren "ssh-tunnel.exe" "ssh-tunnel.exe.backup"

echo Installing new version...
move "new-version.exe" "ssh-tunnel.exe"

echo Starting service...
sc start ssh-tunnel-service

echo Cleaning up...
del "%~f0"
```

#### 命令模式脚本示例：
```batch
@echo off
echo Starting SSH Tunnel Update Process...
echo Waiting for main process to exit...
timeout /t 3 /nobreak >nul

echo Backing up current file...
if exist "ssh-tunnel.exe.backup" del "ssh-tunnel.exe.backup"
if exist "ssh-tunnel.exe" ren "ssh-tunnel.exe" "ssh-tunnel.exe.backup"

echo Installing new version...
move "new-version.exe" "ssh-tunnel.exe"

echo Update completed successfully!
echo Please restart the SSH Tunnel manually.
echo.
pause

echo Cleaning up...
del "%~f0"
```

### 4. 用户体验优化

#### 前端提示优化
- **更新确认**：明确告知用户Windows下的更新流程
- **进度显示**：显示"更新脚本已启动"状态
- **自动检测**：服务模式下自动检测重启状态
- **用户引导**：命令模式下提供明确的操作指引

#### 错误处理
- **备份恢复**：更新失败时自动恢复原文件
- **脚本清理**：成功或失败后都会清理临时脚本
- **状态检查**：服务模式下检查服务重启状态

### 5. 技术实现要点

1. **文件锁检测**：在Windows下检测到文件正在使用时启用脚本模式
2. **时间戳命名**：避免脚本文件名冲突
3. **最小化启动**：批处理以最小化方式启动，减少用户干扰
4. **自清理机制**：脚本执行完毕后自动删除
5. **备份机制**：保留上一版本作为备份

### 6. 使用说明

1. **正常更新流程**：
   - 点击"更新到此版本"
   - 确认更新提示
   - 等待下载完成
   - Windows下会自动启动批处理脚本
   - 程序退出

2. **服务模式**：
   - 批处理自动处理重启
   - 页面会自动检测服务状态
   - 重启完成后自动刷新页面

3. **命令模式**：
   - 批处理窗口会显示更新进度
   - 更新完成后提示按任意键
   - 需要用户手动重启程序

### 7. 故障排除

如果更新过程中遇到问题：

1. **检查备份文件**：`程序名.exe.backup` 是原始版本
2. **手动恢复**：如果更新失败，可以手动将备份文件重命名回来
3. **权限问题**：确保程序有写入权限
4. **防病毒软件**：某些防病毒软件可能会阻止文件替换

这个解决方案确保了Windows下的自更新功能能够可靠运行，同时提供了良好的用户体验。
