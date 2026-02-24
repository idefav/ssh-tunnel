# 日志清理功能

## 功能概述

日志清理功能允许用户通过Web管理界面清空日志文件内容，但保留日志文件本身。这个功能特别适用于：

- 清理占用空间过大的日志文件
- 重置日志内容以便于调试
- 定期维护日志文件

## 功能特性

### 🔒 安全特性
- **确认对话框**: 防止误操作
- **Panic恢复**: 使用safe包进行错误处理
- **权限验证**: 仅管理员可访问

### 🎯 主要功能
- **清空内容**: 清空日志文件内容，但保留文件
- **记录操作**: 在清理后写入操作记录
- **实时反馈**: 提供清理状态和结果反馈

## 使用方法

### 1. 访问管理界面
```
http://localhost:2083/view/logs
```

### 2. 清理日志
1. 点击"清理日志内容"按钮
2. 确认操作提示
3. 等待清理完成
4. 查看操作结果

## API接口

### 清理日志接口
- **URL**: `/admin/logs/clear`
- **方法**: `POST`
- **权限**: 管理员
- **返回**: JSON格式结果

#### 请求示例
```javascript
fetch('/admin/logs/clear', {
    method: 'POST',
    headers: {
        'Content-Type': 'application/json',
    }
})
```

#### 响应示例
```json
{
    "success": true,
    "message": "日志文件内容已成功清理",
    "timestamp": "2025-06-23 23:13:06"
}
```

## 技术实现

### 后端实现
- **位置**: `api/admin/admin.go`
- **函数**: `/admin/logs/clear` handler
- **安全**: 使用`safe.SafeCallWithReturn`包装操作

### 前端实现
- **位置**: `views/logs.gohtml`
- **技术**: JavaScript + Fetch API
- **UI**: Bootstrap按钮组件

### 核心代码
```go
// 使用安全调用清理日志文件
err := safe.SafeCallWithReturn(func() error {
    file, err := os.OpenFile(logFilePath, os.O_WRONLY|os.O_TRUNC, 0644)
    if err != nil {
        return fmt.Errorf("无法打开日志文件: %v", err)
    }
    defer file.Close()
    
    // 写入清理记录
    _, err = file.WriteString(fmt.Sprintf("[%s] 日志文件内容已清理\n", 
        time.Now().Format("2006-01-02 15:04:05")))
    return err
})
```

## 测试方法

### 1. 单元测试
```bash
go test -v ./safe
```

### 2. API测试
使用提供的测试脚本：
```bash
# 在浏览器控制台中运行
testClearLogs()
```

### 3. 手动测试
1. 启动服务
2. 生成一些日志
3. 访问管理界面测试清理功能

## 注意事项

⚠️ **重要提醒**
- 此操作会清空日志文件的所有内容
- 操作不可撤销，请谨慎使用
- 文件本身不会被删除，只是内容被清空
- 清理后会写入一条操作记录

## 更新日志

- **v1.4.4+**: 新增日志清理功能
- 支持Web界面操作
- 集成安全机制
- 提供API接口
