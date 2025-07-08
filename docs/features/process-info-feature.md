# 进程信息显示功能

## 功能概述

在SSH隧道应用的配置管理页面(`/view/app/config`)中新增了"进程信息"显示区域，实时展示当前运行进程的详细信息，帮助用户了解程序的运行状态和位置。

## 功能特性

### 1. 进程信息展示
- **程序执行路径**: 显示当前运行程序二进制文件的完整路径
- **工作目录**: 显示程序运行时的当前工作目录
- **实时获取**: 信息在页面加载时动态获取，确保准确性

### 2. 用户界面
- **位置**: 配置文件路径显示区域下方
- **样式**: 采用卡片布局，与配置文件路径保持一致的设计风格
- **响应式**: 支持不同屏幕尺寸，移动端友好

### 3. 便捷操作
- **一键复制**: 每个路径信息都提供复制按钮
- **视觉反馈**: 复制成功后按钮图标变为勾选标记
- **错误处理**: 当无法获取路径信息时显示相应错误消息

## 技术实现

### 后端实现 (Go)

#### 1. 结构体扩展
```go
type AppConfig struct {
    ConfigFilePath   string
    Config           map[string]interface{}
    ConfigMeta       map[string]ConfigMetadata
    ConfigKeys       map[string]string
    ExecutablePath   string  // 新增：程序执行路径
    WorkingDirectory string  // 新增：工作目录
}
```

#### 2. 路径获取函数
```go
// 获取程序执行路径
func getExecutablePath() string {
    executable, err := os.Executable()
    if err != nil {
        return "无法获取程序路径: " + err.Error()
    }
    // 解析符号链接，获取真实路径
    realPath, err := filepath.EvalSymlinks(executable)
    if err != nil {
        return executable // 如果无法解析符号链接，返回原始路径
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

### 前端实现 (HTML/JavaScript)

#### 1. HTML结构
```html
<!-- 进程信息显示 -->
<div class="card mb-3">
    <div class="card-header">
        <h6 class="mb-0"><i class="bi bi-cpu"></i> 进程信息</h6>
    </div>
    <div class="card-body py-2">
        <div class="row">
            <div class="col-md-6 mb-2">
                <label class="form-label text-muted small">程序执行路径</label>
                <div class="input-group">
                    <input type="text" class="form-control" value="{{.ExecutablePath}}" readonly>
                    <button class="btn btn-outline-secondary" type="button" id="copyExecPathBtn" title="复制程序路径">
                        <i class="bi bi-clipboard"></i>
                    </button>
                </div>
            </div>
            <div class="col-md-6 mb-2">
                <label class="form-label text-muted small">工作目录</label>
                <div class="input-group">
                    <input type="text" class="form-control" value="{{.WorkingDirectory}}" readonly>
                    <button class="btn btn-outline-secondary" type="button" id="copyWorkDirBtn" title="复制工作目录">
                        <i class="bi bi-clipboard"></i>
                    </button>
                </div>
            </div>
        </div>
    </div>
</div>
```

#### 2. JavaScript功能
```javascript
// 复制程序执行路径功能
document.getElementById('copyExecPathBtn').addEventListener('click', function() {
    const pathInput = this.parentElement.querySelector('input');
    const path = pathInput.value;

    navigator.clipboard.writeText(path)
        .then(() => {
            const originalContent = this.innerHTML;
            this.innerHTML = '<i class="bi bi-check"></i>';
            setTimeout(() => this.innerHTML = originalContent, 2000);
        })
        .catch(err => console.error('复制程序路径失败:', err));
});

// 复制工作目录功能
document.getElementById('copyWorkDirBtn').addEventListener('click', function() {
    const pathInput = this.parentElement.querySelector('input');
    const path = pathInput.value;

    navigator.clipboard.writeText(path)
        .then(() => {
            const originalContent = this.innerHTML;
            this.innerHTML = '<i class="bi bi-check"></i>';
            setTimeout(() => this.innerHTML = originalContent, 2000);
        })
        .catch(err => console.error('复制工作目录失败:', err));
});
```

## 使用场景

### 1. 故障排查
- **程序定位**: 快速确认当前运行的程序版本和位置
- **路径验证**: 验证程序是否从预期位置启动
- **环境检查**: 确认工作目录是否正确设置

### 2. 部署管理
- **版本确认**: 在多版本环境中确认正在运行的版本
- **路径管理**: 便于复制路径用于脚本或配置
- **服务配置**: 获取精确路径用于服务配置文件

### 3. 开发调试
- **路径调试**: 快速获取程序和工作目录路径用于调试
- **环境对比**: 在不同环境间对比程序运行位置
- **配置验证**: 确认相对路径配置的基准目录

## 显示信息说明

### 程序执行路径
- **含义**: 当前运行的二进制可执行文件的完整绝对路径
- **特点**: 
  - 自动解析符号链接，显示真实路径
  - 跨平台兼容 (Windows/macOS/Linux)
  - 处理各种路径格式

### 工作目录
- **含义**: 程序启动时的当前工作目录
- **用途**:
  - 相对路径配置的基准目录
  - 日志文件默认输出位置
  - 相对路径资源文件的查找起点

## 错误处理

### 1. 路径获取失败
- **场景**: 系统权限不足或文件系统异常
- **处理**: 显示具体错误信息而不是空白或崩溃
- **示例**: "无法获取程序路径: permission denied"

### 2. 符号链接解析失败
- **场景**: 符号链接损坏或指向不存在的文件
- **处理**: 返回原始路径而非解析后的路径
- **保证**: 确保始终有可用的路径信息

### 3. 复制功能失败
- **场景**: 浏览器不支持或用户拒绝剪贴板权限
- **处理**: 在控制台记录错误，不影响用户界面
- **用户体验**: 复制按钮状态不变，避免误导

## 更新历史

### v1.0 (2025-06-21)
- ✅ 新增进程信息显示功能
- ✅ 支持程序执行路径显示
- ✅ 支持工作目录显示
- ✅ 实现一键复制功能
- ✅ 添加错误处理机制
- ✅ 响应式界面设计

## 相关文档

- [配置管理页面](../config-api.md) - 配置管理API文档
- [服务重启功能](restart-service-feature.md) - 服务重启功能说明
- [多平台部署](../setup/MULTIPLATFORM_SERVICE_SETUP.md) - 部署配置指南
