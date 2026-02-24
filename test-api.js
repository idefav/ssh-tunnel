// 测试日志清理API
async function testClearLogs() {
    try {
        console.log('开始测试日志清理API...');
        
        const response = await fetch('/admin/logs/clear', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            }
        });
        
        console.log('Response status:', response.status);
        console.log('Response ok:', response.ok);
        
        if (response.ok) {
            const result = await response.json();
            console.log('Success result:', result);
            alert('API测试成功: ' + result.message);
        } else {
            const errorText = await response.text();
            console.log('Error response:', errorText);
            alert('API测试失败: ' + errorText);
        }
    } catch (error) {
        console.error('请求异常:', error);
        alert('请求异常: ' + error.message);
    }
}

// 在控制台中运行测试
console.log('执行 testClearLogs() 来测试API');
