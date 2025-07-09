package tunnel

import (
	"context"
	"golang.org/x/crypto/ssh"
	"log"
	"ssh-tunnel/safe"
	"sync"
	"time"
)

// reconnectSSH 实现SSH连接的重连逻辑
func (t *Tunnel) ReconnectSSH(ctx context.Context) {
	// 使用互斥锁确保同一时间只有一个重连过程
	t.reconnectMutex.Lock()
	defer t.reconnectMutex.Unlock()

	if t.client != nil {
		return
	}

	// 如果上下文已取消���则不执行重连
	if ctx.Err() != nil {
		return
	}

	log.Printf("正在尝试重新连接SSH服务器: %s", t.serverAddress)
	var once sync.Once
	withContinue, _ := t.dialSSH(&once)
	if !withContinue {
		return
	}

	// 重连的最大尝试次数
	maxRetries := 10
	// 初始重试间隔（秒）
	baseRetryInterval := t.retryInterval
	if baseRetryInterval == 0 {
		baseRetryInterval = 1 * time.Second
	}

	// 指数退避策略的重试
	for i := 0; i < maxRetries; i++ {
		// 检查上下文是否已取消
		if ctx.Err() != nil {
			log.Println("重连操作被取消")
			return
		}

		// 计算当前重试的等待时间（使用指数退避策略）
		retryInterval := baseRetryInterval * time.Duration(1<<uint(i))
		if retryInterval > 1*time.Minute {
			retryInterval = 1 * time.Minute // 最大等待5分钟
		}

		log.Printf("等待 %v 秒后尝试重连 (尝试 %d/%d)", retryInterval.Seconds(), i+1, maxRetries)
		select {
		case <-ctx.Done():
			return
		case <-time.After(retryInterval):
			// 继续尝试重连
		}

		withContinue, _ := t.dialSSH(&once)
		if withContinue {
			log.Printf("SSH连接尝试失败，继续重试")
			continue
		}

		// 启动keep-alive监控
		var wg sync.WaitGroup
		wg.Add(1)
		t.keepAliveMonitor(ctx, &once, &wg)

		// 当keepAliveMonitor退出时，说明连接已断开
		t.client = nil
		log.Printf("SSH连接已关闭，准备重新连接")

		// 立即开始新的重连循环
		safe.GO(func() {
			t.ReconnectSSH(ctx)
		})
		return
	}

	log.Printf("达到最大重试次数 (%d)，将在服务需要时重新尝试", maxRetries)
}

func (t *Tunnel) dialSSH(once *sync.Once) (withContinue bool, err error) {
	// 尝试建立新的SSH连接

	cl, err := ssh.Dial("tcp", t.serverAddress, &ssh.ClientConfig{
		User:            t.user,
		Auth:            t.auth,
		HostKeyCallback: t.hostKeys,
		Timeout:         10 * time.Second, // 增加超时时间以适应不稳定网络
	})

	if err != nil {
		once.Do(func() {
			log.Printf("SSH重连失败: %v，将继续重试", err)
		})
		return true, err
	}

	// 连接成功
	t.client = cl
	log.Println("成功重新连接到SSH服务器")
	return false, nil
}
